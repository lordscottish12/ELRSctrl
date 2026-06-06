//go:build linux

package detect

import (
	"image"
	"image/draw"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"elrsctrl/internal/video"
)

// TestONNXLiveSmoke runs the real ONNX detector end-to-end against a JPEG, using
// the actual model + libonnxruntime.so. It's skipped unless DETECT_MODEL and
// DETECT_IMAGE are set, so normal `go test` (which has neither the lib nor a model)
// stays green. Run it from WSL:
//
//	DETECT_MODEL=dist/yolov8n.onnx DETECT_LIB=dist/libonnxruntime.so \
//	DETECT_IMAGE=/path/person.jpg go test ./internal/detect/ -run Live -v
func TestONNXLiveSmoke(t *testing.T) {
	model, img := os.Getenv("DETECT_MODEL"), os.Getenv("DETECT_IMAGE")
	if model == "" || img == "" {
		t.Skip("set DETECT_MODEL and DETECT_IMAGE to run the live ONNX smoke test")
	}
	frame := loadJPEGFrame(t, img)

	det, err := NewDetector(DetectorConfig{
		ModelPath: model,
		LibPath:   os.Getenv("DETECT_LIB"),
		InputSize: 640,
		Conf:      0.4,
	})
	if err != nil {
		t.Fatalf("NewDetector: %v", err)
	}
	defer det.Close()

	det.Detect(frame) // warm-up (allocations / first-run setup)
	start := time.Now()
	dets, err := det.Detect(frame)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	t.Logf("detected %d people in %v (frame %dx%d)", len(dets), time.Since(start), frame.W, frame.H)
	for _, d := range dets {
		t.Logf("  person %.2f at %v", d.Score, d.Box)
	}
	if len(dets) == 0 {
		t.Fatalf("expected at least one person in %s — decode/model mismatch?", img)
	}
}

func loadJPEGFrame(t *testing.T, path string) *video.Frame {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := im.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), im, b.Min, draw.Src)
	return &video.Frame{W: b.Dx(), H: b.Dy(), Pix: rgba.Pix}
}
