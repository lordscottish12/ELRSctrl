package ui

import (
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

func TestIntegrateAim(t *testing.T) {
	// gain 4, dt 0.1, err 0.5 -> +0.2.
	if v := integrateAim(0, 0.5, 4, 0.03, false, 0.1); !approx(v, 0.2, 1e-9) {
		t.Errorf("step = %v, want 0.2", v)
	}
	// invert flips the direction.
	if v := integrateAim(0, 0.5, 4, 0.03, true, 0.1); !approx(v, -0.2, 1e-9) {
		t.Errorf("inverted step = %v, want -0.2", v)
	}
	// inside the deadband -> hold.
	if v := integrateAim(0.3, 0.01, 4, 0.03, false, 0.1); v != 0.3 {
		t.Errorf("deadband hold = %v, want 0.3", v)
	}
	// clamps at +1.
	if v := integrateAim(0.95, 1, 4, 0, false, 0.1); v != 1 {
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
