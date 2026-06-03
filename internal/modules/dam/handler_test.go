package dam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/blob"
	"b2bcommerce/internal/imageproc"
	"b2bcommerce/internal/queue/jobs"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "dam-test-secret"

// syncRend generates renditions inline so tests can assert ready status + bytes
// without standing up the worker.
type syncRend struct {
	pool  *pgxpool.Pool
	store blob.Store
	proc  imageproc.Processor
}

func (s syncRend) EnqueueRendition(ctx context.Context, id int64, preset string) error {
	return jobs.GenerateRendition(ctx, s.pool, s.store, s.proc, id, preset)
}

func newServer(t *testing.T) (http.Handler, *auth.Issuer, blob.Store) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	bs, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	proc := imageproc.GoProcessor{}
	h := server.New(st, issuer,
		server.WithMedia(bs, proc),
		server.WithRendition(syncRend{pool: pool, store: bs, proc: proc}),
	)
	return h, issuer, bs
}

func adminToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", []string{"cms.view", "cms.manage"})
	return tok
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 90, B: 240, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// uploadImage posts a multipart image and returns the response recorder.
func uploadImage(t *testing.T, h http.Handler, tok string, data []byte, tags string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "pic.png")
	_, _ = fw.Write(data)
	if tags != "" {
		_ = mw.WriteField("tags", tags)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/media", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func get(t *testing.T, h http.Handler, path, tok string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

type assetResp struct {
	ID         int64             `json:"id"`
	PublicID   string            `json:"public_id"`
	URL        string            `json:"url"`
	Status     string            `json:"status"`
	Renditions []json.RawMessage `json:"renditions"`
	Transforms map[string]string `json:"transforms"`
	Tags       []string          `json:"tags"`
}

func TestUploadGeneratesRenditionsAndDedupes(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer)
	img := pngBytes(t, 800, 400)

	rr := uploadImage(t, h, tok, img, "logo,brand")
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: want 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var a assetResp
	_ = json.Unmarshal(rr.Body.Bytes(), &a)
	// Three seeded presets (thumb/card/hero) → ready with 3 renditions (sync).
	if a.Status != "ready" {
		t.Errorf("status = %q, want ready", a.Status)
	}
	if len(a.Renditions) != 3 {
		t.Errorf("renditions = %d, want 3", len(a.Renditions))
	}
	if len(a.Transforms) != 3 || a.Transforms["thumb"] == "" {
		t.Errorf("expected signed transforms for each preset, got %v", a.Transforms)
	}
	if len(a.Tags) != 2 {
		t.Errorf("tags = %v, want 2", a.Tags)
	}

	// Re-uploading identical bytes dedupes (200, same asset).
	rr2 := uploadImage(t, h, tok, img, "")
	if rr2.Code != http.StatusOK {
		t.Fatalf("dedupe upload: want 200, got %d", rr2.Code)
	}
	var a2 assetResp
	_ = json.Unmarshal(rr2.Body.Bytes(), &a2)
	if a2.ID != a.ID {
		t.Errorf("dedupe returned new asset %d, want existing %d", a2.ID, a.ID)
	}

	// The original blob is served publicly.
	if orig := get(t, h, a.URL, ""); orig.Code != http.StatusOK {
		t.Errorf("serve original: want 200, got %d", orig.Code)
	}

	// Signed transform redirects to the cached rendition (public /media/file).
	tr := get(t, h, a.Transforms["thumb"], "")
	if tr.Code != http.StatusFound {
		t.Fatalf("signed transform: want 302, got %d (%s)", tr.Code, tr.Body.String())
	}
	loc := tr.Header().Get("Location")
	if loc == "" {
		t.Fatal("transform redirect missing Location")
	}
	if served := get(t, h, loc, ""); served.Code != http.StatusOK {
		t.Errorf("serve rendition: want 200, got %d", served.Code)
	}

	// A bare (unsigned) transform URL is rejected.
	bare := "/media/" + a.PublicID + "/t/thumb"
	if br := get(t, h, bare, ""); br.Code != http.StatusForbidden {
		t.Errorf("unsigned transform: want 403, got %d", br.Code)
	}
}

func TestUploadRejectsNonImage(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer)
	rr := uploadImage(t, h, tok, []byte("this is not an image"), "")
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-image upload: want 415, got %d", rr.Code)
	}
}

func TestPresetCreateAndList(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer)

	body := bytes.NewBufferString(`{"name":"square","width":300,"height":300,"fit":"cover","format":"png","quality":90}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/transformation-presets", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create preset: want 201, got %d (%s)", rr.Code, rr.Body.String())
	}

	lr := get(t, h, "/admin/transformation-presets", tok)
	var out struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &out)
	if len(out.Items) != 4 { // 3 seeded + square
		t.Errorf("presets = %d, want 4", len(out.Items))
	}
}

func TestMediaRequiresAuth(t *testing.T) {
	h, _, _ := newServer(t)
	if rr := get(t, h, "/admin/media", ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated list: want 401, got %d", rr.Code)
	}
}
