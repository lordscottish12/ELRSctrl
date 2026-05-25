// Package ui is the Ebiten front-end: an immediate-mode, touch-and-mouse UI with
// three screens (Monitor, Mapping, Settings). It owns the input reader and the
// channel engine, polls the gamepad each frame, and publishes a control snapshot
// to the shared Store that the sender goroutine transmits.
package ui

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/config"
	"elrsctrl/internal/input"
	"elrsctrl/internal/sender"
	"elrsctrl/internal/serial"
	"elrsctrl/internal/state"
)

const (
	tabMonitor = iota
	tabMapping
	tabSettings
	tabCount
)

type bindKind int

const (
	bindNone bindKind = iota
	bindChannelSource
	bindChannelSource2
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

	in      input.State
	armed   bool
	prevArm bool

	tab        int
	selCh      int
	bind       bindKind
	gamepadSel int // -1 = auto

	prevPointerPressed bool // for our own mouse/touch click-edge detection (see readPointer)
	prevRB, prevLB     bool // edge tracking for gamepad tab navigation

	ports        []string
	gamepads     []input.Device
	status       string
	statusFrames int
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
	}
	g.refreshPorts()
	return g
}

func (g *Game) setStatus(format string, a ...any) {
	g.status = fmt.Sprintf(format, a...)
	g.statusFrames = 180 // ~3s at 60 fps
}

func (g *Game) refreshPorts() {
	ports, err := serial.List()
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

	if g.bind != bindNone {
		if src, ok := g.detectActiveSource(); ok {
			g.assignBind(src)
			g.bind = bindNone
		}
	}

	g.applyArmKill()

	// Gamepad tab navigation: LB/RB cycle tabs, but only while disarmed (setup
	// mode) and not mid-bind — so driving inputs and bind capture are never
	// hijacked. Edge-tracked manually since input.State is level-triggered.
	rb := g.in.Connected && g.in.Pressed(input.SrcRB)
	lb := g.in.Connected && g.in.Pressed(input.SrcLB)
	if g.bind == bindNone && !g.armed {
		switch {
		case rb && !g.prevRB:
			g.tab = wrap(g.tab+1, tabCount)
		case lb && !g.prevLB:
			g.tab = wrap(g.tab-1, tabCount)
		}
	}
	g.prevRB, g.prevLB = rb, lb

	live := g.engine.Apply(g.cfg.Channels, g.in)
	fs := channels.FailsafeValues(g.cfg.Channels)
	g.store.SetSnapshot(state.Snapshot{
		Live:    live,
		Failsafe: fs,
		Armed:   g.armed,
		InputOK: g.in.Connected,
	})
	return nil
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

func (g *Game) applyArmKill() {
	if !g.in.Connected {
		g.armed = false
		g.prevArm = false
		return
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
		if button(screen, p, screenW/2-90, screenH/2+40, 180, 54, "Cancel", colBad) {
			g.bind = bindNone
		}
	}

	if g.statusFrames > 0 {
		fillRect(screen, 0, screenH-30, screenW, 30, colPanel2)
		drawText(screen, g.status, 12, screenH-26, sizeSmall, colWarn)
	}
}

func (g *Game) drawTabs(screen *ebiten.Image, p pointer) {
	const tw = screenW / 3
	names := []string{"Monitor", "Mapping", "Settings"}
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
