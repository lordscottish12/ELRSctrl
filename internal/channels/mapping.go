// Package channels turns a gamepad input snapshot into 16 CRSF channel values
// according to a user-defined, fully remappable mapping.
package channels

import (
	"dronectrl/internal/crsf"
	"dronectrl/internal/input"
)

// MapType selects how a channel derives its value from its source(s).
type MapType string

const (
	TypeNone    MapType = "none"    // channel holds center (992)
	TypeAnalog  MapType = "analog"  // proportional from an analog source
	TypeSwitch2 MapType = "switch2" // one button -> Low/High (toggle or momentary)
	TypeSwitch3 MapType = "switch3" // two buttons -> Low/Mid/High
	TypeFixed   MapType = "fixed"   // constant value
)

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
	Trim     int     `yaml:"trim,omitempty"`     // ticks added after scaling
	Min      int     `yaml:"min"`                // low endpoint (ticks)
	Max      int     `yaml:"max"`                // high endpoint (ticks)

	// Switch positions (ticks).
	Low       int  `yaml:"low,omitempty"`
	Mid       int  `yaml:"mid,omitempty"`
	High      int  `yaml:"high,omitempty"`
	Momentary bool `yaml:"momentary,omitempty"` // switch2: hold vs. toggle

	Fixed    int `yaml:"fixed,omitempty"`   // value for TypeFixed
	Failsafe int `yaml:"failsafe"`          // value sent while disarmed / on input loss
}

// DefaultChannel returns an unmapped channel with sane endpoints and a centered
// failsafe — a safe baseline for any channel.
func DefaultChannel(name string) Channel {
	return Channel{
		Name:     name,
		Type:     TypeNone,
		Source:   input.SrcNone,
		Source2:  input.SrcNone,
		Min:      int(crsf.TicksMin),
		Max:      int(crsf.TicksMax),
		Low:      int(crsf.TicksMin),
		Mid:      int(crsf.TicksMid),
		High:     int(crsf.TicksMax),
		Failsafe: int(crsf.TicksMid),
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
