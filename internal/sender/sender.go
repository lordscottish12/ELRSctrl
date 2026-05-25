// Package sender owns the serial link lifecycle and transmits CRSF RC frames at
// a steady rate, independent of the UI frame rate. It is the last line of
// safety: it sends failsafe values whenever the car is disarmed, the gamepad is
// gone, or the UI snapshot is stale (e.g. the UI thread stalled).
package sender

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"dronectrl/internal/crsf"
	"dronectrl/internal/serial"
	"dronectrl/internal/state"
)

// Config holds the transmit parameters.
type Config struct {
	Addr         byte          // CRSF destination address (0xEE transmitter)
	RateHz       int           // frame rate, e.g. 250
	StaleTimeout time.Duration // snapshot older than this -> failsafe
}

// Sender transmits frames from a Store to a serial port.
type Sender struct {
	cfg   Config
	store *state.Store

	addr atomic.Uint32 // CRSF address, changeable at runtime

	mu         sync.Mutex
	portName   string
	baud       int
	reopen     bool
	resetPulse bool

	// Owned solely by the Run goroutine:
	port            *serial.Port
	lastOpenAttempt time.Time
}

// New creates a Sender. portName may be empty (the sender will keep retrying).
// resetPulse controls whether the EN reset is pulsed on each open (see serial.Open).
func New(cfg Config, store *state.Store, portName string, baud int, resetPulse bool) *Sender {
	if cfg.RateHz <= 0 {
		cfg.RateHz = 250
	}
	if cfg.StaleTimeout <= 0 {
		cfg.StaleTimeout = 250 * time.Millisecond
	}
	if cfg.Addr == 0 {
		cfg.Addr = crsf.AddrTransmitter
	}
	s := &Sender{cfg: cfg, store: store, portName: portName, baud: baud, resetPulse: resetPulse}
	s.addr.Store(uint32(cfg.Addr))
	return s
}

// SetPort changes the target port/baud at runtime; the Run loop reopens.
func (s *Sender) SetPort(name string, baud int) {
	s.mu.Lock()
	s.portName, s.baud, s.reopen = name, baud, true
	s.mu.Unlock()
}

// SetAddr changes the CRSF destination address at runtime.
func (s *Sender) SetAddr(addr byte) { s.addr.Store(uint32(addr)) }

// Run drives the transmit loop until ctx is cancelled, then sends a short burst
// of failsafe frames so the car reliably stops.
func (s *Sender) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second / time.Duration(s.cfg.RateHz))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.failsafeBurst()
			s.closePort()
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Sender) tick() {
	s.ensurePort()
	snap := s.store.Snapshot()
	ch := snap.Live
	safe := snap.Armed && snap.InputOK && time.Since(snap.UpdatedAt) <= s.cfg.StaleTimeout
	if !safe {
		ch = snap.Failsafe
	}
	s.writeFrame(crsf.BuildRCFrame(byte(s.addr.Load()), ch))
}

// ensurePort opens (or reopens) the serial port, rate-limiting failed attempts.
func (s *Sender) ensurePort() {
	s.mu.Lock()
	name, baud, reopen := s.portName, s.baud, s.reopen
	s.reopen = false
	s.mu.Unlock()

	if reopen {
		s.closePort()
	}
	if s.port != nil {
		return
	}
	if time.Since(s.lastOpenAttempt) < time.Second {
		return
	}
	s.lastOpenAttempt = time.Now()

	p, err := serial.Open(name, baud, s.resetPulse)
	if err != nil {
		s.store.SetPortStatus(false, name, err.Error())
		return
	}
	s.port = p
	s.store.SetPortStatus(true, name, "")
}

func (s *Sender) writeFrame(f []byte) {
	if s.port == nil {
		return
	}
	if err := s.port.Write(f); err != nil {
		s.store.SetPortStatus(false, s.port.Name(), err.Error())
		s.closePort()
		return
	}
	s.store.IncTx()
}

func (s *Sender) closePort() {
	if s.port != nil {
		_ = s.port.Close()
		s.port = nil
	}
}

func (s *Sender) failsafeBurst() {
	if s.port == nil {
		return
	}
	f := crsf.BuildRCFrame(byte(s.addr.Load()), s.store.Snapshot().Failsafe)
	for i := 0; i < 5; i++ {
		_ = s.port.Write(f)
		time.Sleep(4 * time.Millisecond)
	}
}

// Sweep is a standalone hardware bring-up mode (no UI/Store): it transmits a
// triangle wave on the given 1-based channel while holding the rest centered, so
// you can confirm a servo/ESC on the bound receiver moves before building a
// mapping. Blocks until ctx is cancelled, then centers all channels.
func Sweep(ctx context.Context, portName string, baud int, addr byte, channel, rateHz int, period time.Duration, resetPulse bool) error {
	p, err := serial.Open(portName, baud, resetPulse)
	if err != nil {
		return err
	}
	defer p.Close()

	ticker := time.NewTicker(time.Second / time.Duration(rateHz))
	defer ticker.Stop()
	idx := channel - 1
	start := time.Now()

	centered := func() [crsf.NumChannels]uint16 {
		var ch [crsf.NumChannels]uint16
		for i := range ch {
			ch[i] = crsf.TicksMid
		}
		return ch
	}

	for {
		select {
		case <-ctx.Done():
			_ = p.Write(crsf.BuildRCFrame(addr, centered()))
			return nil
		case <-ticker.C:
			phase := math.Mod(time.Since(start).Seconds(), period.Seconds()) / period.Seconds()
			tri := 1 - math.Abs(2*phase-1) // 0 -> 1 -> 0 ramp
			ch := centered()
			if idx >= 0 && idx < crsf.NumChannels {
				ch[idx] = crsf.TicksMin + uint16(tri*float64(crsf.TicksMax-crsf.TicksMin))
			}
			_ = p.Write(crsf.BuildRCFrame(addr, ch))
		}
	}
}
