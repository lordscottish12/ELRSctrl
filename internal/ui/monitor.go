package ui

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"

	"elrsctrl/internal/crsf"
)

// Monitor screen geometry.
const (
	monStatusY = tabH + 14
	monStatusH = 80
	monBarsTop = tabH + 110
	monNameX   = 16
	monBarX    = 200
	monBarW    = 820
	monValueR  = screenW - 16
)

func monRowH() float32 {
	return float32(screenH-monBarsTop-8) / float32(crsf.NumChannels)
}

// armRects returns the ARM/DISARM and KILL button rectangles.
func armRects() (ax, ay, aw, ah, kx float32) {
	aw, ah = 160, 64
	ay = tabH + 22
	ax = screenW - 360
	kx = screenW - 180
	return
}

func (g *Game) drawMonitor(screen *ebiten.Image, p pointer) {
	snap := g.store.Snapshot()

	// Effective values = what the sender is actually transmitting right now.
	effective := snap.Live
	live := snap.Armed && snap.InputOK
	if !live {
		effective = snap.Failsafe
	}

	// Status panel.
	fillRect(screen, 8, monStatusY, screenW-376, monStatusH, colPanel)
	strokeRect(screen, 8, monStatusY, screenW-376, monStatusH, 1, colGrid)

	portTxt := "Port: (none)"
	portCol := colBad
	if g.store.PortConnected() {
		portTxt = "Port: " + g.store.PortName() + "  ✓"
		portCol = colGood
	} else if name := g.store.PortName(); name != "" {
		portTxt = "Port: " + name + "  ✗"
	}
	drawText(screen, portTxt, 20, monStatusY+10, sizeLabel, portCol)
	drawText(screen, fmt.Sprintf("Frames sent: %d", g.store.TxCount()), 20, monStatusY+38, sizeSmall, colTextDim)

	padTxt := "Gamepad: (none)"
	padCol := colBad
	if g.in.Connected {
		padTxt = "Gamepad: " + g.in.Name
		padCol = colGood
	}
	drawText(screen, padTxt, 360, monStatusY+10, sizeLabel, padCol)
	if e := g.store.LastError(); e != "" && !g.store.PortConnected() {
		drawText(screen, trim(e, 70), 360, monStatusY+38, sizeSmall, colWarn)
	}

	// ARM / KILL.
	ax, ay, aw, ah, kx := armRects()
	armLabel, armCol := "DISARMED", colBad
	if g.armed {
		armLabel, armCol = "ARMED", colGood
	}
	if button(screen, p, ax, ay, aw, ah, armLabel, armCol) {
		g.armed = !g.armed
	}
	if button(screen, p, kx, ay, aw, ah, "KILL", colBad) {
		g.armed = false
		g.setStatus("KILL — disarmed")
	}

	if !live {
		drawText(screen, "Failsafe values are being transmitted (disarm or no input).",
			20, monBarsTop-26, sizeSmall, colWarn)
	}

	// Channel bars.
	rowH := monRowH()
	barH := rowH - 10
	for i := 0; i < crsf.NumChannels; i++ {
		y := float32(monBarsTop) + float32(i)*rowH
		ch := g.cfg.Channels[i]
		val := int(effective[i])

		min, max := ch.Min, ch.Max
		if min == max {
			min, max = int(crsf.TicksMin), int(crsf.TicksMax)
		}
		name := fmt.Sprintf("CH%d  %s", i+1, ch.Name)
		drawText(screen, name, monNameX, float64(y+(rowH-sizeSmall)/2-3), sizeSmall, colText)
		channelBar(screen, monBarX, y+5, monBarW, barH, val, min, max)
		valTxt := fmt.Sprintf("%d us   %d", crsf.TicksToMicros(uint16(val)), val)
		drawTextR(screen, valTxt, monValueR, float64(y+(rowH-sizeSmall)/2-3), sizeSmall, colTextDim)
	}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
