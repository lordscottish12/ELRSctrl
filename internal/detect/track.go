package detect

import (
	"image"
	"math"
	"sort"
)

// iou is the intersection-over-union of two boxes (0 = disjoint, 1 = identical).
func iou(a, b image.Rectangle) float64 {
	inter := a.Intersect(b)
	if inter.Empty() {
		return 0
	}
	ia := area(inter)
	union := area(a) + area(b) - ia
	if union <= 0 {
		return 0
	}
	return float64(ia) / float64(union)
}

func area(r image.Rectangle) int { return r.Dx() * r.Dy() }

// centroidDist is the pixel distance between two boxes' centers.
func centroidDist(a, b image.Rectangle) float64 {
	dx := float64((a.Min.X+a.Max.X)-(b.Min.X+b.Max.X)) / 2
	dy := float64((a.Min.Y+a.Max.Y)-(b.Min.Y+b.Max.Y)) / 2
	return math.Sqrt(dx*dx + dy*dy)
}

func maxDim(r image.Rectangle) int {
	if r.Dx() > r.Dy() {
		return r.Dx()
	}
	return r.Dy()
}

// nms suppresses overlapping detections, keeping the highest-scoring box in each
// cluster (boxes overlapping a kept one by more than thresh are dropped).
func nms(dets []Detection, thresh float64) []Detection {
	sort.SliceStable(dets, func(i, j int) bool { return dets[i].Score > dets[j].Score })
	suppressed := make([]bool, len(dets))
	var keep []Detection
	for i := range dets {
		if suppressed[i] {
			continue
		}
		keep = append(keep, dets[i])
		for j := i + 1; j < len(dets); j++ {
			if !suppressed[j] && iou(dets[i].Box, dets[j].Box) > thresh {
				suppressed[j] = true
			}
		}
	}
	return keep
}

// Tracker assigns stable IDs to detections across frames so a locked target keeps
// its identity even as its box jitters, jumps under camera motion, or briefly drops
// out. Matching prefers IoU but falls back to centroid proximity: when the camera
// pans the whole scene shifts and a box can slide clear of its previous overlap, yet
// it's still the same person — the nearest box within ~one box-size is treated as a
// match so the ID (and the lock) survives the rotation.
type Tracker struct {
	IoUThresh float64 // min IoU to consider a detection the same target (default 0.2)
	MaxMissed int     // drop a track after this many consecutive unmatched frames (default 15)

	next   int
	tracks []Track
}

// Update folds this frame's detections into the track set and returns the live
// tracks (those still within MaxMissed). Currently-visible tracks have Missed == 0;
// a track that wasn't matched this frame coasts on its last box with Missed > 0.
func (t *Tracker) Update(dets []Detection) []Track {
	iouThresh := t.IoUThresh
	if iouThresh <= 0 {
		iouThresh = 0.2
	}
	maxMissed := t.MaxMissed
	if maxMissed <= 0 {
		maxMissed = 15
	}

	matched := make([]bool, len(dets))
	// Greedily match each existing track to its best unclaimed detection: highest IoU
	// above threshold, else the nearest centroid within ~one box-size (ego-motion).
	for i := range t.tracks {
		best, bestIoU := -1, iouThresh
		for j := range dets {
			if matched[j] {
				continue
			}
			if v := iou(t.tracks[i].Box, dets[j].Box); v >= bestIoU {
				best, bestIoU = j, v
			}
		}
		if best < 0 {
			gate := float64(maxDim(t.tracks[i].Box))
			bestD := gate
			for j := range dets {
				if matched[j] {
					continue
				}
				if d := centroidDist(t.tracks[i].Box, dets[j].Box); d < bestD {
					best, bestD = j, d
				}
			}
		}
		if best >= 0 {
			matched[best] = true
			t.tracks[i].Box = dets[best].Box
			t.tracks[i].Score = dets[best].Score
			t.tracks[i].Age++
			t.tracks[i].Missed = 0
		} else {
			t.tracks[i].Missed++
		}
	}

	// Drop tracks that have been missing too long.
	alive := t.tracks[:0]
	for _, tr := range t.tracks {
		if tr.Missed <= maxMissed {
			alive = append(alive, tr)
		}
	}
	t.tracks = alive

	// Spawn tracks for unmatched detections.
	for j := range dets {
		if matched[j] {
			continue
		}
		t.next++
		t.tracks = append(t.tracks, Track{ID: t.next, Box: dets[j].Box, Score: dets[j].Score, Age: 1})
	}

	out := make([]Track, len(t.tracks))
	copy(out, t.tracks)
	return out
}
