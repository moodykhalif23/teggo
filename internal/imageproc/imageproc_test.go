package imageproc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// makePNG returns a w×h PNG with a solid colour.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestTransformCoverCrops(t *testing.T) {
	src := makePNG(t, 400, 200) // 2:1
	out, w, h, format, err := GoProcessor{}.Transform(context.Background(), bytes.NewReader(src),
		Preset{Name: "thumb", Width: 100, Height: 100, Fit: "cover", Format: "jpeg", Quality: 80})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if w != 100 || h != 100 {
		t.Errorf("cover dims = %dx%d, want 100x100", w, h)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("output not decodable: %v", err)
	}
}

func TestTransformContainPreservesAspect(t *testing.T) {
	src := makePNG(t, 400, 200) // 2:1
	_, w, h, _, err := GoProcessor{}.Transform(context.Background(), bytes.NewReader(src),
		Preset{Name: "card", Width: 100, Height: 100, Fit: "contain", Format: "png"})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	// 2:1 contained in 100x100 → 100x50.
	if w != 100 || h != 50 {
		t.Errorf("contain dims = %dx%d, want 100x50", w, h)
	}
}

func TestTransformWebpFormatFallsBackToJpeg(t *testing.T) {
	src := makePNG(t, 50, 50)
	_, _, _, format, err := GoProcessor{}.Transform(context.Background(), bytes.NewReader(src),
		Preset{Name: "x", Width: 20, Height: 20, Fit: "cover", Format: "webp"})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("webp output format = %q, want jpeg (pure-Go fallback)", format)
	}
}
