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
	"fmt"
	"image"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"elrsctrl/internal/video"
)

// detectDebugEnv lets DETECT_DEBUG=1 force per-inference logging on regardless of the
// in-app toggle (back-compat / headless capture).
var detectDebugEnv = os.Getenv("DETECT_DEBUG") != ""

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
	rate    atomic.Int64 // max inference rate (Hz); live-settable so the UI can retune without a model reload
	out     TrackBuffer
	tracker Tracker
	stop    chan struct{}
	done    chan struct{}
	debug   atomic.Bool // log per-frame tracks; live-settable from the UI (or DETECT_DEBUG)

	mu      sync.Mutex
	lastErr string
}

// NewRunner builds a Runner that pulls frames from src and detects at up to rateHz.
// Call Start to launch the goroutine.
func NewRunner(det Detector, src *video.Buffer, rateHz int) *Runner {
	if rateHz <= 0 {
		rateHz = 10
	}
	r := &Runner{
		det:  det,
		src:  src,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	r.rate.Store(int64(rateHz))
	r.debug.Store(detectDebugEnv)
	return r
}

// SetDebug toggles per-inference dets/tracks logging while the runner is live (the env
// var, if set, still forces it on). Safe to call from the UI goroutine.
func (r *Runner) SetDebug(on bool) { r.debug.Store(on || detectDebugEnv) }

// SetRate changes the max inference rate (Hz) live — the run loop resets its ticker on
// the next tick — so the UI can retune detection rate without rebuilding the detector
// (no ONNX reload). Ignored if hz <= 0. Safe to call from the UI goroutine.
func (r *Runner) SetRate(hz int) {
	if hz > 0 {
		r.rate.Store(int64(hz))
	}
}

// Start launches the inference goroutine.
func (r *Runner) Start() { go r.run() }

func (r *Runner) run() {
	defer close(r.done)
	rate := int(r.rate.Load())
	t := time.NewTicker(time.Second / time.Duration(rate))
	defer t.Stop()
	var lastSeq uint64
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			if nr := int(r.rate.Load()); nr != rate && nr > 0 {
				rate = nr // rate changed live (SetRate) — re-pace the ticker
				t.Reset(time.Second / time.Duration(rate))
			}
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
			tracks := r.tracker.Update(dets)
			if r.debug.Load() {
				logTracks(frame, dets, tracks)
			}
			r.out.set(tracks)
		}
	}
}

// logTracks dumps the detection count and each live track (id, box center, missed/age)
// for one inference under DETECT_DEBUG, so a lock that's "lost" can be traced to the
// tracker dropping it vs. detection finding nothing.
func logTracks(f *video.Frame, dets []Detection, tracks []Track) {
	parts := make([]string, len(tracks))
	for i, t := range tracks {
		cx := (t.Box.Min.X + t.Box.Max.X) / 2
		cy := (t.Box.Min.Y + t.Box.Max.Y) / 2
		parts[i] = fmt.Sprintf("#%d c=(%d,%d) miss=%d age=%d", t.ID, cx, cy, t.Missed, t.Age)
	}
	log.Printf("detect: %dx%d dets=%d tracks=%d [%s]", f.W, f.H, len(dets), len(tracks), strings.Join(parts, " "))
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
