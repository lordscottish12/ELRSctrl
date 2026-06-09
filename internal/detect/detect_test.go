package detect

import (
	"image"
	"testing"
)

func rect(x0, y0, x1, y1 int) image.Rectangle { return image.Rect(x0, y0, x1, y1) }

func TestIoU(t *testing.T) {
	a := rect(0, 0, 10, 10)
	if got := iou(a, a); got != 1 {
		t.Errorf("identical IoU = %v, want 1", got)
	}
	if got := iou(a, rect(20, 20, 30, 30)); got != 0 {
		t.Errorf("disjoint IoU = %v, want 0", got)
	}
	// Half-overlap: a∩b = 5x10=50, union = 100+100-50=150 -> 1/3.
	if got := iou(a, rect(5, 0, 15, 10)); got < 0.33 || got > 0.34 {
		t.Errorf("half-overlap IoU = %v, want ~0.333", got)
	}
}

func TestNMS(t *testing.T) {
	dets := []Detection{
		{Box: rect(0, 0, 10, 10), Score: 0.6},
		{Box: rect(1, 1, 11, 11), Score: 0.9}, // overlaps the first heavily
		{Box: rect(50, 50, 60, 60), Score: 0.5}, // separate
	}
	keep := nms(dets, 0.5)
	if len(keep) != 2 {
		t.Fatalf("kept %d, want 2", len(keep))
	}
	if keep[0].Score != 0.9 {
		t.Errorf("first kept score = %v, want 0.9 (highest survives)", keep[0].Score)
	}
}

// Stable-ID, expiry, and ego-motion (centroid) association are covered for the live
// tracker in kalman_test.go (the greedy Tracker these once exercised was retired).
