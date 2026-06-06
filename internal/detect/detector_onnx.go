//go:build linux

package detect

import (
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"elrsctrl/internal/video"
)

// ONNX person detector for a YOLOv8-family model exported by Ultralytics
// (input "images" NCHW RGB 0..1; output "output0" shape [1, 4+numClasses, anchors]
// with box cx,cy,w,h in input pixels and per-class scores; COCO "person" = class 0,
// so its score is always output channel index 4). We read the actual output shape
// from the model, so 320/640 inputs and person-only or full-COCO exports all work.
//
// The onnxruntime shared library is dlopened at runtime (SetSharedLibraryPath), so
// the build needs only cgo — no ORT headers/libs at compile time.

const personChannel = 4 // 4 box coords, then class 0 (person) is the first score

var (
	ortInitOnce sync.Once
	ortInitErr  error
)

type onnxDetector struct {
	session   *ort.AdvancedSession
	input     *ort.Tensor[float32]
	output    *ort.Tensor[float32]
	inputSize  int
	anchors    int
	conf       float64
	coordScale float64 // box-output unit: inputSize for normalized 0..1 exports, 1 for pixel exports (0 = sniff on first frame)
	debug      bool
}

// NewDetector loads the model and prepares a reusable session. cfg.LibPath may be
// empty (defaults to "libonnxruntime.so" on the loader path / next to the binary).
func NewDetector(cfg DetectorConfig) (Detector, error) {
	modelPath := resolveModelPath(cfg.ModelPath)
	libPath := resolveLibPath(cfg.LibPath)
	ortInitOnce.Do(func() {
		ort.SetSharedLibraryPath(libPath)
		ortInitErr = ort.InitializeEnvironment()
	})
	if ortInitErr != nil {
		return nil, fmt.Errorf("detect: onnxruntime init (%s): %w", libPath, ortInitErr)
	}

	inSize := cfg.InputSize
	if inSize <= 0 {
		inSize = 640
	}
	conf := cfg.Conf
	if conf <= 0 {
		conf = 0.4
	}

	// Read the model's output shape so we don't hardcode 640/80-class.
	_, outInfo, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, fmt.Errorf("detect: read model io: %w", err)
	}
	if len(outInfo) == 0 || len(outInfo[0].Dimensions) != 3 {
		return nil, fmt.Errorf("detect: unexpected model output (need YOLOv8 [1,C,anchors])")
	}
	channels := int(outInfo[0].Dimensions[1])
	anchors := int(outInfo[0].Dimensions[2])
	if channels <= personChannel {
		return nil, fmt.Errorf("detect: model has %d output channels, need > %d", channels, personChannel)
	}

	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, int64(inSize), int64(inSize)))
	if err != nil {
		return nil, err
	}
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(channels), int64(anchors)))
	if err != nil {
		input.Destroy()
		return nil, err
	}
	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"images"}, []string{outInfo[0].Name},
		[]ort.ArbitraryTensor{input}, []ort.ArbitraryTensor{output}, nil)
	if err != nil {
		input.Destroy()
		output.Destroy()
		return nil, fmt.Errorf("detect: create session: %w", err)
	}

	return &onnxDetector{
		session: session, input: input, output: output,
		inputSize: inSize, anchors: anchors, conf: conf,
		debug: os.Getenv("DETECT_DEBUG") != "",
	}, nil
}

func (d *onnxDetector) Detect(f *video.Frame) ([]Detection, error) {
	if f == nil || f.W == 0 || f.H == 0 || len(f.Pix) < f.W*f.H*4 {
		return nil, nil
	}
	scale, padX, padY := letterbox(f.W, f.H, d.inputSize)
	d.fillInput(f, scale, padX, padY)
	if err := d.session.Run(); err != nil {
		return nil, err
	}
	dets := d.decode(d.output.GetData(), f.W, f.H, scale, padX, padY)
	if d.debug {
		log.Printf("detect: %d people (coordScale=%.0f, anchors=%d)", len(dets), d.coordScale, d.anchors)
	}
	return dets, nil
}

// letterbox returns the scale and padding to fit a WxH frame into a square of side
// `size` while preserving aspect (the standard YOLO preprocessing).
func letterbox(w, h, size int) (scale, padX, padY float64) {
	scale = float64(size) / float64(w)
	if s := float64(size) / float64(h); s < scale {
		scale = s
	}
	padX = (float64(size) - float64(w)*scale) / 2
	padY = (float64(size) - float64(h)*scale) / 2
	return
}

// fillInput writes the letterboxed frame into the NCHW RGB input tensor (0..1),
// sampling nearest-neighbour and padding with the YOLO gray (114/255).
func (d *onnxDetector) fillInput(f *video.Frame, scale, padX, padY float64) {
	data := d.input.GetData()
	S := d.inputSize
	plane := S * S
	const pad = 114.0 / 255.0
	for iy := 0; iy < S; iy++ {
		fy := (float64(iy) - padY) / scale
		for ix := 0; ix < S; ix++ {
			o := iy*S + ix
			fx := (float64(ix) - padX) / scale
			sx, sy := int(fx), int(fy)
			if fx < 0 || fy < 0 || sx >= f.W || sy >= f.H {
				data[o], data[plane+o], data[2*plane+o] = pad, pad, pad
				continue
			}
			si := (sy*f.W + sx) * 4
			data[o] = float32(f.Pix[si]) / 255
			data[plane+o] = float32(f.Pix[si+1]) / 255
			data[2*plane+o] = float32(f.Pix[si+2]) / 255
		}
	}
}

// decode turns the raw YOLOv8 output (channel-major [c*anchors + a]) into
// person Detections in frame pixels, then NMS-filters them.
func (d *onnxDetector) decode(out []float32, fw, fh int, scale, padX, padY float64) []Detection {
	A := d.anchors
	if d.coordScale == 0 {
		d.coordScale = sniffCoordScale(out, A, d.inputSize)
	}
	cs := d.coordScale
	var dets []Detection
	for a := 0; a < A; a++ {
		score := float64(out[personChannel*A+a])
		if score < d.conf {
			continue
		}
		cx := float64(out[0*A+a]) * cs
		cy := float64(out[1*A+a]) * cs
		w := float64(out[2*A+a]) * cs
		h := float64(out[3*A+a]) * cs
		x0 := (cx - w/2 - padX) / scale
		y0 := (cy - h/2 - padY) / scale
		x1 := (cx + w/2 - padX) / scale
		y1 := (cy + h/2 - padY) / scale
		box := image.Rect(clampi(int(x0), 0, fw), clampi(int(y0), 0, fh),
			clampi(int(x1), 0, fw), clampi(int(y1), 0, fh))
		if box.Dx() < 4 || box.Dy() < 4 {
			continue
		}
		dets = append(dets, Detection{Box: box, Score: score})
	}
	return nms(dets, 0.45)
}

// sniffCoordScale figures out the unit of the box outputs: Ultralytics' standard
// export gives input-pixel coords (0..inputSize), but some exports normalize to
// 0..1. We peek at the largest box-channel value — tiny means normalized, so we
// scale up to pixels; otherwise leave as-is.
func sniffCoordScale(out []float32, A, inputSize int) float64 {
	var max float32
	for c := 0; c < 4; c++ {
		for a := c * A; a < (c+1)*A; a++ {
			if out[a] > max {
				max = out[a]
			}
		}
	}
	if max <= 4 {
		return float64(inputSize)
	}
	return 1
}

func (d *onnxDetector) Close() error {
	if d.session != nil {
		d.session.Destroy()
	}
	if d.input != nil {
		d.input.Destroy()
	}
	if d.output != nil {
		d.output.Destroy()
	}
	return nil
}

// resolveModelPath finds the ONNX model. Absolute paths and existing relative
// paths are used as-is; otherwise (and for the default "model.onnx") we look next
// to the executable, so the model can just sit beside elrsctrl regardless of the
// directory the app was launched from.
func resolveModelPath(configured string) string {
	if configured == "" {
		configured = "model.onnx"
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	if _, err := os.Stat(configured); err == nil {
		return configured // found relative to the working directory
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), configured)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return configured
}

// resolveLibPath picks where to load libonnxruntime.so from. An explicit config
// path wins. Otherwise we look next to the executable (so "just drop the .so beside
// elrsctrl" works), then fall back to the bare name for the system loader path.
func resolveLibPath(configured string) string {
	if configured != "" {
		return configured
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "libonnxruntime.so")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return "libonnxruntime.so"
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
