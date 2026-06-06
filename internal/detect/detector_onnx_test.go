//go:build linux

package detect

import "testing"

// decode()/letterbox() are the only ONNX-path logic not exercised by the runtime,
// and the easiest to get subtly wrong (tensor layout, box unmapping). Exercise them
// with a synthetic YOLOv8 output — no model or onnxruntime lib needed, since decode
// never calls into ORT.

func TestLetterbox(t *testing.T) {
	// 640x480 into 640: fits width, pads top/bottom by (640-480)/2 = 80.
	if s, px, py := letterbox(640, 480, 640); s != 1 || px != 0 || py != 80 {
		t.Errorf("letterbox(640,480,640) = %v,%v,%v; want 1,0,80", s, px, py)
	}
	// 480x640 into 640: fits height, pads left/right by 80.
	if s, px, py := letterbox(480, 640, 640); s != 1 || px != 80 || py != 0 {
		t.Errorf("letterbox(480,640,640) = %v,%v,%v; want 1,80,0", s, px, py)
	}
}

func TestDecodeYOLOOutput(t *testing.T) {
	// 2 anchors, channel-major [c*A + a]: ch0..3 = cx,cy,w,h (input px), ch4 = person.
	// Anchor 0 is a confident box at (cx320,cy320,w100,h200); anchor 1 is below conf.
	const A = 2
	out := []float32{
		320, 0, // cx
		320, 0, // cy
		100, 0, // w
		200, 0, // h
		0.9, 0.1, // person score
	}
	d := &onnxDetector{anchors: A, conf: 0.5, inputSize: 640}
	dets := d.decode(out, 640, 640, 1, 0, 0) // scale 1, no padding

	if len(dets) != 1 {
		t.Fatalf("got %d detections, want 1 (anchor 1 is below conf)", len(dets))
	}
	b := dets[0].Box
	if b.Min.X != 270 || b.Min.Y != 220 || b.Max.X != 370 || b.Max.Y != 420 {
		t.Errorf("box = %v, want (270,220)-(370,420)", b)
	}
	if d := dets[0].Score - 0.9; d < -1e-4 || d > 1e-4 {
		t.Errorf("score = %v, want ~0.9", dets[0].Score)
	}
}
