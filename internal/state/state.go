// Package state holds the thread-safe snapshot handed from the UI loop to the
// serial sender goroutine, plus connection/telemetry the UI reads back.
package state

import (
	"sync"
	"sync/atomic"
	"time"

	"elrsctrl/internal/crsf"
)

// Snapshot is the latest control output the sender should transmit.
type Snapshot struct {
	Live      [crsf.NumChannels]uint16 // mapped channel values
	Failsafe  [crsf.NumChannels]uint16 // values to send when not safe to drive
	Armed     bool                     // user has armed the car
	InputOK   bool                     // gamepad connected and reporting
	UpdatedAt time.Time                // when the UI last refreshed this
}

// Telemetry is the latest ELRS link/battery data parsed from inbound CRSF frames
// (written by the sender's reader goroutine, read by the UI). The *At timestamps
// let the UI judge freshness / connection health.
type Telemetry struct {
	LinkValid  bool
	UplinkLQ   uint8 // link quality 0-100 %
	UplinkRSSI int   // dBm (negative)
	UplinkSNR  int8
	RFMode     uint8
	TXPowerMW  int

	BatteryValid bool
	Voltage      float64 // V
	Current      float64 // A

	LinkAt  time.Time // last LINK_STATISTICS frame
	BattAt  time.Time // last BATTERY_SENSOR frame
	FrameAt time.Time // last telemetry frame of any type
}

// Store is the shared, concurrency-safe state between UI and sender.
type Store struct {
	mu   sync.RWMutex
	snap Snapshot

	portConnected atomic.Bool
	portName      atomic.Value // string
	lastErr       atomic.Value // string
	txCount       atomic.Uint64

	telMu     sync.Mutex
	tel       Telemetry
	telFrames atomic.Uint64
}

// New returns a Store seeded with a centered, disarmed snapshot.
func New() *Store {
	s := &Store{}
	var snap Snapshot
	for i := range snap.Live {
		snap.Live[i] = crsf.TicksMid
		snap.Failsafe[i] = crsf.TicksMid
	}
	snap.UpdatedAt = time.Now()
	s.snap = snap
	s.portName.Store("")
	s.lastErr.Store("")
	return s
}

// SetSnapshot stores the latest control output (called from the UI loop).
func (s *Store) SetSnapshot(snap Snapshot) {
	snap.UpdatedAt = time.Now()
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

// Snapshot returns a copy of the latest control output (called from the sender).
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// --- Sender-reported telemetry (read by the UI) ---

func (s *Store) SetPortStatus(connected bool, name, errMsg string) {
	s.portConnected.Store(connected)
	s.portName.Store(name)
	s.lastErr.Store(errMsg)
}

func (s *Store) IncTx() { s.txCount.Add(1) }

// --- Inbound telemetry (written by the sender's reader, read by the UI) ---

// SetTelemetry stores the latest telemetry. The reader is the sole writer, so it
// can read-modify-write via Telemetry()/SetTelemetry without losing updates.
func (s *Store) SetTelemetry(t Telemetry) {
	s.telMu.Lock()
	s.tel = t
	s.telMu.Unlock()
}

// Telemetry returns a copy of the latest telemetry.
func (s *Store) Telemetry() Telemetry {
	s.telMu.Lock()
	defer s.telMu.Unlock()
	return s.tel
}

// IncTelemetryFrames counts every parsed telemetry frame, so the UI can show
// whether anything is arriving at all (the key "is telemetry working?" diagnostic).
func (s *Store) IncTelemetryFrames()        { s.telFrames.Add(1) }
func (s *Store) TelemetryFrames() uint64    { return s.telFrames.Load() }

func (s *Store) PortConnected() bool { return s.portConnected.Load() }
func (s *Store) PortName() string    { v, _ := s.portName.Load().(string); return v }
func (s *Store) LastError() string   { v, _ := s.lastErr.Load().(string); return v }
func (s *Store) TxCount() uint64      { return s.txCount.Load() }
