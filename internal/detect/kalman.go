package detect

import "image"

// kalmanTracker is a constant-velocity tracker: each track carries a filtered
// center/size plus a center velocity, so it can (a) match a detection against where
// the target is *predicted* to be — not where it last was, which keeps a fast or
// camera-panned target's ID through a zero-IoU jump — and (b) coast *forward* along
// its velocity through a missed frame instead of freezing on the last box, so the
// aim loop reads a moving estimate rather than a stale one.
//
// It's a fixed-gain (alpha-beta) filter, not a full covariance Kalman: the standard
// SORT formulation tracks an 8-D state with covariance matrices, which would pull in
// matrix algebra we'd have to hand-roll (no stdlib linalg) and unit-test carefully.
// The fixed-gain form gives the same practical behaviour — predict, correct, smooth —
// in pure, easily-tested Go, matching this codebase's hand-rolled bias. The gains are
// the steady-state of that Kalman: positionGain corrects the predicted center toward
// the measurement; velocityGain learns the per-frame drift from the same residual.
//
// Prediction-trust cap: a target only stays detected for so long, so an unmatched
// track must not extrapolate forever (a confident way to aim at nothing). Each missed
// frame decays the velocity (per axis — see the gain block), so coasting fades toward a
// standstill well before MaxMissed drops the track entirely.
type kalmanTracker struct {
	IoUThresh float64 // min IoU to match a detection to a predicted box (default 0.2)
	MaxMissed int     // drop a track after this many consecutive unmatched frames (default 15)
	HighConf  float64 // detections >= this are the high tier (drive association + spawn); below it is the low tier (ByteTrack recovery only). 0 = treat all as high

	next   int
	tracks []ktrack
}

// ktrack is one tracked target: the published Track plus the filter's float state
// (the integer Track.Box is rounded from cx,cy,w,h each frame).
type ktrack struct {
	Track
	cx, cy float64 // filtered box center (px)
	w, h   float64 // filtered box size (px)
	vx, vy float64 // center velocity (px/frame)
}

// Filter gains, split per axis because a turret target moves almost entirely
// horizontally — vertical box motion is mostly artifact (the subject clipped at the
// frame edge when close, or the whole scene sliding under tilt). So we both learn
// vertical velocity cautiously and coast it briefly, while trusting and riding
// horizontal velocity far longer:
//
//   - positionGain corrects the predicted center toward the measurement (both axes —
//     we still track real vertical position, we just don't over-predict its velocity).
//   - velocityGainX/Y learn per-frame drift from the residual (β of the alpha-beta
//     pair). Y is half of X so clipping/ego-motion spikes don't become phantom motion.
//   - sizeGain smooths w/h (no velocity term — target size drifts slowly).
//   - predDecayX/Y bleed velocity away while coasting (the prediction-trust cap). X is
//     high (long lead through a horizontal occlusion); Y is low (vertical lead dies
//     fast, so a clipped/tilting box doesn't drag the aim point up or down).
const (
	positionGain  = 0.5
	velocityGainX = 0.2
	velocityGainY = 0.1
	sizeGain      = 0.5
	predDecayX    = 0.85
	predDecayY    = 0.5
)

func (t *kalmanTracker) Update(dets []Detection) []Track {
	iouThresh := t.IoUThresh
	if iouThresh <= 0 {
		iouThresh = 0.2
	}
	maxMissed := t.MaxMissed
	if maxMissed <= 0 {
		maxMissed = 15
	}

	// Predict: advance every track's center along its velocity, then match against
	// these predicted boxes (not the last measured ones).
	for i := range t.tracks {
		t.tracks[i].cx += t.tracks[i].vx
		t.tracks[i].cy += t.tracks[i].vy
		t.tracks[i].Box = boxFromCenter(t.tracks[i].cx, t.tracks[i].cy, t.tracks[i].w, t.tracks[i].h)
	}

	// ByteTrack two-stage association. Split detections into a high tier (confident)
	// and a low tier (weak — e.g. a target that turned or is half-occluded). Stage 1
	// matches tracks to high dets; stage 2 lets *still-unmatched* tracks re-anchor to a
	// low det rather than coast blind. Low dets never spawn new tracks, so transient
	// noise can continue an existing lock but can't create a phantom one.
	high, low := splitByConf(dets, t.HighConf)
	matched := make([]bool, len(dets))
	trackMatched := make([]bool, len(t.tracks))

	for i := range t.tracks { // stage 1: high tier
		if j := bestMatch(t.tracks[i].Box, high, dets, matched, iouThresh); j >= 0 {
			matched[j], trackMatched[i] = true, true
			t.correct(&t.tracks[i], dets[j])
		}
	}
	for i := range t.tracks { // stage 2: recover leftover tracks from the low tier
		if trackMatched[i] {
			continue
		}
		if j := bestMatch(t.tracks[i].Box, low, dets, matched, iouThresh); j >= 0 {
			matched[j], trackMatched[i] = true, true
			t.correct(&t.tracks[i], dets[j])
		}
	}
	for i := range t.tracks { // coast whatever's still unmatched
		if trackMatched[i] {
			continue
		}
		// Keep the already-predicted box, but bleed off velocity so an unmatched track
		// decelerates toward a standstill rather than flying away — vertical faster than
		// horizontal (see the gain block).
		t.tracks[i].vx *= predDecayX
		t.tracks[i].vy *= predDecayY
		t.tracks[i].Missed++
	}

	// Drop tracks missing too long.
	alive := t.tracks[:0]
	for _, tr := range t.tracks {
		if tr.Missed <= maxMissed {
			alive = append(alive, tr)
		}
	}
	t.tracks = alive

	// Spawn a track only for each unmatched *high* detection (zero initial velocity).
	for _, j := range high {
		if matched[j] {
			continue
		}
		t.next++
		mcx, mcy, mw, mh := centerOf(dets[j].Box)
		t.tracks = append(t.tracks, ktrack{
			Track: Track{ID: t.next, Box: dets[j].Box, Score: dets[j].Score, Age: 1},
			cx:    mcx, cy: mcy, w: mw, h: mh,
		})
	}

	out := make([]Track, len(t.tracks))
	for i := range t.tracks {
		out[i] = t.tracks[i].Track
	}
	return out
}

// splitByConf partitions detection indices into a high tier (score >= highConf) and a
// low tier (below it). highConf <= 0 puts everything in the high tier (single-stage).
func splitByConf(dets []Detection, highConf float64) (high, low []int) {
	for j := range dets {
		if highConf <= 0 || dets[j].Score >= highConf {
			high = append(high, j)
		} else {
			low = append(low, j)
		}
	}
	return
}

// bestMatch returns the index (into dets) of the best unclaimed candidate for a box:
// highest IoU above iouThresh, else the nearest centroid within ~one box-size (the
// ego-motion fallback). cands is the tier (high or low) to search. -1 if none.
func bestMatch(box image.Rectangle, cands []int, dets []Detection, matched []bool, iouThresh float64) int {
	best, bestIoU := -1, iouThresh
	for _, j := range cands {
		if matched[j] {
			continue
		}
		if v := iou(box, dets[j].Box); v >= bestIoU {
			best, bestIoU = j, v
		}
	}
	if best < 0 {
		bestD := float64(maxDim(box))
		for _, j := range cands {
			if matched[j] {
				continue
			}
			if d := centroidDist(box, dets[j].Box); d < bestD {
				best, bestD = j, d
			}
		}
	}
	return best
}

// correct folds a matched detection into a track's filter state: the position
// residual (measurement − prediction) nudges the center by positionGain and teaches
// the velocity by velocityGain (alpha-beta update); size is smoothed by sizeGain.
func (t *kalmanTracker) correct(tr *ktrack, det Detection) {
	mcx, mcy, mw, mh := centerOf(det.Box)
	rx, ry := mcx-tr.cx, mcy-tr.cy
	tr.cx += positionGain * rx
	tr.cy += positionGain * ry
	tr.vx += velocityGainX * rx
	tr.vy += velocityGainY * ry
	tr.w += sizeGain * (mw - tr.w)
	tr.h += sizeGain * (mh - tr.h)
	tr.Box = boxFromCenter(tr.cx, tr.cy, tr.w, tr.h)
	tr.Score = det.Score
	tr.Age++
	tr.Missed = 0
}

// centerOf returns a box's center and size as floats.
func centerOf(r image.Rectangle) (cx, cy, w, h float64) {
	w, h = float64(r.Dx()), float64(r.Dy())
	cx = float64(r.Min.X) + w/2
	cy = float64(r.Min.Y) + h/2
	return
}

// boxFromCenter rebuilds an integer box from filtered center/size.
func boxFromCenter(cx, cy, w, h float64) image.Rectangle {
	return image.Rect(
		round(cx-w/2), round(cy-h/2),
		round(cx+w/2), round(cy+h/2),
	)
}

func round(f float64) int {
	if f < 0 {
		return -int(-f + 0.5)
	}
	return int(f + 0.5)
}
