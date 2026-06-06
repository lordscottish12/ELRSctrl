package ui

import (
	"image"
	"math"
	"sort"
	"time"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/crsf"
	"elrsctrl/internal/detect"
	"elrsctrl/internal/input"
)

// autoAim holds the integrated turret position while a target is locked. panPos and
// tiltPos are normalized to [-1,1] within the driven channel's [Min,Max].
type autoAim struct {
	panPos, tiltPos float64
	engaged         bool
	last            time.Time
}

// updateTargeting refreshes the visible person tracks and, while armed, lets the
// operator pick a target: the D-pad cycles left/right through targets and the lock
// button (config.AutoAim.LockSource) toggles autolock on the best one. Disarmed, it
// drops any lock so a fresh arm starts clean. Tracks are refreshed regardless of arm
// state so the boxes still draw on the Monitor-free Run view.
func (g *Game) updateTargeting() {
	if g.detectRun != nil {
		g.tracks, _ = g.detectRun.Latest()
	} else {
		g.tracks = nil
	}
	if !g.armed {
		g.lockedID = 0
		g.prevDLeft, g.prevDRight, g.prevLockBtn = false, false, false
		return
	}

	vis := visibleTracks(g.tracks)
	// Keep the lock while the tracker still holds the target — even coasting
	// (Missed > 0) after a brief loss — and only drop it once the tracker gives up.
	if g.lockedID != 0 && indexOfTrack(g.tracks, g.lockedID) < 0 {
		g.lockedID = 0
	}

	left := g.in.Connected && g.in.Pressed(input.SrcDLeft)
	right := g.in.Connected && g.in.Pressed(input.SrcDRight)
	if left && !g.prevDLeft {
		g.lockedID = stepLock(vis, g.lockedID, -1)
	}
	if right && !g.prevDRight {
		g.lockedID = stepLock(vis, g.lockedID, +1)
	}
	g.prevDLeft, g.prevDRight = left, right

	lockBtn := g.cfg.AutoAim.LockSource != input.SrcNone &&
		g.in.Connected && g.in.Pressed(g.cfg.AutoAim.LockSource)
	if lockBtn && !g.prevLockBtn {
		if g.lockedID != 0 {
			g.lockedID = 0 // release
		} else {
			g.lockedID = bestTrack(vis, g.aimPoint()) // grab the target nearest the crosshair
		}
	}
	g.prevLockBtn = lockBtn

	// Returning the camera to its default/forward position (the pan/tilt channels'
	// recenter button) also drops the lock, so auto-aim stops fighting the recenter
	// and the rate-mode recenter can actually take effect.
	if g.lockedID != 0 && g.recenterPressed() {
		g.lockedID = 0
	}
}

// recenterPressed reports whether a recenter button bound on either driven turret
// channel (its rate-mode RecenterSource) is held — the "return to default view".
func (g *Game) recenterPressed() bool {
	if !g.in.Connected {
		return false
	}
	for _, ch := range [...]int{g.cfg.AutoAim.PanChannel - 1, g.cfg.AutoAim.TiltChannel - 1} {
		if ch < 0 || ch >= len(g.cfg.Channels) {
			continue
		}
		if rs := g.cfg.Channels[ch].RecenterSource; rs != input.SrcNone && g.in.Pressed(rs) {
			return true
		}
	}
	return false
}

// updateAutoAim overrides the configured pan/tilt channels to drive the locked
// target toward the crosshair. It's a no-op unless armed, locked, the target is
// currently visible, and at least one of pan/tilt is configured — so it can never
// fight the sender's failsafe and only ever moves the turret channels.
func (g *Game) updateAutoAim(live *[crsf.NumChannels]uint16, now time.Time) {
	aa := g.cfg.AutoAim
	panCh, tiltCh := aa.PanChannel-1, aa.TiltChannel-1
	if !g.armed || g.lockedID == 0 || (panCh < 0 && tiltCh < 0) {
		g.aim.engaged = false
		return
	}
	tr, ok := trackByID(g.tracks, g.lockedID)
	if !ok {
		g.aim.engaged = false
		return
	}
	fw, fh := g.frameSize()
	if fw == 0 || fh == 0 {
		return
	}

	// Seed from the channels' current values on engage so the turret doesn't jerk.
	if !g.aim.engaged {
		g.aim.panPos = seedPos(live, panCh, g.cfg.Channels)
		g.aim.tiltPos = seedPos(live, tiltCh, g.cfg.Channels)
		g.aim.last = now
		g.aim.engaged = true
	}
	dt := now.Sub(g.aim.last).Seconds()
	g.aim.last = now
	if dt < 0 {
		dt = 0
	} else if dt > 0.1 { // cap after a UI stall
		dt = 0.1
	}

	// Integrate toward the target only on a fresh detection. While the track is
	// coasting (Missed > 0 — e.g. lost during a fast slew), hold the turret at its
	// current position instead of reverting to center, giving detection a moment to
	// re-acquire without the view snapping back.
	if tr.Missed == 0 {
		aimX, aimY := aimPointFrame(fw, fh, g.cfg.OSD.CrosshairX, g.cfg.OSD.CrosshairY)
		cx := float64(tr.Box.Min.X+tr.Box.Max.X) / 2
		cy := float64(tr.Box.Min.Y+tr.Box.Max.Y) / 2
		errX := (cx - aimX) / (float64(fw) / 2)
		errY := (cy - aimY) / (float64(fh) / 2)
		if panCh >= 0 {
			g.aim.panPos = integrateAim(g.aim.panPos, errX, aa.PanGain, aa.Deadband, aa.PanInvert, dt)
		}
		if tiltCh >= 0 {
			g.aim.tiltPos = integrateAim(g.aim.tiltPos, errY, aa.TiltGain, aa.Deadband, aa.TiltInvert, dt)
		}
	}

	// Drive (or, while coasting, hold) the channels at the current position.
	if panCh >= 0 {
		live[panCh] = posToTicks(g.aim.panPos, g.cfg.Channels[panCh])
	}
	if tiltCh >= 0 {
		live[tiltCh] = posToTicks(g.aim.tiltPos, g.cfg.Channels[tiltCh])
	}
}

// aimPoint is the crosshair aim point in frame pixels for the latest frame.
func (g *Game) aimPoint() image.Point {
	fw, fh := g.frameSize()
	if fw == 0 {
		return image.Point{}
	}
	x, y := aimPointFrame(fw, fh, g.cfg.OSD.CrosshairX, g.cfg.OSD.CrosshairY)
	return image.Pt(int(x), int(y))
}

// frameSize reports the current video frame dimensions (0,0 if none).
func (g *Game) frameSize() (int, int) {
	if g.videoCap == nil {
		return 0, 0
	}
	if f, _ := g.videoCap.Buffer().Latest(); f != nil {
		return f.W, f.H
	}
	return 0, 0
}

// --- auto-aim channel assignment (Mapping screen) ---

// aimRoleLabels are the per-channel auto-aim states, folding axis + invert into one
// stepper so the turret channels are pickable on-screen with no YAML editing.
var aimRoleLabels = []string{"—", "Pan", "Pan (rev)", "Tilt", "Tilt (rev)"}

// aimRoleIndex returns channel i's index into aimRoleLabels.
func (g *Game) aimRoleIndex(i int) int {
	aa := g.cfg.AutoAim
	switch i + 1 {
	case aa.PanChannel:
		if aa.PanInvert {
			return 2
		}
		return 1
	case aa.TiltChannel:
		if aa.TiltInvert {
			return 4
		}
		return 3
	}
	return 0
}

// setAimRole assigns channel i to an auto-aim role (clearing any prior role on it;
// because pan/tilt are single channels, reassigning automatically frees the old one).
func (g *Game) setAimRole(i, state int) {
	aa := &g.cfg.AutoAim
	if aa.PanChannel == i+1 {
		aa.PanChannel, aa.PanInvert = 0, false
	}
	if aa.TiltChannel == i+1 {
		aa.TiltChannel, aa.TiltInvert = 0, false
	}
	switch state {
	case 1:
		aa.PanChannel, aa.PanInvert = i+1, false
	case 2:
		aa.PanChannel, aa.PanInvert = i+1, true
	case 3:
		aa.TiltChannel, aa.TiltInvert = i+1, false
	case 4:
		aa.TiltChannel, aa.TiltInvert = i+1, true
	}
}

// --- pure helpers (unit-tested) ---

// aimPointFrame maps the on-screen crosshair (video-area center + offset px) back
// to frame pixels, inverting the Run-screen letterbox. Zero offset = frame center.
func aimPointFrame(fw, fh, offX, offY int) (float64, float64) {
	areaW, areaH := float64(screenW), float64(screenH-tabH)
	scale := math.Min(areaW/float64(fw), areaH/float64(fh))
	ox := (areaW - float64(fw)*scale) / 2
	oy := float64(tabH) + (areaH-float64(fh)*scale)/2
	crossX := areaW/2 + float64(offX)
	crossY := float64(tabH) + areaH/2 + float64(offY)
	return (crossX - ox) / scale, (crossY - oy) / scale
}

// integrateAim advances a normalized turret position toward reducing the pixel
// error: it slews at gain*err (units/sec), holds inside the deadband, optionally
// inverts, and clamps to [-1,1]. As the turret moves the target toward the
// crosshair, err shrinks and it settles — a simple visual-servo loop.
func integrateAim(pos, err, gain, deadband float64, invert bool, dt float64) float64 {
	if math.Abs(err) < deadband {
		return pos
	}
	delta := gain * err * dt
	if invert {
		delta = -delta
	}
	return clampF(pos+delta, -1, 1)
}

// posToTicks maps a normalized position [-1,1] to CRSF ticks within [Min,Max].
func posToTicks(pos float64, ch channels.Channel) uint16 {
	mid := float64(ch.Min+ch.Max) / 2
	half := float64(ch.Max-ch.Min) / 2
	return crsf.ClampTicks(int(math.Round(mid + pos*half)))
}

// seedPos converts a channel's current tick value to a normalized position.
func seedPos(live *[crsf.NumChannels]uint16, ch int, chans []channels.Channel) float64 {
	if ch < 0 || ch >= len(chans) {
		return 0
	}
	half := float64(chans[ch].Max-chans[ch].Min) / 2
	if half == 0 {
		return 0
	}
	mid := float64(chans[ch].Min+chans[ch].Max) / 2
	return clampF((float64(live[ch])-mid)/half, -1, 1)
}

// visibleTracks returns the currently-detected tracks (Missed == 0) sorted left to
// right, so the D-pad cycles in a predictable spatial order.
func visibleTracks(tracks []detect.Track) []detect.Track {
	var v []detect.Track
	for _, t := range tracks {
		if t.Missed == 0 {
			v = append(v, t)
		}
	}
	sort.Slice(v, func(i, j int) bool {
		return v[i].Box.Min.X+v[i].Box.Max.X < v[j].Box.Min.X+v[j].Box.Max.X
	})
	return v
}

func indexOfTrack(ts []detect.Track, id int) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func trackByID(ts []detect.Track, id int) (detect.Track, bool) {
	for _, t := range ts {
		if t.ID == id {
			return t, true
		}
	}
	return detect.Track{}, false
}

// stepLock returns the id of the next/prev visible target (dir ±1, wrapping); from
// no lock it grabs the first (right) or last (left).
func stepLock(vis []detect.Track, cur, dir int) int {
	if len(vis) == 0 {
		return 0
	}
	i := indexOfTrack(vis, cur)
	if i < 0 {
		if dir > 0 {
			return vis[0].ID
		}
		return vis[len(vis)-1].ID
	}
	return vis[wrap(i+dir, len(vis))].ID
}

// bestTrack picks the visible target whose center is nearest the aim point.
func bestTrack(vis []detect.Track, aim image.Point) int {
	best, bestD := 0, math.MaxFloat64
	for _, t := range vis {
		cx := float64(t.Box.Min.X+t.Box.Max.X) / 2
		cy := float64(t.Box.Min.Y+t.Box.Max.Y) / 2
		dx, dy := cx-float64(aim.X), cy-float64(aim.Y)
		if d := dx*dx + dy*dy; d < bestD {
			best, bestD = t.ID, d
		}
	}
	return best
}
