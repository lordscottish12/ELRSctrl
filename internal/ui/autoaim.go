package ui

import (
	"image"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/crsf"
	"elrsctrl/internal/detect"
	"elrsctrl/internal/input"
)

// aimDebugEnv forces the verbose target-lock / auto-aim logging on regardless of the
// in-app toggle (back-compat / headless capture). The in-app "Auto-aim debug log"
// setting (config.AutoAim.Debug) is the Deck-friendly way; see g.aimDebug.
var aimDebugEnv = os.Getenv("AUTOAIM_DEBUG") != ""

// aimDebug reports whether to emit the verbose lock-lifecycle + per-frame aim logging,
// from either the in-app toggle or the AUTOAIM_DEBUG env override.
func (g *Game) aimDebug() bool { return g.cfg.AutoAim.Debug || aimDebugEnv }

// autoAim holds the turret control state while a target is locked. panPos and tiltPos
// are the commanded position, normalized to [-1,1] within the driven channel's
// [Min,Max]. The PD controller advances once per fresh detection (~10 Hz), so it keeps
// the previous error and a filtered error-rate per axis for the derivative (braking)
// term that kills the overshoot a pure integrator shows on this delayed loop.
type autoAim struct {
	panPos, tiltPos float64
	engaged         bool
	last            time.Time // wall-clock of the last control step (fresh detection)
	lastSeq         uint64    // detection seq of the last control step
	havePrev        bool      // smoothed aim point / prevErr / rate are seeded
	smX, smY        float64   // EMA-smoothed target aim point (frame px) — filters box jitter
	prevErrX        float64
	prevErrY        float64
	rateX           float64 // filtered d(errX)/dt
	rateY           float64
	errX, errY      float64   // last control error (smoothed), kept for the debug readout
	lastLog         time.Time // throttles the per-frame AUTOAIM_DEBUG readout
}

// updateTargeting refreshes the visible person tracks and, while armed, lets the
// operator pick a target: the D-pad cycles left/right through targets and the lock
// button (config.AutoAim.LockSource) toggles autolock on the best one. Disarmed, it
// drops any lock so a fresh arm starts clean. Tracks are refreshed regardless of arm
// state so the boxes still draw on the Monitor-free Run view.
func (g *Game) updateTargeting() {
	if g.detectRun != nil {
		g.tracks, g.tracksSeq = g.detectRun.Latest()
	} else {
		g.tracks, g.tracksSeq = nil, 0
	}
	if !g.armed {
		if g.lockedID != 0 && g.aimDebug() {
			log.Printf("autoaim: lock cleared (disarmed) id=%d", g.lockedID)
		}
		g.lockedID = 0
		g.prevDLeft, g.prevDRight, g.prevLockBtn = false, false, false
		return
	}

	vis := visibleTracks(g.tracks)
	// Keep the lock while the tracker still holds the target — even coasting
	// (Missed > 0) after a brief loss — and only drop it once the tracker gives up.
	if g.lockedID != 0 && indexOfTrack(g.tracks, g.lockedID) < 0 {
		if g.aimDebug() {
			log.Printf("autoaim: lock LOST id=%d — track aged out of tracker (visible now=%d)", g.lockedID, len(vis))
		}
		g.lockedID = 0
	}

	left := g.in.Connected && g.in.Pressed(input.SrcDLeft)
	right := g.in.Connected && g.in.Pressed(input.SrcDRight)
	if left && !g.prevDLeft {
		g.lockedID = stepLock(vis, g.lockedID, -1)
		g.logLock("d-pad left")
	}
	if right && !g.prevDRight {
		g.lockedID = stepLock(vis, g.lockedID, +1)
		g.logLock("d-pad right")
	}
	g.prevDLeft, g.prevDRight = left, right

	lockBtn := g.cfg.AutoAim.LockSource != input.SrcNone &&
		g.in.Connected && g.in.Pressed(g.cfg.AutoAim.LockSource)
	if lockBtn && !g.prevLockBtn {
		if g.lockedID != 0 {
			if g.aimDebug() {
				log.Printf("autoaim: lock released id=%d (lock button)", g.lockedID)
			}
			g.lockedID = 0 // release
		} else {
			g.lockedID = bestTrack(vis, g.aimPoint()) // grab the target nearest the crosshair
			g.logLock("lock button")
		}
	}
	g.prevLockBtn = lockBtn

	// Returning the camera to its default/forward position (the pan/tilt channels'
	// recenter button) also drops the lock, so auto-aim stops fighting the recenter
	// and the rate-mode recenter can actually take effect.
	if g.lockedID != 0 && g.recenterPressed() {
		if g.aimDebug() {
			log.Printf("autoaim: lock released id=%d (recenter)", g.lockedID)
		}
		g.lockedID = 0
	}
}

// logLock reports a lock acquisition/step under AUTOAIM_DEBUG (no-op when nothing is
// locked, e.g. stepping with no visible targets).
func (g *Game) logLock(via string) {
	if g.aimDebug() && g.lockedID != 0 {
		log.Printf("autoaim: lock acquired id=%d (%s, visible=%d)", g.lockedID, via, len(visibleTracks(g.tracks)))
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

	// Seed from the channels' current values on engage so the turret doesn't jerk, and
	// reset the smoothing/derivative state so the first detection doesn't see a bogus rate.
	if !g.aim.engaged {
		g.aim.panPos = seedPos(live, panCh, g.cfg.Channels)
		g.aim.tiltPos = seedPos(live, tiltCh, g.cfg.Channels)
		g.aim.last = now
		g.aim.lastSeq = g.tracksSeq
		g.aim.havePrev = false
		g.aim.rateX, g.aim.rateY = 0, 0
		g.aim.engaged = true
	}

	aimX, aimY := aimPointFrame(fw, fh, g.cfg.OSD.CrosshairX, g.cfg.OSD.CrosshairY)

	// Advance the PD controller only on a *fresh* detection (the measurement updates at
	// ~10 Hz, not the UI's 60 fps): integrating a stale error every frame, or computing
	// a derivative from it, is meaningless. While coasting (Missed > 0) or between
	// detections, hold the last command so the view doesn't snap back and detection gets
	// a moment to re-acquire.
	if tr.Missed == 0 && g.tracksSeq != g.aim.lastSeq {
		dt := now.Sub(g.aim.last).Seconds()
		g.aim.last = now
		g.aim.lastSeq = g.tracksSeq
		if dt <= 0 {
			dt = 1.0 / 10 // sane fallback for the first/odd interval
		} else if dt > 0.3 {
			dt = 0.3 // cap after a stall so the derivative doesn't blow up
		}

		// Aim point in the box: horizontal center, vertical AimHeight down from the top
		// (0.25 = upper torso, so a weak jet doesn't drop to the feet).
		tgtX, tgtY := boxAimPoint(tr.Box, aa.AimHeight)

		// EMA-smooth the aim point before the controller sees it. This filters the
		// detection box's frame-to-frame jitter so a *tight* deadband can give precise
		// aim without the turret hunting on noise.
		const posAlpha = 0.4
		if g.aim.havePrev {
			g.aim.smX = posAlpha*tgtX + (1-posAlpha)*g.aim.smX
			g.aim.smY = posAlpha*tgtY + (1-posAlpha)*g.aim.smY
		} else {
			g.aim.smX, g.aim.smY = tgtX, tgtY
		}
		errX := (g.aim.smX - aimX) / (float64(fw) / 2)
		errY := (g.aim.smY - aimY) / (float64(fh) / 2)

		// Filtered apparent target velocity (px-normalized / s) for the derivative brake.
		const rateAlpha = 0.4
		if g.aim.havePrev {
			g.aim.rateX = rateAlpha*((errX-g.aim.prevErrX)/dt) + (1-rateAlpha)*g.aim.rateX
			g.aim.rateY = rateAlpha*((errY-g.aim.prevErrY)/dt) + (1-rateAlpha)*g.aim.rateY
		}
		g.aim.prevErrX, g.aim.prevErrY = errX, errY
		g.aim.errX, g.aim.errY = errX, errY
		g.aim.havePrev = true

		if panCh >= 0 {
			g.aim.panPos = stepAim(g.aim.panPos, errX, g.aim.rateX, aa.PanGain, aa.Damp, aa.Deadband, aa.PanInvert, dt)
		}
		if tiltCh >= 0 {
			g.aim.tiltPos = stepAim(g.aim.tiltPos, errY, g.aim.rateY, aa.TiltGain, aa.Damp, aa.Deadband, aa.TiltInvert, dt)
		}
	}

	// Drive (or, while coasting/between detections, hold) the channels at the current
	// command.
	if panCh >= 0 {
		live[panCh] = posToTicks(g.aim.panPos, g.cfg.Channels[panCh])
	}
	if tiltCh >= 0 {
		live[tiltCh] = posToTicks(g.aim.tiltPos, g.cfg.Channels[tiltCh])
	}

	// Throttled readout: tgt is the smoothed aim point we drive to the crosshair; watch
	// whether |err| trends toward 0 (closing on the target) or grows (driving the wrong
	// way — flip that axis's invert, or confirm with the Run-screen TEST AIM button).
	if g.aimDebug() && now.Sub(g.aim.lastLog) >= 200*time.Millisecond {
		g.aim.lastLog = now
		log.Printf("autoaim: id=%d missed=%d box=(%d,%d,%d,%d) tgt=(%.0f,%.0f) crosshair=(%.0f,%.0f) err=(%+.2f,%+.2f) rate=(%+.2f,%+.2f) pos pan=%+.2f tilt=%+.2f",
			g.lockedID, tr.Missed, tr.Box.Min.X, tr.Box.Min.Y, tr.Box.Max.X, tr.Box.Max.Y,
			g.aim.smX, g.aim.smY, aimX, aimY, g.aim.errX, g.aim.errY, g.aim.rateX, g.aim.rateY, g.aim.panPos, g.aim.tiltPos)
	}
}

// --- turret direction test (Run screen) ---

// turretTest sweeps the configured pan/tilt channels through up→right→down→left so
// the operator can confirm the auto-aim axis/direction assignments by watching the
// turret (and its mounted camera) move. Like auto-aim it overrides only the pan/tilt
// channels upstream of the snapshot, so the sender's failsafe still wins. It needs the
// transmitter armed (disarmed = failsafe), and since the gamepad cursor is hidden while
// armed the test self-arms when started disarmed and disarms again when it ends —
// armedByTest records that, so a manual arm before the test is left armed afterwards.
type turretTest struct {
	active      bool
	start       time.Time
	armedByTest bool
}

// turretTestStepSecs is how long the turret holds each of the four directions.
const turretTestStepSecs = 1.0

// turretTestDeflect is the fraction of each channel's range the test drives to — short
// of the endpoints so the direction is obvious without slamming the servo into a stop.
const turretTestDeflect = 0.8

// turretTestLabels name each phase for the Run-screen readout.
var turretTestLabels = [...]string{"UP", "RIGHT", "DOWN", "LEFT"}

// startTurretTest kicks off the direction sweep. It refuses only when no pan/tilt
// channel is assigned (nothing to drive); if disarmed it self-arms for the test's
// duration so it works without leaving the cursor-hidden armed state to tap UI. armed
// is forced true each frame by applyArmKill while the test is active.
func (g *Game) startTurretTest() {
	if g.cfg.AutoAim.PanChannel == 0 && g.cfg.AutoAim.TiltChannel == 0 {
		g.setStatus("Assign a Pan/Tilt channel (Mapping) before testing")
		return
	}
	armedByTest := !g.armed
	g.armed = true
	g.turretTest = turretTest{active: true, start: time.Now(), armedByTest: armedByTest}
	if armedByTest {
		g.setStatus("Turret test (auto-armed): up → right → down → left")
	} else {
		g.setStatus("Turret test: up → right → down → left")
	}
}

// cancelTurretTest aborts an in-progress test, disarming again if the test was what
// armed (a manual pre-test arm is left untouched). Called from every disarm path
// (kill button, panic chord, gamepad disconnect) so a kill always wins over the test's
// self-arm. Idempotent.
func (g *Game) cancelTurretTest() {
	if !g.turretTest.active {
		return
	}
	if g.turretTest.armedByTest {
		g.armed = false
	}
	g.turretTest = turretTest{}
}

// updateTurretTest drives the pan/tilt channels through the direction sweep while
// active. It returns true while it owns the channels, so the caller skips auto-aim.
func (g *Game) updateTurretTest(live *[crsf.NumChannels]uint16, now time.Time) bool {
	if !g.turretTest.active {
		return false
	}
	if !g.armed { // disarmed out from under us (kill) — abort
		g.turretTest = turretTest{}
		return false
	}
	phase := turretTestPhase(now.Sub(g.turretTest.start).Seconds())
	if phase < 0 {
		// Sequence finished — hand the channels back to mapping/failsafe, and undo the
		// self-arm so the vehicle returns to disarmed (the safe resting state).
		if g.turretTest.armedByTest {
			g.armed = false
		}
		g.turretTest = turretTest{}
		g.setStatus("Turret test complete")
		return false
	}
	aa := g.cfg.AutoAim
	panCh, tiltCh := aa.PanChannel-1, aa.TiltChannel-1
	panPos, tiltPos := turretTestPos(phase, aa.PanInvert, aa.TiltInvert)
	if panCh >= 0 {
		live[panCh] = posToTicks(panPos, g.cfg.Channels[panCh])
	}
	if tiltCh >= 0 {
		live[tiltCh] = posToTicks(tiltPos, g.cfg.Channels[tiltCh])
	}
	return true
}

// turretTestPhase maps elapsed seconds to a phase index 0..3 (up/right/down/left), or
// -1 once the four-step sweep is done.
func turretTestPhase(elapsed float64) int {
	p := int(elapsed / turretTestStepSecs)
	if p < 0 || p > 3 {
		return -1
	}
	return p
}

// turretTestPos returns the normalized pan/tilt positions for a phase, honoring the
// same invert flags auto-aim uses — so if the turret moves the wrong way here, it
// would chase a target the wrong way too, and the fix is to flip that axis's invert.
func turretTestPos(phase int, panInvert, tiltInvert bool) (pan, tilt float64) {
	switch phase {
	case 0: // UP: a target above the crosshair gives errY = -1
		tilt = aimDeflect(-1, tiltInvert)
	case 1: // RIGHT: a target right of the crosshair gives errX = +1
		pan = aimDeflect(+1, panInvert)
	case 2: // DOWN
		tilt = aimDeflect(+1, tiltInvert)
	case 3: // LEFT
		pan = aimDeflect(-1, panInvert)
	}
	return
}

// aimDeflect is the normalized turret position the controller settles at for a constant
// full-scale error `err` on an axis with the given invert flag, scaled to the test
// deflection. It mirrors stepAim's steady state (errRate→0, so v=gain·err; invert
// negates the slew), so a wrong direction here means a wrong direction in auto-aim too.
func aimDeflect(err float64, invert bool) float64 {
	if invert {
		err = -err
	}
	return err * turretTestDeflect
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

// boxAimPoint returns the point inside a target box the turret drives onto the
// crosshair: horizontal center, and heightFrac down from the top (0 = top of head,
// 0.25 = upper torso, 0.5 = box center, 1 = feet).
func boxAimPoint(b image.Rectangle, heightFrac float64) (x, y float64) {
	x = float64(b.Min.X+b.Max.X) / 2
	y = float64(b.Min.Y) + heightFrac*float64(b.Dy())
	return
}

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

// stepAim advances a normalized turret position with a PD-on-error velocity command:
// the proportional term (gain·err) slews toward the target, the derivative term
// (damp·errRate) brakes as the error shrinks — together a lead/PD law that cuts the
// overshoot a pure integrator shows on a delayed (~10 Hz) visual-servo loop. errRate is
// the (filtered) apparent target velocity in normalized px/s. Holds inside the
// deadband, optionally inverts, integrates over dt and clamps to [-1,1].
func stepAim(pos, err, errRate, gain, damp, deadband float64, invert bool, dt float64) float64 {
	if math.Abs(err) < deadband {
		return pos
	}
	v := gain*err + damp*errRate
	if invert {
		v = -v
	}
	return clampF(pos+v*dt, -1, 1)
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
