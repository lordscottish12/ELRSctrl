package channels

import (
	"math"
	"time"

	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

// Engine evaluates a mapping each frame. It is stateful because toggle switches
// remember their position, pulse mode flips on a wall-clock timer, and rate-mode
// channels integrate their position over time.
type Engine struct {
	toggle      [crsf.NumChannels]bool
	prevBtn     [crsf.NumChannels]bool
	pulseOn     [crsf.NumChannels]bool      // current half of the pulse cycle while held
	pulseFlipAt [crsf.NumChannels]time.Time // wall-clock instant to flip pulseOn next

	armed      bool                        // set each Apply; rate channels only integrate while armed
	ratePos    [crsf.NumChannels]float64   // rate mode: accumulated position, normalized -1..1 (Min..Max)
	rateInit   [crsf.NumChannels]bool      // rate mode: ratePos has been seeded
	rateLastAt [crsf.NumChannels]time.Time // rate mode: previous Apply instant, for dt
}

// Apply evaluates all channels against the current input snapshot. now drives the
// pulse-mode timer and rate-mode integration; pass time.Now() in production.
// armed gates rate-mode integration (it holds position while disarmed so stick
// motion on the setup screens can't drift a turret). Channels beyond len(chans)
// are centered.
func (e *Engine) Apply(chans []Channel, in input.State, armed bool, now time.Time) [crsf.NumChannels]uint16 {
	e.armed = armed
	var out [crsf.NumChannels]uint16
	for i := 0; i < crsf.NumChannels; i++ {
		if i < len(chans) {
			out[i] = e.applyOne(i, chans[i], in, now)
		} else {
			out[i] = crsf.TicksMid
		}
	}
	return out
}

func (e *Engine) applyOne(i int, c Channel, in input.State, now time.Time) uint16 {
	switch c.Type {
	case TypeFixed:
		return crsf.ClampTicks(c.Fixed)

	case TypeSwitch2:
		pressed := in.Pressed(c.Source)
		var on bool
		switch c.PressMode {
		case PressMomentary:
			on = pressed
		case PressPulse:
			on = e.updatePulse(i, pressed, c.PulseHz, now)
		default: // PressToggle and unset
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
		if c.Mode == ModeRate {
			return e.rateValue(i, c, in, now)
		}
		return analogValue(c, in)

	default: // TypeNone
		return crsf.TicksMid
	}
}

// rateValue implements integrating ("rate of change") control: the shaped source
// sets how fast the channel moves, not where it sits, so releasing the stick to
// center holds the current position. Position accumulates in normalized space
// [-1,1] (mapped to [Min,Max]) and is clamped to the endpoints. It advances only
// while armed; a RecenterSource button, if bound, snaps to center while held,
// regardless of arm state.
func (e *Engine) rateValue(i int, c Channel, in input.State, now time.Time) uint16 {
	if !e.rateInit[i] {
		e.ratePos[i] = 0 // start centered
		e.rateLastAt[i] = now
		e.rateInit[i] = true
	}
	dt := now.Sub(e.rateLastAt[i]).Seconds()
	e.rateLastAt[i] = now
	if dt < 0 {
		dt = 0
	}
	if dt > 0.1 { // cap dt after a UI stall so position can't lurch
		dt = 0.1
	}

	switch {
	case c.RecenterSource != input.SrcNone && in.Pressed(c.RecenterSource):
		e.ratePos[i] = 0
	case e.armed:
		sweep := c.SweepSecs
		if sweep <= 0 {
			sweep = DefaultSweepSecs
		}
		// A full Min->Max sweep spans 2 normalized units; at full deflection it
		// should take `sweep` seconds, so the per-second rate is 2/sweep.
		e.ratePos[i] = clampBipolar(e.ratePos[i] + rateCommand(c, in)*(2.0/sweep)*dt)
	}

	mid := float64(c.Min+c.Max) / 2
	half := float64(c.Max-c.Min) / 2
	v := mid + e.ratePos[i]*half + float64(c.Trim)
	return crsf.ClampTicks(int(math.Round(v)))
}

// rateCommand shapes an analog source into a signed rate command for ModeRate.
// Bipolar sources give -1..1 (drive either way); a unipolar trigger gives 0..1
// (one direction only). Reverse/Deadzone/Expo apply; Scale does not (SweepSecs is
// the speed control).
func rateCommand(c Channel, in input.State) float64 {
	raw := in.Analog(c.Source)
	if c.Source.Bipolar() {
		x := raw
		if c.Reverse {
			x = -x
		}
		x = deadzoneBipolar(x, c.Deadzone)
		return clampBipolar(expo(x, c.Expo))
	}
	x := clamp01(raw)
	if c.Reverse {
		x = 1 - x
	}
	x = deadzoneUnipolar(x, c.Deadzone)
	return clamp01(expo(x, c.Expo))
}

func analogValue(c Channel, in input.State) uint16 {
	raw := in.Analog(c.Source)
	scale := c.Scale
	if scale <= 0 {
		scale = 1 // 0/unset means no scaling (kept out of YAML by omitempty)
	}

	if c.Source.Bipolar() {
		x := raw
		if c.Reverse {
			x = -x
		}
		x = deadzoneBipolar(x, c.Deadzone)
		x = expo(x, c.Expo)
		x = clampBipolar(x * scale) // "rate": <1 gentler, >1 saturates sooner; endpoints stay the limit
		mid := float64(c.Min+c.Max) / 2
		half := float64(c.Max-c.Min) / 2
		v := mid + x*half + float64(c.Trim)
		return crsf.ClampTicks(int(math.Round(v)))
	}

	// Unipolar (single trigger), 0..1. Expo softens the low/idle end here, mirroring
	// how it softens around center on the bipolar path.
	x := clamp01(raw)
	if c.Reverse {
		x = 1 - x
	}
	x = deadzoneUnipolar(x, c.Deadzone)
	x = expo(x, c.Expo)
	x = clamp01(x * scale)
	v := float64(c.Min) + x*float64(c.Max-c.Min) + float64(c.Trim)
	return crsf.ClampTicks(int(math.Round(v)))
}

// updatePulse advances the pulse-mode square wave for channel i. Released = Low
// (and resets the timer so the next press starts cleanly at High). Held = flip
// pulseOn whenever wall-clock crosses pulseFlipAt; half-period = 1/(2*hz).
func (e *Engine) updatePulse(i int, pressed bool, hz int, now time.Time) bool {
	if !pressed {
		e.pulseOn[i] = false
		e.pulseFlipAt[i] = time.Time{}
		return false
	}
	if hz <= 0 {
		hz = 4
	}
	half := time.Second / time.Duration(2*hz)
	if e.pulseFlipAt[i].IsZero() { // first frame of a new press: snap High and schedule the drop
		e.pulseOn[i] = true
		e.pulseFlipAt[i] = now.Add(half)
		return true
	}
	for !now.Before(e.pulseFlipAt[i]) { // catches up if Apply was called late (UI stall)
		e.pulseOn[i] = !e.pulseOn[i]
		e.pulseFlipAt[i] = e.pulseFlipAt[i].Add(half)
	}
	return e.pulseOn[i]
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

// clampBipolar bounds x to [-1, 1] so a scale > 1 saturates at the endpoints
// rather than driving the output past them.
func clampBipolar(x float64) float64 {
	if x < -1 {
		return -1
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
