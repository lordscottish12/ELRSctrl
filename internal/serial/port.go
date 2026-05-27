// Package serial is a thin wrapper over go.bug.st/serial providing port
// enumeration and a CRSF-friendly open (configurable baud, 8N1). Lifecycle and
// reconnection are managed by the sender, so this stays intentionally minimal.
package serial

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// PortInfo describes a serial port for the UI picker. Device is what you pass to
// Open (e.g. "/dev/ttyUSB0" or "COM5"); Label is a human-friendly name derived
// from /dev/serial/by-id when available (Linux), otherwise just the device.
type PortInfo struct {
	Device string
	Label  string
}

// ListPorts enumerates serial ports with friendly labels, and hides devices that
// are never a CRSF target — notably the Steam Deck's own built-in Steam
// Controller, which exposes a /dev/ttyACM serial interface that looks pickable
// but silently swallows writes: opening it "succeeds" yet nothing reaches the TX
// module. Keep it out of the picker so it can't be selected by mistake.
func ListPorts() ([]PortInfo, error) {
	devices, err := gs.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("list serial ports: %w", err)
	}
	byID := serialByID() // device path -> /dev/serial/by-id descriptor (Linux; nil elsewhere)
	out := make([]PortInfo, 0, len(devices))
	for _, dev := range devices {
		id := byID[dev]
		if isExcludedPort(id) {
			continue
		}
		label := dev
		if id != "" {
			label = friendlyName(id)
		}
		out = append(out, PortInfo{Device: dev, Label: label})
	}
	return out, nil
}

// serialByID maps each resolved /dev/ttyXXX to its /dev/serial/by-id descriptor
// name. Linux-only; returns nil where that tree doesn't exist (e.g. Windows).
func serialByID() map[string]string {
	const dir = "/dev/serial/by-id"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		target, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		m[target] = e.Name()
	}
	return m
}

// isExcludedPort reports whether a by-id descriptor names a device that should
// never appear in the picker. The Steam Deck's built-in controller is the one we
// know bites (see ListPorts).
func isExcludedPort(byID string) bool {
	return strings.Contains(strings.ToLower(byID), "steam_controller")
}

// friendlyName turns a /dev/serial/by-id descriptor like
// "usb-Silicon_Labs_CP2102_USB_to_UART_Bridge_Controller_0001-if00-port0" into
// "Silicon Labs CP2102 USB to UART Bridge Controller 0001".
func friendlyName(byID string) string {
	s := strings.TrimPrefix(byID, "usb-")
	if i := strings.Index(s, "-if"); i > 0 {
		s = s[:i] // drop the trailing interface/port suffix
	}
	return strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
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
