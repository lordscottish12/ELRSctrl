// Package serial is a thin wrapper over go.bug.st/serial providing port
// enumeration and a CRSF-friendly open (configurable baud, 8N1). Lifecycle and
// reconnection are managed by the sender, so this stays intentionally minimal.
package serial

import (
	"fmt"
	"time"

	gs "go.bug.st/serial"
)

// List returns the names of serial ports currently present on the system.
func List() ([]string, error) {
	ports, err := gs.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("list serial ports: %w", err)
	}
	return ports, nil
}

// Port is an open serial connection.
type Port struct {
	name string
	p    gs.Port
}

// Open opens name at baud with the CRSF wire format (8 data bits, no parity,
// one stop bit). When resetPulse is true it pulses a clean EN reset after open
// (see below) — needed for ESP modules reached over their flashing UART, opt-out
// for other links.
func Open(name string, baud int, resetPulse bool) (*Port, error) {
	if name == "" {
		return nil, fmt.Errorf("no serial port configured")
	}
	mode := &gs.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   gs.NoParity,
		StopBits: gs.OneStopBit,
	}
	p, err := gs.Open(name, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s @ %d: %w", name, baud, err)
	}
	if resetPulse {
		// ESP-based modules whose USB bridge sits on the flashing UART auto-reset
		// from the DTR/RTS lines (DTR->GPIO0, RTS->EN). The OS asserts both when the
		// port opens, which drops the chip into the bootloader so the firmware never
		// runs. Recover deterministically: hold GPIO0 high (DTR off) and pulse EN
		// (RTS) so the chip reboots into the application, then give it time to come
		// up before we start streaming. Harmless on links where RTS/DTR aren't wired
		// to the MCU (CRSF uses no flow control), but adds ~450ms to open, so it's
		// gated behind the --reset-pulse flag.
		_ = p.SetDTR(false)
		_ = p.SetRTS(false)
		time.Sleep(50 * time.Millisecond)
		_ = p.SetRTS(true) // EN low -> reset
		time.Sleep(100 * time.Millisecond)
		_ = p.SetRTS(false) // EN high -> boot the app (GPIO0 still high)
		time.Sleep(300 * time.Millisecond)
	}
	return &Port{name: name, p: p}, nil
}

// Name returns the port name.
func (p *Port) Name() string { return p.name }

// Write sends a full frame to the port.
func (p *Port) Write(b []byte) error {
	_, err := p.p.Write(b)
	if err != nil {
		return fmt.Errorf("write %s: %w", p.name, err)
	}
	return nil
}

// Close releases the port.
func (p *Port) Close() error {
	if p.p == nil {
		return nil
	}
	return p.p.Close()
}
