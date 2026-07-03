package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTestPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "img.png")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestReadImageRun(t *testing.T) {
	path := writeTestPNG(t)
	input, _ := json.Marshal(map[string]string{"path": path})

	out, err := ReadImage{}.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "" {
		t.Error("expected a non-empty caption")
	}

	// ReadImage implements ImageProducer; the image must decode through.
	var ip ImageProducer = ReadImage{}
	imgs, err := ip.Images(input)
	if err != nil {
		t.Fatalf("Images: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("got %d images, want 1", len(imgs))
	}
	if imgs[0].MediaType != "image/png" || imgs[0].Data == "" {
		t.Errorf("bad image content: %+v", imgs[0])
	}
}

func TestReadImageErrors(t *testing.T) {
	// Missing path.
	if _, err := (ReadImage{}).Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Error("expected error for missing path")
	}

	// Non-image file with an image extension (corrupt).
	dir := t.TempDir()
	bad := filepath.Join(dir, "not.png")
	if err := os.WriteFile(bad, []byte("hello, not a png"), 0644); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"path": bad})
	if _, err := (ReadImage{}).Run(context.Background(), input); err == nil {
		t.Error("expected error for corrupt image")
	}
	if _, err := (ReadImage{}).Images(input); err == nil {
		t.Error("expected Images error for corrupt image")
	}
}

func TestReadImageRegistered(t *testing.T) {
	r := DefaultRegistry()
	if _, ok := r.Get("read_image"); !ok {
		t.Error("read_image not registered in DefaultRegistry")
	}
}
