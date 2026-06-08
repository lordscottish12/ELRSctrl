package ui

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// armRects returns the ARM/DISARM and KILL button rectangles, overlaid top-right
// on the Run screen.
func armRects() (ax, ay, aw, ah, kx float32) {
	aw, ah = 160, 64
	ay = tabH + 22
	ax = screenW - 360
	kx = screenW - 180
	return
}

// drawRun is the play/drive screen: the live analog FPV feed fills the area below
// the tabs, with the Betaflight-style OSD and the ARM/DISARM + KILL controls
// overlaid on top. The full channel readout lives on the Monitor tab; this view
// stays clean for flying.
func (g *Game) drawRun(screen *ebiten.Image, p pointer) {
	snap := g.store.Snapshot()
	live := snap.Armed && snap.InputOK

	// Video area = everything below the tab bar. Black letterbox underneath.
	const vx, vy = float32(0), float32(tabH)
	vw, vh := float32(screenW), float32(screenH-tabH)
	fillRect(screen, vx, vy, vw, vh, colVideoBG)

	frameOK := false
	if g.videoCap == nil {
		msg := "No video — enable a capture device in Settings"
		if g.cfg.Video.Enabled && g.cfg.Video.Device != "" { // enabled but not open yet
			msg = "Connecting to video…"
			if g.videoErr != "" {
				msg = "Video: " + g.videoErr + " (retrying)"
			}
		}
		drawTextC(screen, msg, screenW/2, (tabH+screenH)/2, sizeTitle, colTextDim)
	} else if frame, seq := g.videoCap.Buffer().Latest(); frame != nil {
		// (Re)allocate the GPU texture on first frame or a resolution change,
		// then upload only when the frame actually advanced.
		if g.videoTex == nil || g.videoTex.Bounds().Dx() != frame.W || g.videoTex.Bounds().Dy() != frame.H {
			g.videoTex = ebiten.NewImage(frame.W, frame.H)
			g.videoSeq = 0
		}
		if seq != g.videoSeq {
			g.videoTex.WritePixels(frame.Pix)
			g.videoSeq = seq
		}
		ox, oy, dw, dh := drawImageFit(screen, g.videoTex, vx, vy, vw, vh)
		g.drawDetections(screen, ox, oy, dw, dh, frame.W, frame.H)
		g.updateFPS(seq)
		frameOK = true
	} else {
		msg := g.videoCap.Err()
		if msg == "" {
			msg = "Waiting for video…"
		}
		drawTextC(screen, msg, screenW/2, (tabH+screenH)/2, sizeTitle, colTextDim)
	}

	g.drawOSD(screen, vx, vy, vw, vh, live, frameOK)

	// ARM / KILL controls (top-right). Interactive — separate from the OSD indicator.
	ax, ay, aw, ah, kx := armRects()
	armLabel, armCol := "DISARMED", colBad
	if g.armed {
		armLabel, armCol = "ARMED", colGood
	}
	if button(screen, p, ax, ay, aw, ah, armLabel, armCol) {
		g.armed = !g.armed
		if !g.armed {
			g.cancelTurretTest()
		}
	}
	if button(screen, p, kx, ay, aw, ah, "KILL", colBad) {
		g.armed = false
		g.cancelTurretTest()
		g.setStatus("KILL — disarmed")
	}

	// Turret direction test, under the ARM/KILL row — only when an auto-aim channel is
	// assigned. Tap it (disarmed is fine — it self-arms) to sweep pan/tilt
	// up→right→down→left and confirm the channel/direction assignments by watching the
	// turret (and its camera) move; it disarms again when done. Tap TESTING… to abort.
	if g.cfg.AutoAim.PanChannel != 0 || g.cfg.AutoAim.TiltChannel != 0 {
		ty := ay + ah + 12
		label, col := "TEST AIM", colPanel2
		if g.turretTest.active {
			label, col = "TESTING… (tap to stop)", colAccent
		}
		if button(screen, p, ax, ty, kx+aw-ax, ah, label, col) {
			if g.turretTest.active {
				g.cancelTurretTest()
				g.setStatus("Turret test stopped")
			} else {
				g.startTurretTest()
			}
		}
	}
}

// drawOSD renders the toggleable overlay elements within the video area: a center
// crosshair, arm-state text, the vehicle name, and the capture debug readout. The
// area center is the image center (the feed is always centered), so the crosshair
// lands on the picture.
func (g *Game) drawOSD(screen *ebiten.Image, vx, vy, vw, vh float32, live, frameOK bool) {
	o := g.cfg.OSD
	cx := float64(vx + vw/2)
	top := float64(vy)
	bottom := float64(vy + vh)

	if o.Crosshair && frameOK {
		drawCrosshair(screen, vx+vw/2+float32(o.CrosshairX), vy+vh/2+float32(o.CrosshairY))
	}

	if o.ArmState {
		label, col := "DISARMED", colBad
		if g.armed {
			label, col = "ARMED", colGood
		}
		drawTextOutlinedC(screen, label, cx, top+26, sizeTitle, col)
	}
	// Failsafe is safety-critical, so it's shown whenever active regardless of toggles.
	if !live {
		drawTextOutlinedC(screen, "FAILSAFE", cx, top+58, sizeLabel, colWarn)
	}

	// Turret direction test readout: name the current sweep direction so the operator
	// can check it against which way the turret actually moves.
	if g.turretTest.active {
		if ph := turretTestPhase(time.Since(g.turretTest.start).Seconds()); ph >= 0 {
			drawTextOutlinedC(screen, "TEST AIM: "+turretTestLabels[ph], cx, top+58, sizeTitle, colAccent)
		}
	}

	// Target-lock status (only while armed, when detection is running).
	if g.detectRun != nil && g.armed {
		if g.lockedID != 0 {
			drawTextOutlinedC(screen, fmt.Sprintf("LOCKED #%d", g.lockedID), cx, top+88, sizeLabel, colBad)
		} else if len(visibleTracks(g.tracks)) > 0 {
			drawTextOutlinedC(screen, "NO LOCK", cx, top+88, sizeSmall, colTextDim)
		}
	}

	if o.ShowName && o.VehicleName != "" {
		drawTextOutlinedC(screen, o.VehicleName, cx, bottom-28, sizeTitle, colText)
	}

	if o.VideoInfo && g.videoCap != nil {
		info := g.videoCap.Info()
		if info == "" {
			info = g.videoCap.Err()
		}
		if frameOK && info != "" {
			info = fmt.Sprintf("%s  %.0f fps", info, g.vidFPS)
		}
		if info != "" {
			drawTextOutlined(screen, info, float64(vx)+14, bottom-26, sizeSmall, colTextDim)
		}
	}

	if o.Telemetry {
		g.drawTelemetry(screen, float64(vx)+14, top+14)
	}
}

// drawTelemetry renders the ELRS link/battery block at (x, y), top-left over the
// feed. The link line doubles as the connection-health indicator.
func (g *Game) drawTelemetry(screen *ebiten.Image, x, y float64) {
	tel := g.store.Telemetry()
	now := time.Now()
	const stale = 2 * time.Second

	var line string
	var col color.Color
	switch {
	case tel.LinkValid && now.Sub(tel.LinkAt) < stale:
		line = fmt.Sprintf("LINK %d%%  %ddBm  %dmW", tel.UplinkLQ, tel.UplinkRSSI, tel.TXPowerMW)
		switch {
		case tel.UplinkLQ >= 80:
			col = colGood
		case tel.UplinkLQ >= 50:
			col = colWarn
		default:
			col = colBad
		}
	case tel.FrameAt.IsZero() || now.Sub(tel.FrameAt) >= stale:
		line, col = "NO TELEMETRY", colTextDim
	default:
		line, col = "NO LINK", colWarn // frames arriving, but no link stats (RX down)
	}
	drawTextOutlined(screen, line, x, y, sizeLabel, col)

	if tel.BatteryValid && now.Sub(tel.BattAt) < stale {
		batt := fmt.Sprintf("%.1fV  %.1fA", tel.Voltage, tel.Current)
		drawTextOutlined(screen, batt, x, y+24, sizeSmall, colText)
	}
}

// updateFPS recomputes the capture frame rate ~once per second from the frame
// Buffer's monotonically-increasing seq. A seq that went backwards means the device
// was reopened, so it just rebaselines.
func (g *Game) updateFPS(seq uint64) {
	now := time.Now()
	if g.vidFPSAt.IsZero() || seq < g.vidFPSSeq {
		g.vidFPSAt, g.vidFPSSeq = now, seq
		return
	}
	if d := now.Sub(g.vidFPSAt); d >= time.Second {
		g.vidFPS = float64(seq-g.vidFPSSeq) / d.Seconds()
		g.vidFPSAt, g.vidFPSSeq = now, seq
	}
}

// drawImageFit draws src scaled to fit the (x,y,w,h) box, preserving aspect ratio
// and centering it (letterbox/pillarbox). It returns the displayed image rect
// (ox,oy,dw,dh) in screen px, so overlays can map frame coordinates onto it.
func drawImageFit(dst, src *ebiten.Image, x, y, w, h float32) (ox, oy, dw, dh float32) {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw == 0 || sh == 0 {
		return x, y, 0, 0
	}
	scale := math.Min(float64(w)/float64(sw), float64(h)/float64(sh))
	dw = float32(float64(sw) * scale)
	dh = float32(float64(sh) * scale)
	ox = x + (w-dw)/2
	oy = y + (h-dh)/2
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(float64(ox), float64(oy))
	op.Filter = ebiten.FilterLinear
	dst.DrawImage(src, op)
	return
}

// drawDetections overlays the current person tracks, mapping their frame-pixel boxes
// onto the displayed image rect (ox,oy,dw,dh). Only currently-visible tracks (not
// coasting on a missed frame) are drawn.
func (g *Game) drawDetections(screen *ebiten.Image, ox, oy, dw, dh float32, fw, fh int) {
	if fw == 0 || fh == 0 {
		return
	}
	sx, sy := dw/float32(fw), dh/float32(fh)
	for _, tr := range g.tracks {
		locked := tr.ID == g.lockedID
		if tr.Missed != 0 && !locked {
			continue // don't clutter with coasting non-locked tracks
		}
		x := ox + float32(tr.Box.Min.X)*sx
		y := oy + float32(tr.Box.Min.Y)*sy
		w := float32(tr.Box.Dx()) * sx
		h := float32(tr.Box.Dy()) * sy
		col, thick, label := colAccent, float32(2), fmt.Sprintf("person %.0f%%", tr.Score*100)
		if locked {
			col, thick, label = colBad, 4, "LOCKED "+label // weapon-lock red, bolder
			if tr.Missed != 0 {
				label = "LOCKED (coasting)" // last-known box while re-acquiring
			}
		}
		strokeRect(screen, x, y, w, h, thick, col)
		drawTextOutlined(screen, label, float64(x), float64(y)-18, sizeSmall, col)

		// Mark the aim point on the locked target — horizontal center, AimHeight down
		// from the box top (where the turret drives onto the crosshair), so the height
		// calibration is visible.
		if locked {
			ax := x + w/2
			ay := y + h*float32(g.cfg.AutoAim.AimHeight)
			vector.DrawFilledCircle(screen, ax, ay, 5, colOutline, true)
			vector.DrawFilledCircle(screen, ax, ay, 3, col, true)
		}
	}
}
