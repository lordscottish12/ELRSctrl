// Package channels turns a gamepad input snapshot into 16 CRSF channel values
// according to a user-defined, fully remappable mapping.
package channels

import (
	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

// MapType selects how a channel derives its value from its source(s).
type MapType string

const (
	TypeNone    MapType = "none"    // channel holds center (992)
	TypeAnalog  MapType = "analog"  // proportional from an analog source
	TypeSwitch2 MapType = "switch2" // one button -> Low/High (see PressMode)
	TypeSwitch3 MapType = "switch3" // two buttons -> Low/Mid/High
	TypeFixed   MapType = "fixed"   // constant value
)

// PressMode selects how a switch2 channel responds to its button.
type PressMode string

const (
	PressToggle    PressMode = "toggle"    // click flips Low<->High; default
	PressMomentary PressMode = "momentary" // held = High, released = Low
	PressPulse     PressMode = "pulse"     // held = oscillate Low<->High at PulseHz; released = Low
)

// AnalogMode selects how an analog channel turns its source into a value.
type AnalogMode string

const (
	// ModePosition maps the source directly to an absolute position; centering
	// the stick restores center. Default (empty string is treated as this).
	ModePosition AnalogMode = "position"
	// ModeRate is integrating ("rate of change") control: the source sets how
	// fast the channel moves, not where it sits, so releasing the stick to center
	// holds the current position instead of restoring it — useful for a turret.
	ModeRate AnalogMode = "rate"
)

// DefaultSweepSecs is the rate-mode time for a full Min->Max sweep at full stick
// deflection when a channel's SweepSecs is unset.
const DefaultSweepSecs = 2.0

// Channel is the configuration for a single RC channel.
type Channel struct {
	Name    string       `yaml:"name"`
	Type    MapType      `yaml:"type"`
	Source  input.Source `yaml:"source"`            // analog source or "up" button
	Source2 input.Source `yaml:"source2,omitempty"` // switch3 "down" button

	// Analog shaping.
	Reverse  bool    `yaml:"reverse,omitempty"`
	Deadzone float64 `yaml:"deadzone,omitempty"` // 0..1 fraction ignored around rest
	Expo     float64 `yaml:"expo,omitempty"`     // -1..1; >0 softens center, <0 sharpens
	Scale    float64 `yaml:"scale,omitempty"`    // input multiplier / "rate"; 0 or unset = 1.0 (no scaling). Position mode only.
	Trim     int     `yaml:"trim,omitempty"`     // ticks added after scaling
	Min      int     `yaml:"min"`                // low endpoint (ticks)
	Max      int     `yaml:"max"`                // high endpoint (ticks)

	// Analog rate ("rate of change") mode. ModeRate integrates the source into a
	// held position instead of mapping it directly; Reverse/Expo/Deadzone/Min/Max
	// still apply (Scale does not — SweepSecs is the speed control).
	Mode           AnalogMode   `yaml:"mode,omitempty"`            // "" = position
	SweepSecs      float64      `yaml:"sweep_secs,omitempty"`      // rate mode: seconds for a full Min->Max sweep at full deflection (0 = DefaultSweepSecs)
	RecenterSource input.Source `yaml:"recenter_source,omitempty"` // rate mode: button that snaps the position back to center while held

	// Switch positions (ticks).
	Low       int       `yaml:"low,omitempty"`
	Mid       int       `yaml:"mid,omitempty"`
	High      int       `yaml:"high,omitempty"`
	PressMode PressMode `yaml:"press_mode,omitempty"` // switch2 only; default = toggle
	PulseHz   int       `yaml:"pulse_hz,omitempty"`   // switch2 pulse mode: full cycles per second (default 4)

	// Deprecated: switch2 hold-vs-toggle bool; superseded by PressMode. Kept so
	// older YAML still loads — normalize() migrates "momentary: true" to
	// PressMode=momentary and clears this field.
	Momentary bool `yaml:"momentary,omitempty"`

	Fixed    int `yaml:"fixed,omitempty"` // value for TypeFixed
	Failsafe int `yaml:"failsafe"`        // value sent while disarmed / on input loss
}

// DefaultChannel returns an unmapped channel with sane endpoints and a centered
// failsafe — a safe baseline for any channel.
func DefaultChannel(name string) Channel {
	return Channel{
		Name:           name,
		Type:           TypeNone,
		Source:         input.SrcNone,
		Source2:        input.SrcNone,
		RecenterSource: input.SrcNone,
		Min:            int(crsf.TicksMin),
		Max:            int(crsf.TicksMax),
		Low:            int(crsf.TicksMin),
		Mid:            int(crsf.TicksMid),
		High:           int(crsf.TicksMax),
		Failsafe:       int(crsf.TicksMid),
	}
}

// NewProfile returns 16 default channels named "CH1".."CH16".
func NewProfile() []Channel {
	out := make([]Channel, crsf.NumChannels)
	for i := range out {
		out[i] = DefaultChannel("CH" + itoa(i+1))
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
