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

func TestTrackerCentroidAssociation(t *testing.T) {
	// A camera pan slides the box clear of its previous overlap (IoU 0) but only by
	// ~one box-width — the centroid fallback should keep the same ID, not spawn one.
	tr := Tracker{}
	out := tr.Update([]Detection{{Box: rect(0, 0, 20, 40), Score: 0.9}})
	id := out[0].ID

	out = tr.Update([]Detection{{Box: rect(25, 0, 45, 40), Score: 0.9}}) // shifted +25px, no overlap
	if len(out) != 1 {
		t.Fatalf("got %d tracks, want 1 (centroid should re-associate)", len(out))
	}
	if out[0].ID != id {
		t.Errorf("ID changed %d -> %d across an ego-motion jump", id, out[0].ID)
	}
}

func TestTrackerStableIDsAndExpiry(t *testing.T) {
	tr := Tracker{MaxMissed: 2}

	// Frame 1: one person -> ID 1.
	out := tr.Update([]Detection{{Box: rect(0, 0, 20, 40), Score: 0.9}})
	if len(out) != 1 || out[0].ID != 1 {
		t.Fatalf("frame1 = %+v, want one track id1", out)
	}

	// Frame 2: the same person nudged + a new disjoint one -> ID 1 keeps, ID 2 new.
	out = tr.Update([]Detection{
		{Box: rect(2, 1, 22, 41), Score: 0.9}, // overlaps track 1
		{Box: rect(100, 0, 120, 40), Score: 0.8},
	})
	if len(out) != 2 {
		t.Fatalf("frame2 tracks = %d, want 2", len(out))
	}
	byID := map[int]Track{}
	for _, x := range out {
		byID[x.ID] = x
	}
	if got, ok := byID[1]; !ok || got.Age != 2 || got.Missed != 0 {
		t.Errorf("track 1 = %+v, want Age2 Missed0", got)
	}
	if _, ok := byID[2]; !ok {
		t.Errorf("expected a new track id2, got %+v", out)
	}

	// Three empty frames: both tracks miss; after MaxMissed=2 they're gone.
	tr.Update(nil)
	tr.Update(nil)
	out = tr.Update(nil)
	if len(out) != 0 {
		t.Errorf("after expiry tracks = %+v, want none", out)
	}
}
