// Package detect runs a person-detection neural net on the video feed and tracks
// targets across frames, for the Run-screen target-lock / auto-aim. Inference runs
// on its own goroutine and publishes the latest tracks through a single-slot buffer
// (mirrors video.Buffer / state.Store — drop, never block), so it's fully decoupled
// from the UI and from the CRSF transmit loop.
//
// ONNX inference is Linux-only (detector_onnx.go); every other platform gets a
// no-op stub (detector_stub.go) so the Windows dev build stays cgo-free. The
// tracking / NMS / geometry below are pure Go and unit-tested everywhere.
package detect

import (
	"image"
	"sync"
	"time"

	"elrsctrl/internal/video"
)

// Detection is one person box in frame pixels with its confidence.
type Detection struct {
	Box   image.Rectangle
	Score float64
}

// Track is a Detection given a stable identity across frames (so a lock can follow
// one person). Missed counts consecutive frames the track wasn't matched.
type Track struct {
	ID     int
	Box    image.Rectangle
	Score  float64
	Age    int
	Missed int
}

// DetectorConfig configures a platform Detector (see NewDetector in the
// per-platform files).
type DetectorConfig struct {
	ModelPath string
	LibPath   string
	InputSize int
	Conf      float64
}

// Detector runs inference on a single frame. Implementations are not required to
// be safe for concurrent use; the Runner owns one.
type Detector interface {
	Detect(f *video.Frame) ([]Detection, error)
	Close() error
}

// TrackBuffer is the single-slot latest-tracks mailbox (like video.Buffer): the
// Runner sets, the UI reads the newest with Latest.
type TrackBuffer struct {
	mu     sync.Mutex
	tracks []Track
	seq    uint64
}

func (b *TrackBuffer) set(t []Track) {
	b.mu.Lock()
	b.tracks = t
	b.seq++
	b.mu.Unlock()
}

// Latest returns the newest tracks and a sequence number (nil, 0 before any run).
func (b *TrackBuffer) Latest() ([]Track, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tracks, b.seq
}

// Runner owns the detector + tracker and the inference goroutine.
type Runner struct {
	det     Detector
	src     *video.Buffer
	rate    int
	out     TrackBuffer
	tracker Tracker
	stop    chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	lastErr string
}

// NewRunner builds a Runner that pulls frames from src and detects at up to rateHz.
// Call Start to launch the goroutine.
func NewRunner(det Detector, src *video.Buffer, rateHz int) *Runner {
	if rateHz <= 0 {
		rateHz = 10
	}
	return &Runner{
		det:  det,
		src:  src,
		rate: rateHz,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// Start launches the inference goroutine.
func (r *Runner) Start() { go r.run() }

func (r *Runner) run() {
	defer close(r.done)
	t := time.NewTicker(time.Second / time.Duration(r.rate))
	defer t.Stop()
	var lastSeq uint64
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			frame, seq := r.src.Latest()
			if frame == nil || seq == lastSeq {
				continue // no new frame since last inference
			}
			lastSeq = seq
			dets, err := r.det.Detect(frame)
			if err != nil {
				r.setErr(err.Error())
				continue
			}
			r.out.set(r.tracker.Update(dets))
		}
	}
}

func (r *Runner) setErr(msg string) {
	r.mu.Lock()
	r.lastErr = msg
	r.mu.Unlock()
}

// Latest returns the newest tracks and their sequence number.
func (r *Runner) Latest() ([]Track, uint64) { return r.out.Latest() }

// Err returns the most recent detection error, or "".
func (r *Runner) Err() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

// Close stops the goroutine and releases the detector.
func (r *Runner) Close() error {
	close(r.stop)
	<-r.done
	return r.det.Close()
}
