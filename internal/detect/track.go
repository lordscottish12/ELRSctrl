package detect

import (
	"image"
	"math"
	"sort"
)

// Box geometry + NMS shared by the detector and the kalman tracker. (The original
// greedy IoU Tracker that also lived here was retired once the kalman tracker —
// constant-velocity prediction + ByteTrack association — was validated on the Deck.)

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
