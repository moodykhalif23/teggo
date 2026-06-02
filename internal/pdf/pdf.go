package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Renderer turns an HTML document into PDF bytes.
type Renderer interface {
	Render(ctx context.Context, html string) ([]byte, error)
}

// Gotenberg renders HTML via a Gotenberg service's Chromium module.
type Gotenberg struct {
	BaseURL string
	HTTP    *http.Client
}

func NewGotenberg(baseURL string) *Gotenberg {
	return &Gotenberg{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (g *Gotenberg) Render(ctx context.Context, html string) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("files", "index.html")
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(part, html); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	url := g.BaseURL + "/forms/chromium/convert/html"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("gotenberg status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return io.ReadAll(resp.Body)
}

type Stub struct{}

func (Stub) Render(_ context.Context, _ string) ([]byte, error) {
	return []byte(minimalPDF), nil
}

// minimalPDF is a hand-written, structurally valid empty single-page PDF.
const minimalPDF = "%PDF-1.4\n" +
	"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
	"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]>>endobj\n" +
	"trailer<</Root 1 0 R>>\n" +
	"%%EOF\n"
