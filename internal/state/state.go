// Package state holds the thread-safe snapshot handed from the UI loop to the
// serial sender goroutine, plus connection/telemetry the UI reads back.
package state

import (
	"sync"
	"sync/atomic"
	"time"

	"dronectrl/internal/crsf"
)

// Snapshot is the latest control output the sender should transmit.
type Snapshot struct {
	Live      [crsf.NumChannels]uint16 // mapped channel values
	Failsafe  [crsf.NumChannels]uint16 // values to send when not safe to drive
	Armed     bool                     // user has armed the car
	InputOK   bool                     // gamepad connected and reporting
	UpdatedAt time.Time                // when the UI last refreshed this
}

// Store is the shared, concurrency-safe state between UI and sender.
type Store struct {
	mu   sync.RWMutex
	snap Snapshot

	portConnected atomic.Bool
	portName      atomic.Value // string
	lastErr       atomic.Value // string
	txCount       atomic.Uint64
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

func (s *Store) PortConnected() bool { return s.portConnected.Load() }
func (s *Store) PortName() string    { v, _ := s.portName.Load().(string); return v }
func (s *Store) LastError() string   { v, _ := s.lastErr.Load().(string); return v }
func (s *Store) TxCount() uint64      { return s.txCount.Load() }
