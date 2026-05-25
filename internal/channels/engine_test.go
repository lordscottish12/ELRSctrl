package channels

import (
	"testing"

	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

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

	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 0)); got != crsf.TicksMid {
		t.Errorf("center = %d, want %d", got, crsf.TicksMid)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 1)); got != crsf.TicksMax {
		t.Errorf("full+ = %d, want %d", got, crsf.TicksMax)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, -1)); got != crsf.TicksMin {
		t.Errorf("full- = %d, want %d", got, crsf.TicksMin)
	}
}

func TestAnalogReverse(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcLeftStickX)
	c.Reverse = true
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 1)); got != crsf.TicksMin {
		t.Errorf("reversed full+ = %d, want %d", got, crsf.TicksMin)
	}
}

func TestAnalogDeadzone(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcLeftStickX)
	c.Deadzone = 0.2
	// Inside deadzone -> center.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 0.1)); got != crsf.TicksMid {
		t.Errorf("inside deadzone = %d, want %d", got, crsf.TicksMid)
	}
	// At the edge (1.0) deadzone shouldn't reduce the endpoint.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcLeftStickX, 1)); got != crsf.TicksMax {
		t.Errorf("full+ with deadzone = %d, want %d", got, crsf.TicksMax)
	}
}

func TestTriggersCombined(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcTriggers)
	// R2 fully forward -> +1 -> max.
	if got := e.applyOne(0, c, stateWithAxis(input.SrcTriggers, 1)); got != crsf.TicksMax {
		t.Errorf("R2 fwd = %d, want %d", got, crsf.TicksMax)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcTriggers, -1)); got != crsf.TicksMin {
		t.Errorf("L2 rev = %d, want %d", got, crsf.TicksMin)
	}
}

func TestUnipolarTrigger(t *testing.T) {
	var e Engine
	c := analogCh(input.SrcRightTrigger) // unipolar 0..1
	if got := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 0)); got != crsf.TicksMin {
		t.Errorf("trigger 0 = %d, want %d (low endpoint)", got, crsf.TicksMin)
	}
	if got := e.applyOne(0, c, stateWithAxis(input.SrcRightTrigger, 1)); got != crsf.TicksMax {
		t.Errorf("trigger 1 = %d, want %d", got, crsf.TicksMax)
	}
}

func TestSwitch2Momentary(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch2
	c.Source = input.SrcA
	c.Momentary = true

	if got := e.applyOne(0, c, input.NewState()); got != uint16(c.Low) {
		t.Errorf("released = %d, want %d", got, c.Low)
	}
	if got := e.applyOne(0, c, stateWithButtons(input.SrcA)); got != uint16(c.High) {
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
	if got := e.applyOne(0, c, pressed); got != uint16(c.High) {
		t.Fatalf("first press = %d, want High %d", got, c.High)
	}
	if got := e.applyOne(0, c, released); got != uint16(c.High) {
		t.Fatalf("after release = %d, want still High %d", got, c.High)
	}
	if got := e.applyOne(0, c, pressed); got != uint16(c.Low) {
		t.Fatalf("second press = %d, want Low %d", got, c.Low)
	}
}

func TestSwitch3(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeSwitch3
	c.Source = input.SrcDUp   // up -> High
	c.Source2 = input.SrcDDown // down -> Low

	if got := e.applyOne(0, c, input.NewState()); got != uint16(c.Mid) {
		t.Errorf("neither = %d, want Mid %d", got, c.Mid)
	}
	if got := e.applyOne(0, c, stateWithButtons(input.SrcDUp)); got != uint16(c.High) {
		t.Errorf("up = %d, want High %d", got, c.High)
	}
	if got := e.applyOne(0, c, stateWithButtons(input.SrcDDown)); got != uint16(c.Low) {
		t.Errorf("down = %d, want Low %d", got, c.Low)
	}
}

func TestFixedAndFailsafe(t *testing.T) {
	var e Engine
	c := DefaultChannel("t")
	c.Type = TypeFixed
	c.Fixed = 1500
	if got := e.applyOne(0, c, input.NewState()); got != 1500 {
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
