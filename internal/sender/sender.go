// Package sender owns the serial link lifecycle and transmits CRSF RC frames at
// a steady rate, independent of the UI frame rate. It is the last line of
// safety: it sends failsafe values whenever the car is disarmed, the gamepad is
// gone, or the UI snapshot is stale (e.g. the UI thread stalled).
package sender

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"elrsctrl/internal/crsf"
	"elrsctrl/internal/serial"
	"elrsctrl/internal/state"
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
		// An explicit port change (Settings → pick/Save) should take effect now,
		// not wait out the failed-reconnect throttle below, so the Monitor reflects
		// the new port immediately.
		s.closePort()
		s.lastOpenAttempt = time.Time{}
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
	// Side-channel telemetry reader, bound to this port instance. It ends when the
	// port is closed/reopened (Read errors), so there's exactly one per open and it
	// never touches the transmit path below.
	go s.readTelemetry(p)
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

// readTelemetry drains inbound CRSF frames from p (the ELRS module relays link
// stats / battery back over the handset UART) and folds them into the Store. It is
// a side channel: full-duplex serial means it runs fully independently of the
// transmit loop. It exits when p is closed/reopened (Read returns an error).
func (s *Sender) readTelemetry(p *serial.Port) {
	_ = p.SetReadTimeout(200 * time.Millisecond) // wake periodically; (0,nil) on idle
	var parser crsf.Parser
	buf := make([]byte, 256)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			parser.Push(buf[:n], s.applyTelemetry)
		}
		if err != nil {
			return // port closed/reopened
		}
	}
}

// applyTelemetry folds one parsed frame into the Store's Telemetry. The reader is
// the sole writer, so the read-modify-write is race-free.
func (s *Sender) applyTelemetry(typ byte, payload []byte) {
	s.store.IncTelemetryFrames()
	now := time.Now()
	switch typ {
	case crsf.FrameTypeLinkStats:
		ls, ok := crsf.DecodeLinkStats(payload)
		if !ok {
			return
		}
		t := s.store.Telemetry()
		t.LinkValid = true
		t.UplinkLQ = ls.UplinkLQ
		t.UplinkRSSI = -int(ls.UplinkRSSI1)
		t.UplinkSNR = ls.UplinkSNR
		t.RFMode = ls.RFMode
		t.TXPowerMW = crsf.TXPowerMW(ls.TXPowerIdx)
		t.LinkAt, t.FrameAt = now, now
		s.store.SetTelemetry(t)
	case crsf.FrameTypeBattery:
		b, ok := crsf.DecodeBattery(payload)
		if !ok {
			return
		}
		t := s.store.Telemetry()
		t.BatteryValid = true
		t.Voltage = b.Voltage
		t.Current = b.Current
		t.BattAt, t.FrameAt = now, now
		s.store.SetTelemetry(t)
	default:
		// counted above (TelemetryFrames) but not surfaced — still proves the link.
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
//
// Writes run on a dedicated goroutine fed by a small drop-if-full queue, so a
// stalled write (the link not draining — e.g. an ELRS module that never starts
// reading the port) can't freeze the build loop. A twice-a-second heartbeat
// reports whether frames are landing, stalling, or erroring, which is the whole
// point of bring-up — the old version swallowed write errors silently.
func Sweep(ctx context.Context, portName string, baud int, addr byte, channel, rateHz int, period time.Duration, resetPulse bool) error {
	p, err := serial.Open(portName, baud, resetPulse)
	if err != nil {
		return err
	}
	defer p.Close()

	idx := channel - 1
	centered := func() [crsf.NumChannels]uint16 {
		var ch [crsf.NumChannels]uint16
		for i := range ch {
			ch[i] = crsf.TicksMid
		}
		return ch
	}

	// Writer goroutine owns every port write. inFlight holds the start time of an
	// in-progress write (0 when idle) so the reporter can spot a write that's
	// blocked because the device isn't draining its buffer.
	frames := make(chan []byte, 8)
	var sent, failed, dropped atomic.Uint64
	var inFlight atomic.Int64
	var lastErr atomic.Value // string
	lastErr.Store("")
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for f := range frames {
			inFlight.Store(time.Now().UnixNano())
			err := p.Write(f)
			inFlight.Store(0)
			if err != nil {
				failed.Add(1)
				lastErr.Store(err.Error())
			} else {
				sent.Add(1)
			}
		}
		_ = p.Write(crsf.BuildRCFrame(addr, centered())) // park centered on the way out
	}()

	ticker := time.NewTicker(time.Second / time.Duration(rateHz))
	defer ticker.Stop()
	report := time.NewTicker(500 * time.Millisecond)
	defer report.Stop()
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			close(frames)
			<-writerDone
			return nil
		case <-report.C:
			msg := fmt.Sprintf("tx: %d ok, %d dropped, %d failed", sent.Load(), dropped.Load(), failed.Load())
			if e, _ := lastErr.Load().(string); e != "" {
				msg += " (last err: " + e + ")"
			}
			if t := inFlight.Load(); t != 0 {
				if d := time.Since(time.Unix(0, t)); d > 200*time.Millisecond {
					msg += fmt.Sprintf("  <-- WRITE STALLED %dms (device not reading this port?)", d.Milliseconds())
				}
			}
			log.Print(msg)
		case <-ticker.C:
			phase := math.Mod(time.Since(start).Seconds(), period.Seconds()) / period.Seconds()
			tri := 1 - math.Abs(2*phase-1) // 0 -> 1 -> 0 ramp
			ch := centered()
			if idx >= 0 && idx < crsf.NumChannels {
				ch[idx] = crsf.TicksMin + uint16(tri*float64(crsf.TicksMax-crsf.TicksMin))
			}
			select {
			case frames <- crsf.BuildRCFrame(addr, ch):
			default:
				dropped.Add(1) // writer is blocked; don't let it back up the loop
			}
		}
	}
}
