package channels

import (
	"testing"
	"time"

	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

// t0 is a fixed instant tests use so pulse timing is deterministic.
var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func stateWithAxis(src input.Source, v float64) input.State {
	s := input.NewState()
	s.Connected = true
	s.Axes[src] = v
	return s
}

func stateWithButtons(btns ...input.Source) input.State {
	s := input.NewState()
	s.Connected = true
	for _, b := range btns {
		s.Buttons[b] = true
	}
	return s
}

func analogCh(src input.Source) Channel {
	c := DefaultChannel("t")
	c.Type = TypeAnalog
	c.Source = src
	return c
}

func TestAnalogCenterEndpoints(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcLeftStickX)

	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 0), t0); got != crsf.TicksMid {
		t.Errorf("center = %d, want %d", got, crsf.TicksMid)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 1), t0); got != crsf.TicksMax {
		t.Errorf("full+ = %d, want %d", got, crsf.TicksMax)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, -1), t0); got != crsf.TicksMin {
		t.Errorf("full- = %d, want %d", got, crsf.TicksMin)
	}
}

func TestAnalogReverse(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcLeftStickX)
	c.Reverse = true
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 1), t0); got != crsf.TicksMin {
		t.Errorf("reversed full+ = %d, want %d", got, crsf.TicksMin)
	}
}

func TestAnalogDeadzone(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcLeftStickX)
	c.Deadzone = 0.2
	// Inside deadzone -> center.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 0.1), t0); got != crsf.TicksMid {
		t.Errorf("inside deadzone = %d, want %d", got, crsf.TicksMid)
	}
	// At the edge (1.0) deadzone shouldn't reduce the endpoint.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 1), t0); got != crsf.TicksMax {
		t.Errorf("full+ with deadzone = %d, want %d", got, crsf.TicksMax)
	}
}

func TestTriggersCombined(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcTriggers)
	// R2 fully forward -> +1 -> max.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcTriggers, 1), t0); got != crsf.TicksMax {
		t.Errorf("R2 fwd = %d, want %d", got, crsf.TicksMax)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcTriggers, -1), t0); got != crsf.TicksMin {
		t.Errorf("L2 rev = %d, want %d", got, crsf.TicksMin)
	}
}

func TestUnipolarTrigger(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcRightTrigger) // unipolar 0..1
	if got := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 0), t0); got != crsf.TicksMin {
		t.Errorf("trigger 0 = %d, want %d (low endpoint)", got, crsf.TicksMin)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 1), t0); got != crsf.TicksMax {
		t.Errorf("trigger 1 = %d, want %d", got, crsf.TicksMax)
	}
}

func TestAnalogScale(t *testing.T) {
	var e Engine
	full := analogCh(input.SrcLeftStickX)
	scaled := analogCh(input.SrcLeftStickX)
	scaled.Scale = 0.5

	// Scaling the input by 0.5 must equal feeding half the deflection at scale 1.
	a := e.applyOne(0, scaled, stateWithAxis(input.SrcLeftStickX, 1), t0)
	b := e.applyOne(0, full, stateWithAxis(input.SrcLeftStickX, 0.5), t0)
	if a != b {
		t.Errorf("scale 0.5 @ full = %d, want it to equal scale 1.0 @ half = %d", a, b)
	}
	// Center is unaffected by scale.
	if got := e.applyOne(0, scaled, stateWithAxis(input.SrcLeftStickX, 0), t0); got != crsf.TicksMid {
		t.Errorf("scaled center = %d, want %d", got, crsf.TicksMid)
	}
	// Zero/unset scale is treated as 1.0 (no scaling), so every existing config is unchanged.
	zero := analogCh(input.SrcLeftStickX) // Scale defaults to 0
	if got := e.applyOne(0, zero, stateWithAxis(input.SrcLeftStickX, 1), t0); got != crsf.TicksMax {
		t.Errorf("scale 0 (=1.0) full+ = %d, want %d", got, crsf.TicksMax)
	}
}

func TestUnipolarExpo(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcRightTrigger) // unipolar 0..1
	c.Expo = 0.5                         // soften the low/idle end

	// Endpoints are unchanged by expo.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 0), t0); got != crsf.TicksMin {
		t.Errorf("expo trigger 0 = %d, want %d", got, crsf.TicksMin)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 1), t0); got != crsf.TicksMax {
		t.Errorf("expo trigger 1 = %d, want %d", got, crsf.TicksMax)
	}
	// Half throw with expo>0 sits below the linear midpoint (softer near idle).
	lin := e.applyOne(0, analogCh(input.SrcRightTrigger), stateWithAxis(input.SrcRightTrigger, 0.5), t0)
	soft := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 0.5), t0)
	if soft >= lin {
		t.Errorf("expo>0 at half throw = %d, want less than linear %d", soft, lin)
	}
}

func rateCh(src input.Source) Channel {
	c := analogCh(src)
	c.Mode = ModeRate
	c.SweepSecs = 2.0 // center->endpoint = half a sweep = 1s at full deflection
	return c
}

// drive integrates a held axis value over n steps of 0.1s (the dt cap), returning
// the last output. The first call seeds position at center.
func drive(e *Engine, c Channel, in input.State, start time.Time, steps int) (uint16, time.Time) {
	now := start
	got := e.applyOne(0, c, in, now) // seed
	for i := 0; i < steps; i++ {
		now = now.Add(100 * time.Millisecond)
		got = e.applyOne(0, c, in, now)
	}
	return got, now
}

func TestRateSweepReachesEndpoint(t *testing.T) {
	var e Engine
	e.armed = true
	c := rateCh(input.SrcLeftStickX)
	full := stateWithAxis(input.SrcLeftStickX, 1)

	// Seed sits at center.
	if got := e.applyOne(0, c, full, t0); got != crsf.TicksMid {
		t.Fatalf("seed = %d, want center %d", got, crsf.TicksMid)
	}
	// Full deflection for 1s (center->Max = half of the 2s sweep) hits Max...
	got, now := drive(&e, c, full, t0, 10)
	if got != crsf.TicksMax {
		t.Fatalf("after 1s @ full = %d, want Max %d", got, crsf.TicksMax)
	}
	// ...and holding longer clamps at Max rather than overshooting.
	if got := e.applyOne(0, c, full, now.Add(time.Second)); got != crsf.TicksMax {
		t.Fatalf("overdriven = %d, want clamped Max %d", got, crsf.TicksMax)
	}
}

func TestRateHoldsWhenCentered(t *testing.T) {
	var e Engine
	e.armed = true
	c := rateCh(input.SrcLeftStickX)

	// Push for 0.5s, then release the stick to center.
	moved, now := drive(&e, c, stateWithAxis(input.SrcLeftStickX, 1), t0, 5)
	if moved <= crsf.TicksMid {
		t.Fatalf("expected position above center after driving, got %d", moved)
	}
	center := stateWithAxis(input.SrcLeftStickX, 0)
	var held uint16
	for i := 0; i < 5; i++ {
		now = now.Add(100 * time.Millisecond)
		held = e.applyOne(0, c, center, now)
	}
	if held != moved {
		t.Errorf("centering the stick moved position: held %d, want it to stay at %d", held, moved)
	}
}

func TestRateFreezesWhileDisarmed(t *testing.T) {
	var e Engine // armed defaults false
	c := rateCh(input.SrcLeftStickX)
	got, _ := drive(&e, c, stateWithAxis(input.SrcLeftStickX, 1), t0, 5)
	if got != crsf.TicksMid {
		t.Errorf("disarmed = %d, want center %d (no integration while disarmed)", got, crsf.TicksMid)
	}
}

func TestRateRecenter(t *testing.T) {
	var e Engine
	e.armed = true
	c := rateCh(input.SrcLeftStickX)
	c.RecenterSource = input.SrcA

	moved, now := drive(&e, c, stateWithAxis(input.SrcLeftStickX, 1), t0, 5)
	if moved == crsf.TicksMid {
		t.Fatalf("expected off-center position before recenter, got center")
	}
	// Press recenter while the stick is still deflected -> snaps to center.
	recenter := stateWithAxis(input.SrcLeftStickX, 1)
	recenter.Buttons[input.SrcA] = true
	if got := e.applyOne(0, c, recenter, now.Add(100*time.Millisecond)); got != crsf.TicksMid {
		t.Errorf("recenter = %d, want center %d", got, crsf.TicksMid)
	}
}

func TestRateNoLurchAfterDisarmedGap(t *testing.T) {
	// A long disarmed gap must not cause a giant jump on re-arm: position only
	// advances from the moment integration resumes.
	var e Engine
	c := rateCh(input.SrcLeftStickX)
	full := stateWithAxis(input.SrcLeftStickX, 1)

	e.applyOne(0, c, full, t0) // seed, disarmed
	// 10 seconds pass while disarmed; lastAt tracks even though position is frozen.
	e.applyOne(0, c, full, t0.Add(10*time.Second))
	if got := e.applyOne(0, c, full, t0.Add(10*time.Second)); got != crsf.TicksMid {
		t.Fatalf("still disarmed = %d, want center %d", got, crsf.TicksMid)
	}
	// Re-arm and step a single capped dt: position must move only a little, not lurch.
	e.armed = true
	got := e.applyOne(0, c, full, t0.Add(10*time.Second+100*time.Millisecond))
	if got == crsf.TicksMid {
		t.Errorf("expected slight movement after re-arm, got center")
	}
	if got >= crsf.TicksMax {
		t.Errorf("lurched to %d after re-arm; expected a small step from center", got)
	}
}

func TestSwitch2Momentary(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch2
	c.Source = input.SrcA
	c.PressMode = PressMomentary

	if got := e.applyOne(0, c, input.NewState(), t0); got != uint16(c.Low) {
		t.Errorf("released = %d, want %d", got, c.Low)
	}
	if got := e.applyOne(0, c, stateWithButtons(input.SrcA), t0); got != uint16(c.High) {
		t.Errorf("held = %d, want %d", got, c.High)
	}
}

func TestSwitch2Toggle(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch2
	c.Source = input.SrcA

	released := input.NewState()
	pressed := stateWithButtons(input.SrcA)

	// Press once -> High; release stays High; press again -> Low.
	if got := e.applyOne(0, c, pressed, t0); got != uint16(c.High) {
		t.Fatalf("first press = %d, want High %d", got, c.High)
	}
	if got := e.applyOne(0, c, released, t0); got != uint16(c.High) {
		t.Fatalf("after release = %d, want still High %d", got, c.High)
	}
	if got := e.applyOne(0, c, pressed, t0); got != uint16(c.Low) {
		t.Fatalf("second press = %d, want Low %d", got, c.Low)
	}
}

func TestSwitch2Pulse(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch2
	c.Source = input.SrcA
	c.PressMode = PressPulse
	c.PulseHz = 4 // 250 ms period, 125 ms half-cycle

	released := input.NewState()
	pressed := stateWithButtons(input.SrcA)
	half := 125 * time.Millisecond

	// Released: stays Low regardless of time.
	if got := e.applyOne(0, c, released, t0); got != uint16(c.Low) {
		t.Fatalf("released = %d, want Low %d", got, c.Low)
	}
	// First frame of a press snaps High and arms the flip timer.
	if got := e.applyOne(0, c, pressed, t0); got != uint16(c.High) {
		t.Fatalf("press t0 = %d, want High %d", got, c.High)
	}
	// Still inside the first half-period -> still High.
	if got := e.applyOne(0, c, pressed, t0.Add(half-time.Millisecond)); got != uint16(c.High) {
		t.Fatalf("press just before flip = %d, want High %d", got, c.High)
	}
	// Crossing the half-period flips to Low.
	if got := e.applyOne(0, c, pressed, t0.Add(half)); got != uint16(c.Low) {
		t.Fatalf("press at flip = %d, want Low %d", got, c.Low)
	}
	// Two more half-periods -> Low -> High -> Low.
	if got := e.applyOne(0, c, pressed, t0.Add(2*half)); got != uint16(c.High) {
		t.Fatalf("press at 2x flip = %d, want High %d", got, c.High)
	}
	// Release snaps back to Low immediately.
	if got := e.applyOne(0, c, released, t0.Add(3*half)); got != uint16(c.Low) {
		t.Fatalf("release = %d, want Low %d", got, c.Low)
	}
	// A second press, far in the future, must start cleanly at High again — not
	// resume mid-cycle from before the release.
	if got := e.applyOne(0, c, pressed, t0.Add(10*time.Second)); got != uint16(c.High) {
		t.Fatalf("second press = %d, want High %d", got, c.High)
	}
}

func TestSwitch2PulseCatchupAfterStall(t *testing.T) {
	// If Apply is called late (UI stalled), the timer should resolve to the
	// position the schedule says it should be at — not lag arbitrarily.
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch2
	c.Source = input.SrcA
	c.PressMode = PressPulse
	c.PulseHz = 4
	pressed := stateWithButtons(input.SrcA)
	half := 125 * time.Millisecond

	e.applyOne(0, c, pressed, t0) // High, flip scheduled at t0+125ms
	// Jump 4 half-periods in one go: schedule says High -> Low -> High -> Low -> High.
	// pulseOn started true; flipping 4 times lands on true again.
	if got := e.applyOne(0, c, pressed, t0.Add(4*half+10*time.Millisecond)); got != uint16(c.High) {
		t.Fatalf("post-stall = %d, want High %d", got, c.High)
	}
}

func TestSwitch3(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch3
	c.Source = input.SrcDUp    // up -> High
	c.Source2 = input.SrcDDown // down -> Low

	if got := e.applyOne(0, c, input.NewState(), t0); got != uint16(c.Mid) {
		t.Errorf("neither = %d, want Mid %d", got, c.Mid)
	}
	if got := e.applyOne(0, c, stateWithButtons(input.SrcDUp), t0); got != uint16(c.High) {
		t.Errorf("up = %d, want High %d", got, c.High)
	}
	if got := e.applyOne(0, c, stateWithButtons(input.SrcDDown), t0); got != uint16(c.Low) {
		t.Errorf("down = %d, want Low %d", got, c.Low)
	}
}

func TestFixedAndFailsafe(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeFixed
	c.Fixed = 1500
	if got := e.applyOne(0, c, input.NewState(), t0); got != 1500 {
		t.Errorf("fixed = %d, want 1500", got)
	}

	chans := []Channel{{Failsafe: 1000}, {Failsafe: 992}}
	fs := FailsafeValues(chans)
	if fs[0] != 1000 || fs[1] != 992 {
		t.Errorf("failsafe = %v, want [1000 992 ...]", fs[:2])
	}
	if fs[5] != crsf.TicksMid {
		t.Errorf("unconfigured failsafe = %d, want center %d", fs[5], crsf.TicksMid)
	}
}
