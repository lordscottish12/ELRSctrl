// Package ui is the Ebiten front-end: an immediate-mode, touch-and-mouse UI with
// four screens (Run, Monitor, Mapping, Settings). It owns the input reader and the
// channel engine, polls the gamepad each frame, and publishes a control snapshot
// to the shared Store that the sender goroutine transmits.
package ui

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/config"
	"elrsctrl/internal/detect"
	"elrsctrl/internal/input"
	"elrsctrl/internal/sender"
	"elrsctrl/internal/serial"
	"elrsctrl/internal/state"
	"elrsctrl/internal/version"
	"elrsctrl/internal/video"
)

const (
	tabRun = iota
	tabMonitor
	tabMapping
	tabSettings
	tabCount
)

type bindKind int

const (
	bindNone bindKind = iota
	bindChannelSource
	bindChannelSource2
	bindChannelRecenter
	bindArm
	bindKill
)

// Game is the Ebiten game and application controller.
type Game struct {
	cfg     *config.Config
	cfgPath string
	store   *state.Store
	snd     *sender.Sender
	reader  *input.Reader
	engine  channels.Engine

	in             input.State
	armed          bool
	prevArm        bool
	panicKillSince time.Time // wall-clock when LB+RB chord started (zero = chord not held)

	tab        int
	selCh      int
	bind       bindKind
	editName   bool // Settings: OSD vehicle-name text field has keyboard focus
	gamepadSel int  // -1 = auto

	prevPointerPressed bool // for our own mouse/touch click-edge detection (see readPointer)
	prevRB, prevLB     bool // edge tracking for gamepad tab navigation
	prevB              bool // edge tracking for B = cancel-bind

	// Gamepad-driven virtual pointer, so the whole UI is operable from the
	// controller alone (the Steam Deck has no reliable mouse/touch into the app).
	// See readPointer / updateCursor / drawCursor.
	cursorX, cursorY float64
	bindReady        bool // bind mode has seen a neutral frame; safe to capture next actuation

	ports        []serial.PortInfo
	gamepads     []input.Device
	status       string
	statusFrames int

	// Analog FPV capture for the Run screen. videoCap is opened/closed by
	// applyVideo (off the transmit path); videoTex caches the last frame uploaded
	// to the GPU, re-uploaded only when videoSeq advances.
	videoCap *video.Capture
	videoTex *ebiten.Image
	videoSeq uint64

	// Video capture supervision: auto-(re)open and reopen-on-stall so a flaky UVC
	// dongle recovers without a physical replug (see superviseVideo).
	videoErr     string // last open error, shown on Run while retrying
	videoSeenSeq uint64
	videoSeenAt  time.Time
	videoRetryAt time.Time

	// Capture fps, measured in the UI from the frame Buffer's seq for the OSD
	// video-info readout (recomputed ~once/sec in drawRun).
	vidFPS    float64
	vidFPSSeq uint64
	vidFPSAt  time.Time

	// Person-detection runner (Run-screen target boxes). Decoupled goroutine bound
	// to the current video Buffer; nil when detection is off / unavailable.
	detectRun *detect.Runner

	// Target lock + auto-aim. tracks is the latest detection snapshot; lockedID is
	// the operator-selected target (0 = none); aim integrates the turret position.
	tracks                          []detect.Track
	lockedID                        int
	prevDLeft, prevDRight, prevLockBtn bool
	aim                             autoAim
}

// New builds the Game. cfgPath may be "" (Save is then disabled).
func New(cfg *config.Config, cfgPath string, store *state.Store, snd *sender.Sender, reader *input.Reader) *Game {
	g := &Game{
		cfg:        cfg,
		cfgPath:    cfgPath,
		store:      store,
		snd:        snd,
		reader:     reader,
		gamepadSel: -1,
		in:         input.NewState(),
		cursorX:    screenW / 2,
		cursorY:    screenH / 2,
	}
	g.refreshPorts()
	g.applyVideo()
	return g
}

// applyVideo reconciles the capture device with cfg.Video: close any open capture,
// then open the configured device if video is enabled. Best-effort — a failure
// just leaves videoCap as the failed/stub handle (its Err shows on the Run
// screen) and never blocks the UI or the transmit loop.
func (g *Game) applyVideo() {
	if g.videoCap != nil {
		g.videoCap.Close()
		g.videoCap = nil
	}
	g.videoTex, g.videoSeq = nil, 0
	// Detection feeds off the video Buffer, so reconcile it whenever video changes
	// (a new device means a new Buffer to bind the runner to).
	defer g.applyDetect()
	if !g.cfg.Video.Enabled || g.cfg.Video.Device == "" {
		g.videoErr = ""
		return
	}
	cap, err := video.Open(g.cfg.Video.Device)
	if err != nil {
		if g.videoErr != err.Error() { // surface a new failure once; superviseVideo retries quietly
			g.setStatus("video: %v", err)
		}
		g.videoErr = err.Error()
		return
	}
	g.videoCap = cap
	g.videoErr = ""
	g.videoSeenSeq, g.videoSeenAt = 0, time.Now() // start the stall/grace clock
}

const (
	videoStallTimeout = 4 * time.Second // no new frame this long -> reopen
	videoRetryEvery   = 2 * time.Second // throttle open attempts
)

// superviseVideo keeps the capture alive: it (re)opens the device when video is
// enabled but not running, and reopens a stalled stream (no new frame within
// videoStallTimeout). This mirrors the sender's serial reconnection — cheap analog
// UVC dongles often need a fresh open to actually start streaming, so this recovers
// on its own instead of requiring a physical replug, and picks a replug up at once.
func (g *Game) superviseVideo(now time.Time) {
	if !g.cfg.Video.Enabled || g.cfg.Video.Device == "" {
		return
	}
	if g.videoCap != nil {
		if _, seq := g.videoCap.Buffer().Latest(); seq != g.videoSeenSeq {
			g.videoSeenSeq, g.videoSeenAt = seq, now
			return // frames are flowing
		}
		if now.Sub(g.videoSeenAt) < videoStallTimeout {
			return // still within the start-up / stall grace window
		}
	}
	if now.Sub(g.videoRetryAt) < videoRetryEvery {
		return
	}
	g.videoRetryAt = now
	g.applyVideo()
}

// applyDetect reconciles the detection runner with cfg.Detect and the current video
// capture: stop any existing runner, then start a fresh one bound to the live video
// Buffer if detection is enabled and a capture is open. Best-effort — a missing
// model / onnxruntime lib just leaves detection off (status shows why).
func (g *Game) applyDetect() {
	if g.detectRun != nil {
		g.detectRun.Close()
		g.detectRun = nil
	}
	if !g.cfg.Detect.Enabled || g.videoCap == nil {
		return
	}
	det, err := detect.NewDetector(detect.DetectorConfig{
		ModelPath: g.cfg.Detect.ModelPath,
		LibPath:   g.cfg.Detect.LibPath,
		InputSize: g.cfg.Detect.InputSize,
		Conf:      g.cfg.Detect.Conf,
	})
	if err != nil {
		g.setStatus("detect: %v", err)
		return
	}
	r := detect.NewRunner(det, g.videoCap.Buffer(), g.cfg.Detect.RateHz)
	r.Start()
	g.detectRun = r
}

func (g *Game) setStatus(format string, a ...any) {
	g.status = fmt.Sprintf(format, a...)
	g.statusFrames = 180   // ~3s at 60 fps
	log.Print(g.status)    // also to stdout — readable in full when launched from a terminal
}

func (g *Game) refreshPorts() {
	ports, err := serial.ListPorts()
	if err != nil {
		g.setStatus("port list error: %v", err)
		return
	}
	g.ports = ports
}

// Update advances one frame: poll input, resolve bind mode, evaluate the
// mapping, and publish the snapshot. All pointer interaction lives in Draw
// (immediate-mode), so the dynamic layouts can't drift between the two passes.
func (g *Game) Update() error {
	if g.statusFrames > 0 {
		g.statusFrames--
	}
	g.gamepads = input.List()
	g.in = g.reader.Poll()

	g.updateBind()
	g.updateNameEdit()
	g.applyArmKill()
	g.updateCursor()
	g.superviseVideo(time.Now())
	g.updateTargeting()

	// Gamepad tab navigation: LB/RB cycle tabs, but only while disarmed (setup
	// mode) and not mid-bind — so driving inputs and bind capture are never
	// hijacked. Edge-tracked manually since input.State is level-triggered.
	rb := g.in.Connected && g.in.Pressed(input.SrcRB)
	lb := g.in.Connected && g.in.Pressed(input.SrcLB)
	if g.bind == bindNone && !g.armed && !g.editName {
		switch {
		case rb && !g.prevRB:
			g.tab = wrap(g.tab+1, tabCount)
		case lb && !g.prevLB:
			g.tab = wrap(g.tab-1, tabCount)
		}
	}
	g.prevRB, g.prevLB = rb, lb

	now := time.Now()
	live := g.engine.Apply(g.cfg.Channels, g.in, g.armed, now)
	// Auto-aim overrides the pan/tilt channels in-place when armed and locked — it
	// sits upstream of the snapshot, so the sender's failsafe still wins on disarm.
	g.updateAutoAim(&live, now)
	fs := channels.FailsafeValues(g.cfg.Channels)
	g.store.SetSnapshot(state.Snapshot{
		Live:    live,
		Failsafe: fs,
		Armed:   g.armed,
		InputOK: g.in.Connected,
	})
	return nil
}

// updateBind drives bind-capture mode. It waits for one neutral frame before
// capturing (bindReady) so the A press that *entered* bind mode — or a stick
// still deflected from cursor navigation — isn't grabbed as the binding. B
// cancels, which reserves B as the gamepad "back" button: to map B itself, step
// the Source stepper to it rather than pressing it in bind mode.
func (g *Game) updateBind() {
	bCancel := g.in.Connected && g.in.Pressed(input.SrcB)
	defer func() { g.prevB = bCancel }()

	if g.bind == bindNone {
		g.bindReady = false
		return
	}
	if bCancel && !g.prevB {
		g.bind = bindNone
		g.setStatus("bind cancelled")
		return
	}
	src, ok := g.detectActiveSource()
	if !ok {
		g.bindReady = true // neutral — arm capture for the next actuation
		return
	}
	if g.bindReady {
		g.assignBind(src)
		g.bind = bindNone
	}
}

// nameMaxLen caps the OSD vehicle name so it can't overflow the field or overlay.
const nameMaxLen = 24

// updateNameEdit captures keyboard input into the OSD vehicle name while its
// Settings field has focus (g.editName). Runs in Update (not Draw) since key edges
// commit on the Update tick; the field is drawn and focus toggled in Draw. Enter or
// Escape commits; Backspace deletes; printable runes append up to nameMaxLen.
func (g *Game) updateNameEdit() {
	if !g.editName {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) ||
		inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) ||
		inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.editName = false
		return
	}
	name := []rune(g.cfg.OSD.VehicleName)
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(name) > 0 {
		name = name[:len(name)-1]
	}
	for _, r := range ebiten.AppendInputChars(nil) {
		if r >= 0x20 && r != 0x7f && len(name) < nameMaxLen {
			name = append(name, r)
		}
	}
	g.cfg.OSD.VehicleName = string(name)
}

// padCursorActive reports whether the gamepad-driven virtual cursor is live: only
// while disarmed and not binding, mirroring the LB/RB tab-nav gate so the driving
// sticks are never hijacked once armed, and bind capture isn't fighting a click.
func (g *Game) padCursorActive() bool {
	return g.in.Connected && !g.armed && g.bind == bindNone
}

// updateCursor moves the virtual cursor from the left stick (proportional) and the
// d-pad (full-tilt), in screen pixels. Ebiten reports stick-up as -1, which maps
// straight to a smaller y (cursor up), so no inversion is needed.
func (g *Game) updateCursor() {
	if !g.padCursorActive() {
		return
	}
	const speed = 22.0 // px/frame at full deflection (~1 s to cross the screen at 60 fps)
	dx := axisDeadzone(g.in.Analog(input.SrcLeftStickX))
	dy := axisDeadzone(g.in.Analog(input.SrcLeftStickY))
	if g.in.Pressed(input.SrcDLeft) {
		dx -= 1
	}
	if g.in.Pressed(input.SrcDRight) {
		dx += 1
	}
	if g.in.Pressed(input.SrcDUp) {
		dy -= 1
	}
	if g.in.Pressed(input.SrcDDown) {
		dy += 1
	}
	g.cursorX = clampF(g.cursorX+clampF(dx, -1, 1)*speed, 0, screenW-1)
	g.cursorY = clampF(g.cursorY+clampF(dy, -1, 1)*speed, 0, screenH-1)
}

// axisDeadzone zeroes small stick noise and rescales the remainder to 0..1 so
// motion ramps from a standstill just past the deadzone.
func axisDeadzone(v float64) float64 {
	const dz = 0.2
	a := math.Abs(v)
	if a < dz {
		return 0
	}
	s := (a - dz) / (1 - dz)
	if v < 0 {
		return -s
	}
	return s
}

// detectActiveSource returns the most strongly actuated control, for bind mode.
func (g *Game) detectActiveSource() (input.Source, bool) {
	for _, b := range input.ButtonSources {
		if g.in.Pressed(b) {
			return b, true
		}
	}
	best, bestV := input.SrcNone, 0.5
	for _, a := range input.AnalogSources {
		if v := math.Abs(g.in.Analog(a)); v > bestV {
			best, bestV = a, v
		}
	}
	if best != input.SrcNone {
		return best, true
	}
	return input.SrcNone, false
}

func (g *Game) assignBind(src input.Source) {
	switch g.bind {
	case bindChannelSource:
		g.cfg.Channels[g.selCh].Source = src
		g.autoType(g.selCh, src)
	case bindChannelSource2:
		g.cfg.Channels[g.selCh].Source2 = src
	case bindChannelRecenter:
		g.cfg.Channels[g.selCh].RecenterSource = src
	case bindArm:
		g.cfg.Safety.ArmSource = src
	case bindKill:
		g.cfg.Safety.KillSource = src
	}
	g.setStatus("bound to %s", src.Label())
}

// autoType picks a reasonable mapping type when a source is freshly bound.
func (g *Game) autoType(i int, src input.Source) {
	c := &g.cfg.Channels[i]
	switch {
	case src.IsAnalog():
		c.Type = channels.TypeAnalog
	case src.IsButton() && (c.Type == channels.TypeNone || c.Type == channels.TypeAnalog):
		c.Type = channels.TypeSwitch2
	}
}

// panicKillHold is how long LB+RB must be held to force-disarm. Long enough that
// an incidental simultaneous bumper press while driving won't trip it; short
// enough to feel like a real escape.
const panicKillHold = 500 * time.Millisecond

func (g *Game) applyArmKill() {
	if !g.in.Connected {
		g.armed = false
		g.prevArm = false
		g.panicKillSince = time.Time{}
		return
	}
	// Panic-kill chord: LB+RB held >= panicKillHold while armed always disarms,
	// bypassing the configurable Kill binding. This is the guaranteed escape when
	// the regular Kill button isn't reaching the app (e.g. Steam Input intercepts
	// Menu/View on the Deck) and the cursor is gone because we're armed.
	both := g.in.Pressed(input.SrcLB) && g.in.Pressed(input.SrcRB)
	switch {
	case !both || !g.armed:
		g.panicKillSince = time.Time{}
	case g.panicKillSince.IsZero():
		g.panicKillSince = time.Now()
	case time.Since(g.panicKillSince) >= panicKillHold:
		g.armed = false
		g.panicKillSince = time.Time{}
		g.setStatus("PANIC KILL — disarmed (LB+RB)")
	default:
		remaining := panicKillHold - time.Since(g.panicKillSince)
		g.setStatus("Hold LB+RB to KILL… %d ms", remaining/time.Millisecond)
	}
	if g.cfg.Safety.KillSource != input.SrcNone && g.in.Pressed(g.cfg.Safety.KillSource) {
		g.armed = false
	}
	armPressed := g.cfg.Safety.ArmSource != input.SrcNone && g.in.Pressed(g.cfg.Safety.ArmSource)
	if g.cfg.Safety.ArmToggle {
		if armPressed && !g.prevArm {
			g.armed = !g.armed
		}
	} else {
		g.armed = armPressed // deadman / hold-to-arm
	}
	g.prevArm = armPressed
}

const tabH = 56

// Draw renders the current screen and handles all pointer interaction.
func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(colBG)
	p := g.readPointer()
	ip := p
	if g.bind != bindNone {
		ip = pointer{} // suppress interaction with the screen beneath the overlay
	}

	g.drawTabs(screen, ip)
	switch g.tab {
	case tabRun:
		g.drawRun(screen, ip)
	case tabMonitor:
		g.drawMonitor(screen, ip)
	case tabMapping:
		g.drawMapping(screen, ip)
	case tabSettings:
		g.drawSettings(screen, ip)
	}

	if g.bind != bindNone {
		fillRect(screen, 0, 0, screenW, screenH, colDim)
		drawTextC(screen, "Move a stick / trigger or press a button to bind…",
			screenW/2, screenH/2-30, sizeTitle, colText)
		drawTextC(screen, "press B (or tap Cancel) to back out",
			screenW/2, screenH/2+6, sizeSmall, colTextDim)
		if button(screen, p, screenW/2-90, screenH/2+40, 180, 54, "Cancel", colBad) {
			g.bind = bindNone
		}
	}

	if g.statusFrames > 0 {
		fillRect(screen, 0, screenH-30, screenW, 30, colPanel2)
		// Stop long messages (e.g. an ONNX load error) from running under the
		// bottom-right version string — truncate to the space left of it.
		avail := float64(screenW-12) - textWidth(version.String(), sizeSmall) - 24
		drawText(screen, truncToWidth(g.status, sizeSmall, avail), 12, screenH-26, sizeSmall, colWarn)
	}

	// Build identifier in the bottom-right corner, so a running build can be
	// matched back to its commit at a glance. Drawn last (but under the cursor)
	// so it sits on top of every screen.
	drawTextR(screen, version.String(), screenW-12, screenH-24, sizeSmall, colTextDim)

	// Virtual cursor on top of everything, so the controller can drive the UI.
	if g.padCursorActive() {
		drawCursor(screen, float32(g.cursorX), float32(g.cursorY), g.in.Pressed(input.SrcA))
	}
}

func (g *Game) drawTabs(screen *ebiten.Image, p pointer) {
	names := []string{"Run", "Monitor", "Mapping", "Settings"}
	tw := float32(screenW) / float32(len(names))
	for i, n := range names {
		x := float32(i) * tw
		c := colPanel
		if i == g.tab {
			c = colAccent
		}
		fillRect(screen, x, 0, tw, tabH, c)
		strokeRect(screen, x, 0, tw, tabH, 1, colGrid)
		drawTextC(screen, n, float64(x+tw/2), tabH/2, sizeTitle, colText)
		if p.clicked && p.in(x, 0, tw, tabH) {
			g.tab = i
		}
	}
}

// Layout returns the fixed logical resolution; Ebiten scales it to the window.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenW, screenH
}
