package detect

import (
	"image"
	"testing"
)

// boxAt returns a 20x40 box centered at (cx, cy).
func boxAt(cx, cy int) image.Rectangle { return rect(cx-10, cy-20, cx+10, cy+20) }

func centerX(r image.Rectangle) int { return (r.Min.X + r.Max.X) / 2 }

// feed walks a target across several frames so the filter learns a rightward velocity.
func feed(tr *kalmanTracker, fromCX, step, frames int) int {
	cx := fromCX
	for i := 0; i < frames; i++ {
		tr.Update([]Detection{{Box: boxAt(cx, 100), Score: 0.9}})
		cx += step
	}
	return cx
}

func TestKalmanStableIDAndExpiry(t *testing.T) {
	tr := &kalmanTracker{MaxMissed: 2}
	out := tr.Update([]Detection{{Box: boxAt(100, 100), Score: 0.9}})
	if len(out) != 1 || out[0].ID != 1 {
		t.Fatalf("frame1 = %+v, want one track id1", out)
	}
	out = tr.Update([]Detection{{Box: boxAt(104, 100), Score: 0.9}}) // small nudge, overlaps
	if len(out) != 1 || out[0].ID != 1 || out[0].Missed != 0 || out[0].Age != 2 {
		t.Fatalf("frame2 = %+v, want id1 age2 missed0", out)
	}
	// Three empty frames: after MaxMissed=2 the track is gone.
	tr.Update(nil)
	tr.Update(nil)
	if out = tr.Update(nil); len(out) != 0 {
		t.Errorf("after expiry = %+v, want none", out)
	}
}

func TestKalmanPredictsForwardThroughMiss(t *testing.T) {
	tr := &kalmanTracker{}
	last := feed(tr, 100, 10, 6) // moving +10px/frame, last *fed* center is `last-10`
	fedCX := last - 10

	// One missed frame: the box must coast *forward* (not freeze), i.e. its center
	// should advance past the last measured position in the direction of motion.
	out := tr.Update(nil)
	if len(out) != 1 {
		t.Fatalf("coasting track lost: %+v", out)
	}
	if got := centerX(out[0].Box); got <= fedCX {
		t.Errorf("missed-frame center = %d, want > last measured %d (should coast forward)", got, fedCX)
	}
	if out[0].Missed != 1 {
		t.Errorf("Missed = %d, want 1", out[0].Missed)
	}
}

func TestKalmanMatchesFastTargetViaPrediction(t *testing.T) {
	tr := &kalmanTracker{}
	feed(tr, 100, 30, 5) // fast: +30px/frame learns a strong velocity
	id := tr.tracks[0].ID

	// Next detection jumps +30px again — far enough that the *last measured* box and
	// the new one barely overlap, but the predicted box lands on it, so the ID holds.
	out := tr.Update([]Detection{{Box: boxAt(100+30*5, 100), Score: 0.9}})
	if len(out) != 1 {
		t.Fatalf("got %d tracks, want 1 (prediction should re-associate)", len(out))
	}
	if out[0].ID != id {
		t.Errorf("ID changed %d -> %d on a fast target", id, out[0].ID)
	}
}

func TestKalmanPredictionTrustCapDecays(t *testing.T) {
	tr := &kalmanTracker{MaxMissed: 100} // don't let expiry hide a runaway
	feed(tr, 100, 20, 6)                 // learn a rightward velocity

	// The first coast frame advances by ~the learned velocity.
	a := centerX(tr.tracks[0].Box)
	tr.Update(nil)
	firstStep := centerX(tr.tracks[0].Box) - a
	if firstStep <= 0 {
		t.Fatalf("first coast step = %d, want forward motion", firstStep)
	}
	// After many coast frames the per-frame advance must collapse toward zero (the
	// trust cap): velocity decays geometrically, so the box eases to a standstill rather
	// than drifting off at constant speed. Robust to the exact predDecayX value.
	for i := 0; i < 25; i++ {
		tr.Update(nil)
	}
	c := centerX(tr.tracks[0].Box)
	tr.Update(nil)
	lateStep := centerX(tr.tracks[0].Box) - c
	if lateStep*3 >= firstStep {
		t.Errorf("late coast step %d not << first step %d — velocity didn't decay", lateStep, firstStep)
	}
}

func TestKalmanVerticalCoastShorterThanHorizontal(t *testing.T) {
	// A target moving equally fast in x and y: because vertical velocity is learned
	// cautiously and decays fast (people move horizontally; vertical is mostly artifact),
	// the box must coast much further horizontally than vertically through an occlusion.
	tr := &kalmanTracker{MaxMissed: 100}
	cx, cy := 100, 100
	for i := 0; i < 6; i++ {
		tr.Update([]Detection{{Box: rect(cx-10, cy-20, cx+10, cy+20), Score: 0.9}})
		cx += 20
		cy += 20
	}
	x0, y0 := center(tr.tracks[0].Box)
	for i := 0; i < 8; i++ {
		tr.Update(nil) // occlusion
	}
	x1, y1 := center(tr.tracks[0].Box)
	dx, dy := abs(x1-x0), abs(y1-y0)
	if dx <= dy {
		t.Errorf("horizontal coast %dpx not greater than vertical %dpx — per-axis decay/gain not applied", dx, dy)
	}
}

func center(r image.Rectangle) (int, int) {
	return (r.Min.X + r.Max.X) / 2, (r.Min.Y + r.Max.Y) / 2
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestKalmanByteTrackRecoversFromLowConf(t *testing.T) {
	tr := &kalmanTracker{HighConf: 0.5}
	// Establish a confident track.
	tr.Update([]Detection{{Box: boxAt(100, 100), Score: 0.9}})
	id := tr.tracks[0].ID

	// Next frame the detection is weak (0.3 < HighConf) and overlaps. Stage 1 finds no
	// high det, but stage 2 must re-anchor the track to the low det — Missed stays 0.
	out := tr.Update([]Detection{{Box: boxAt(103, 100), Score: 0.3}})
	if len(out) != 1 || out[0].ID != id {
		t.Fatalf("got %+v, want the same track id%d recovered", out, id)
	}
	if out[0].Missed != 0 {
		t.Errorf("Missed = %d, want 0 (low-tier detection should re-anchor the track)", out[0].Missed)
	}
}

func TestKalmanByteTrackLowConfNeverSpawns(t *testing.T) {
	tr := &kalmanTracker{HighConf: 0.5}
	// A lone weak detection with no existing track to continue must not create one —
	// otherwise noise spawns phantom locks.
	out := tr.Update([]Detection{{Box: boxAt(100, 100), Score: 0.3}})
	if len(out) != 0 {
		t.Errorf("got %+v, want no tracks (low-conf must not spawn)", out)
	}
}

func TestKalmanSmoothsJitter(t *testing.T) {
	tr := &kalmanTracker{}
	// A stationary target whose box jitters ±6px. The filtered center should sit
	// nearer the true center (100) than the raw measurements swing.
	jit := []int{106, 94, 105, 95, 103, 97}
	var out []Track
	for _, cx := range jit {
		out = tr.Update([]Detection{{Box: boxAt(cx, 100), Score: 0.9}})
	}
	if d := centerX(out[0].Box) - 100; d < -4 || d > 4 {
		t.Errorf("filtered center off by %dpx, want within ±4 of true 100 (jitter not smoothed)", d)
	}
}
