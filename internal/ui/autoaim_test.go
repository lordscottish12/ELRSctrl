package ui

import (
	"image"
	"testing"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/crsf"
)

func approx(a, b, tol float64) bool { d := a - b; return d < tol && d > -tol }

func TestAimPointFrame(t *testing.T) {
	// 1280x720 fills the 1280-wide area; zero offset -> frame center.
	if x, y := aimPointFrame(1280, 720, 0, 0); !approx(x, 640, 0.5) || !approx(y, 360, 0.5) {
		t.Errorf("center aim = (%.1f,%.1f), want (640,360)", x, y)
	}
	// At scale 1, a +100px screen offset is +100px in the frame.
	if x, _ := aimPointFrame(1280, 720, 100, 0); !approx(x, 740, 0.5) {
		t.Errorf("offset aim x = %.1f, want 740", x)
	}
}

func TestStepAim(t *testing.T) {
	// No rate, gain 4, dt 0.1, err 0.5 -> +0.2 (pure proportional slew).
	if v := stepAim(0, 0.5, 0, 4, 0.5, 0.03, false, 0.1); !approx(v, 0.2, 1e-9) {
		t.Errorf("P-only step = %v, want 0.2", v)
	}
	// Derivative brake: err shrinking (rate -1) with damp 0.5 reduces the slew.
	// v = 4*0.5 + 0.5*(-1) = 1.5; *0.1 = 0.15.
	if v := stepAim(0, 0.5, -1, 4, 0.5, 0.03, false, 0.1); !approx(v, 0.15, 1e-9) {
		t.Errorf("PD step = %v, want 0.15", v)
	}
	// invert flips the whole velocity (P and D together).
	if v := stepAim(0, 0.5, -1, 4, 0.5, 0.03, true, 0.1); !approx(v, -0.15, 1e-9) {
		t.Errorf("inverted PD step = %v, want -0.15", v)
	}
	// inside the deadband -> hold, regardless of rate.
	if v := stepAim(0.3, 0.01, 5, 4, 0.5, 0.03, false, 0.1); v != 0.3 {
		t.Errorf("deadband hold = %v, want 0.3", v)
	}
	// clamps at +1.
	if v := stepAim(0.95, 1, 0, 4, 0, 0, false, 0.1); v != 1 {
		t.Errorf("clamp = %v, want 1", v)
	}
}

func TestPosToTicksAndSeed(t *testing.T) {
	ch := channels.Channel{Min: int(crsf.TicksMin), Max: int(crsf.TicksMax)} // 172..1811
	if got := posToTicks(0, ch); got != crsf.TicksMid {
		t.Errorf("posToTicks(0) = %d, want %d", got, crsf.TicksMid)
	}
	if got := posToTicks(1, ch); got != crsf.TicksMax {
		t.Errorf("posToTicks(1) = %d, want %d", got, crsf.TicksMax)
	}
	if got := posToTicks(-1, ch); got != crsf.TicksMin {
		t.Errorf("posToTicks(-1) = %d, want %d", got, crsf.TicksMin)
	}

	chans := []channels.Channel{ch}
	var live [crsf.NumChannels]uint16
	live[0] = crsf.TicksMax
	if got := seedPos(&live, 0, chans); !approx(got, 1, 1e-6) {
		t.Errorf("seedPos(max) = %v, want 1", got)
	}
	live[0] = crsf.TicksMid
	if got := seedPos(&live, 0, chans); !approx(got, 0, 0.01) {
		t.Errorf("seedPos(mid) = %v, want ~0", got)
	}
}

func TestBoxAimPoint(t *testing.T) {
	b := image.Rect(100, 200, 300, 600) // 200 wide, 400 tall
	// Horizontal is always the center.
	if x, _ := boxAimPoint(b, 0.25); x != 200 {
		t.Errorf("aim x = %v, want 200", x)
	}
	// 0.25 down from the top of a 400-tall box starting at y=200 -> 200 + 100 = 300.
	if _, y := boxAimPoint(b, 0.25); y != 300 {
		t.Errorf("aim y (0.25) = %v, want 300", y)
	}
	// Center (0.5) -> the box's vertical middle (400), the old behavior.
	if _, y := boxAimPoint(b, 0.5); y != 400 {
		t.Errorf("aim y (0.5) = %v, want 400", y)
	}
}

func TestTurretTestPhase(t *testing.T) {
	for _, c := range []struct {
		elapsed float64
		want    int
	}{{0, 0}, {0.5, 0}, {1.2, 1}, {2.5, 2}, {3.9, 3}, {4.0, -1}, {10, -1}} {
		if got := turretTestPhase(c.elapsed); got != c.want {
			t.Errorf("turretTestPhase(%.1f) = %d, want %d", c.elapsed, got, c.want)
		}
	}
}

// TestTurretTestPos pins the direction convention and, crucially, that it matches the
// steady state auto-aim's integrator settles at for the same axis/invert — so a wrong
// direction in the test means a wrong direction in auto-aim (flip that invert).
func TestTurretTestPos(t *testing.T) {
	// UP (phase 0) drives tilt the same way the integrator does for a target above the
	// crosshair (errY = -1); pan stays centered.
	pan, tilt := turretTestPos(0, false, false)
	if pan != 0 {
		t.Errorf("UP pan = %v, want 0", pan)
	}
	if want := sign(stepAim(0, -1, 0, 4, 0.5, 0, false, 0.1)); sign(tilt) != want {
		t.Errorf("UP tilt sign = %v, want %v (match controller)", sign(tilt), want)
	}
	// Invert flips it, again matching the controller.
	_, tiltInv := turretTestPos(0, false, true)
	if want := sign(stepAim(0, -1, 0, 4, 0.5, 0, true, 0.1)); sign(tiltInv) != want {
		t.Errorf("UP tilt (inverted) sign = %v, want %v", sign(tiltInv), want)
	}
	// RIGHT (phase 1) drives pan for errX = +1; tilt stays centered.
	pan, tilt = turretTestPos(1, false, false)
	if tilt != 0 {
		t.Errorf("RIGHT tilt = %v, want 0", tilt)
	}
	if want := sign(stepAim(0, +1, 0, 4, 0.5, 0, false, 0.1)); sign(pan) != want {
		t.Errorf("RIGHT pan sign = %v, want %v", sign(pan), want)
	}
	// Deflection stays inside the endpoints.
	if pan <= 0 || pan >= 1 {
		t.Errorf("RIGHT pan = %v, want in (0,1)", pan)
	}
}

func sign(v float64) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}
