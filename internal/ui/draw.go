package ui

import (
	"bytes"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/goregular"

	"elrsctrl/internal/input"
)

// Logical screen size (Steam Deck native resolution).
const (
	screenW = 1280
	screenH = 800
)

// Palette.
var (
	colBG      = color.RGBA{0x12, 0x14, 0x18, 0xff}
	colPanel   = color.RGBA{0x1e, 0x22, 0x28, 0xff}
	colPanel2  = color.RGBA{0x2a, 0x2f, 0x36, 0xff}
	colText    = color.RGBA{0xe6, 0xe8, 0xea, 0xff}
	colTextDim = color.RGBA{0x9a, 0xa0, 0xa6, 0xff}
	colAccent  = color.RGBA{0x4a, 0x90, 0xd9, 0xff}
	colGood    = color.RGBA{0x3f, 0xb9, 0x50, 0xff}
	colBad     = color.RGBA{0xd9, 0x4a, 0x4a, 0xff}
	colWarn    = color.RGBA{0xd9, 0xa3, 0x4a, 0xff}
	colGrid    = color.RGBA{0x3a, 0x40, 0x48, 0xff}
	colDim     = color.RGBA{0x00, 0x00, 0x00, 0xb0} // overlay scrim
)

var (
	fontSource *text.GoTextFaceSource
	faceCache  = map[float64]*text.GoTextFace{}
)

func init() {
	s, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		panic(err)
	}
	fontSource = s
}

func face(size float64) *text.GoTextFace {
	if f, ok := faceCache[size]; ok {
		return f
	}
	f := &text.GoTextFace{Source: fontSource, Size: size}
	faceCache[size] = f
	return f
}

// --- primitives ---

func fillRect(dst *ebiten.Image, x, y, w, h float32, c color.Color) {
	vector.DrawFilledRect(dst, x, y, w, h, c, false)
}

func strokeRect(dst *ebiten.Image, x, y, w, h, sw float32, c color.Color) {
	vector.StrokeRect(dst, x, y, w, h, sw, c, false)
}

// drawText draws s with its top-left at (x, y).
func drawText(dst *ebiten.Image, s string, x, y, size float64, c color.Color) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleWithColor(c)
	text.Draw(dst, s, face(size), op)
}

// drawTextC draws s centered on (cx, cy).
func drawTextC(dst *ebiten.Image, s string, cx, cy, size float64, c color.Color) {
	w, h := text.Measure(s, face(size), 0)
	drawText(dst, s, cx-w/2, cy-h/2, size, c)
}

// drawTextR draws s right-aligned, ending at (rx, y-top).
func drawTextR(dst *ebiten.Image, s string, rx, y, size float64, c color.Color) {
	w, _ := text.Measure(s, face(size), 0)
	drawText(dst, s, rx-w, y, size, c)
}

func textWidth(s string, size float64) float64 {
	w, _ := text.Measure(s, face(size), 0)
	return w
}

func lighten(c color.RGBA, d uint8) color.RGBA {
	add := func(v uint8) uint8 {
		if int(v)+int(d) > 255 {
			return 255
		}
		return v + d
	}
	return color.RGBA{add(c.R), add(c.G), add(c.B), c.A}
}

// --- pointer (unified mouse + single-touch) ---

type pointer struct {
	x, y    int
	pressed bool // held down this frame
	clicked bool // just pressed this frame (a tap/click)
}

// readPointer samples the mouse/touch for this frame. The click *edge* is derived
// here from the level-triggered "is pressed" state plus our own previous-frame
// flag, NOT from inpututil's IsMouseButtonJustPressed/AppendJustPressedTouchIDs:
// all UI interaction runs in Draw, but inpututil commits its just-pressed edge on
// the Update tick, so polled from Draw it never reports a click. Tracking the edge
// ourselves keeps every widget (tabs, arm, steppers, BIND) clickable.
func (g *Game) readPointer() pointer {
	var p pointer
	// Gamepad virtual cursor is the baseline pointer when active (Steam Deck has no
	// dependable mouse/touch into the app); A is its click. Mouse/touch below take
	// precedence whenever they're actually used (dev machine, or a USB mouse on the
	// Deck), so nothing regresses on the desktop.
	if g.padCursorActive() {
		p.x, p.y = int(g.cursorX), int(g.cursorY)
		p.pressed = g.in.Pressed(input.SrcA)
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		p.pressed = true
		p.x, p.y = ebiten.CursorPosition()
	}
	if ids := ebiten.AppendTouchIDs(nil); len(ids) > 0 {
		p.pressed = true
		p.x, p.y = ebiten.TouchPosition(ids[0])
	}
	p.clicked = p.pressed && !g.prevPointerPressed
	g.prevPointerPressed = p.pressed
	return p
}

func (p pointer) in(x, y, w, h float32) bool {
	fx, fy := float32(p.x), float32(p.y)
	return fx >= x && fx < x+w && fy >= y && fy < y+h
}

// --- immediate-mode widgets ---

const (
	sizeTitle  = 26.0
	sizeLabel  = 18.0
	sizeValue  = 18.0
	sizeSmall  = 15.0
	sizeButton = 19.0
)

// button draws a labelled button and reports a click this frame.
func button(dst *ebiten.Image, p pointer, x, y, w, h float32, label string, base color.RGBA) bool {
	c := base
	if p.pressed && p.in(x, y, w, h) {
		c = lighten(base, 0x20)
	}
	fillRect(dst, x, y, w, h, c)
	strokeRect(dst, x, y, w, h, 1, colGrid)
	drawTextC(dst, label, float64(x+w/2), float64(y+h/2), sizeButton, colText)
	return p.clicked && p.in(x, y, w, h)
}

// toggleButton is a button whose fill reflects on/off state.
func toggleButton(dst *ebiten.Image, p pointer, x, y, w, h float32, label string, on bool) bool {
	base := colPanel2
	if on {
		base = colGood
	}
	return button(dst, p, x, y, w, h, label, base)
}

// stepper draws "[<]  value  [>]" and returns -1, 0 or +1 when an arrow is clicked.
func stepper(dst *ebiten.Image, p pointer, x, y, w, h float32, value string) int {
	aw := h // square arrow buttons
	delta := 0
	if button(dst, p, x, y, aw, h, "<", colPanel2) {
		delta = -1
	}
	if button(dst, p, x+w-aw, y, aw, h, ">", colPanel2) {
		delta = 1
	}
	fillRect(dst, x+aw, y, w-2*aw, h, colPanel)
	strokeRect(dst, x+aw, y, w-2*aw, h, 1, colGrid)
	drawTextC(dst, value, float64(x+w/2), float64(y+h/2), sizeValue, colText)
	return delta
}

// channelBar draws a horizontal bar for a value within [min,max] with a center
// tick, used on the monitor screen.
func channelBar(dst *ebiten.Image, x, y, w, h float32, value, min, max int) {
	fillRect(dst, x, y, w, h, colPanel)
	strokeRect(dst, x, y, w, h, 1, colGrid)
	if max <= min {
		return
	}
	frac := float32(value-min) / float32(max-min)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	fillRect(dst, x, y, w*frac, h, colAccent)
	// center tick
	fillRect(dst, x+w/2-1, y, 2, h, colTextDim)
}

// drawCursor draws the gamepad virtual pointer as a reticle whose center is the
// hotspot (the pointer's x,y). A dark halo keeps it visible on light fills; the
// ring fills in while the click button (A) is held.
func drawCursor(dst *ebiten.Image, x, y float32, pressed bool) {
	if pressed {
		vector.DrawFilledCircle(dst, x, y, 11, color.RGBA{0x4a, 0x90, 0xd9, 0x70}, true)
	}
	vector.StrokeCircle(dst, x, y, 12, 4, color.RGBA{0x00, 0x00, 0x00, 0xc0}, true)
	vector.StrokeCircle(dst, x, y, 12, 2, colAccent, true)
	vector.DrawFilledCircle(dst, x, y, 3, colText, true)
}
