package channels

import (
	"math"

	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

// Engine evaluates a mapping each frame. It is stateful because toggle switches
// remember their position and need rising-edge detection.
type Engine struct {
	toggle  [crsf.NumChannels]bool
	prevBtn [crsf.NumChannels]bool
}

// Apply evaluates all channels against the current input snapshot. Channels
// beyond len(chans) are centered.
func (e *Engine) Apply(chans []Channel, in input.State) [crsf.NumChannels]uint16 {
	var out [crsf.NumChannels]uint16
	for i := 0; i < crsf.NumChannels; i++ {
		if i < len(chans) {
			out[i] = e.applyOne(i, chans[i], in)
		} else {
			out[i] = crsf.TicksMid
		}
	}
	return out
}

func (e *Engine) applyOne(i int, c Channel, in input.State) uint16 {
	switch c.Type {
	case TypeFixed:
		return crsf.ClampTicks(c.Fixed)

	case TypeSwitch2:
		pressed := in.Pressed(c.Source)
		var on bool
		if c.Momentary {
			on = pressed
		} else {
			if pressed && !e.prevBtn[i] { // rising edge toggles
				e.toggle[i] = !e.toggle[i]
			}
			on = e.toggle[i]
		}
		e.prevBtn[i] = pressed
		if c.Reverse {
			on = !on
		}
		if on {
			return crsf.ClampTicks(c.High)
		}
		return crsf.ClampTicks(c.Low)

	case TypeSwitch3:
		up := in.Pressed(c.Source)
		down := in.Pressed(c.Source2)
		hi, lo := c.High, c.Low
		if c.Reverse {
			hi, lo = lo, hi
		}
		switch {
		case up && !down:
			return crsf.ClampTicks(hi)
		case down && !up:
			return crsf.ClampTicks(lo)
		default:
			return crsf.ClampTicks(c.Mid)
		}

	case TypeAnalog:
		return analogValue(c, in)

	default: // TypeNone
		return crsf.TicksMid
	}
}

func analogValue(c Channel, in input.State) uint16 {
	raw := in.Analog(c.Source)

	if c.Source.Bipolar() {
		x := raw
		if c.Reverse {
			x = -x
		}
		x = deadzoneBipolar(x, c.Deadzone)
		x = expo(x, c.Expo)
		mid := float64(c.Min+c.Max) / 2
		half := float64(c.Max-c.Min) / 2
		v := mid + x*half + float64(c.Trim)
		return crsf.ClampTicks(int(math.Round(v)))
	}

	// Unipolar (single trigger), 0..1.
	x := clamp01(raw)
	if c.Reverse {
		x = 1 - x
	}
	x = deadzoneUnipolar(x, c.Deadzone)
	v := float64(c.Min) + x*float64(c.Max-c.Min) + float64(c.Trim)
	return crsf.ClampTicks(int(math.Round(v)))
}

// FailsafeValues returns the configured failsafe value for each channel; used by
// the sender when disarmed, on input loss, or on a stale snapshot.
func FailsafeValues(chans []Channel) [crsf.NumChannels]uint16 {
	var out [crsf.NumChannels]uint16
	for i := 0; i < crsf.NumChannels; i++ {
		if i < len(chans) {
			out[i] = crsf.ClampTicks(chans[i].Failsafe)
		} else {
			out[i] = crsf.TicksMid
		}
	}
	return out
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// deadzoneBipolar zeroes |x| <= dz and rescales the remainder back to full range.
func deadzoneBipolar(x, dz float64) float64 {
	if dz <= 0 {
		return x
	}
	if dz >= 1 {
		return 0
	}
	a := math.Abs(x)
	if a <= dz {
		return 0
	}
	s := (a - dz) / (1 - dz)
	if x < 0 {
		return -s
	}
	return s
}

func deadzoneUnipolar(x, dz float64) float64 {
	if dz <= 0 {
		return x
	}
	if dz >= 1 {
		return 0
	}
	if x <= dz {
		return 0
	}
	return (x - dz) / (1 - dz)
}

// expo applies an RC-style exponential curve. e in [-1,1]; e>0 softens around
// center (y = (1-e)x + e x^3), e<0 sharpens.
func expo(x, e float64) float64 {
	if e == 0 {
		return x
	}
	if e > 1 {
		e = 1
	}
	if e < -1 {
		e = -1
	}
	return (1-e)*x + e*x*x*x
}
