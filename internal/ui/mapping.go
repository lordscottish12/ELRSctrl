package ui

import (
	"fmt"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

// drawMapping renders the channel list (left) and the editor for the selected
// channel (right), handling all interaction in the same pass.
func (g *Game) drawMapping(screen *ebiten.Image, p pointer) {
	listX, listW := float32(8), float32(380)
	top := float32(tabH + 8)
	rowH := float32(screenH-tabH-16) / float32(crsf.NumChannels)

	for i := 0; i < crsf.NumChannels; i++ {
		y := top + float32(i)*rowH
		ch := g.cfg.Channels[i]
		base := colPanel
		if i == g.selCh {
			base = colPanel2
		}
		fillRect(screen, listX, y, listW, rowH-3, base)
		strokeRect(screen, listX, y, listW, rowH-3, 1, colGrid)
		drawText(screen, fmt.Sprintf("CH%d  %s", i+1, ch.Name), float64(listX+10), float64(y+5), sizeLabel, colText)
		sub := fmt.Sprintf("%s · %s", string(ch.Type), ch.Source.Label())
		drawText(screen, sub, float64(listX+10), float64(y+rowH-21), sizeSmall, colTextDim)
		if p.clicked && p.in(listX, y, listW, rowH-3) {
			g.selCh = i
		}
	}

	g.drawChannelEditor(screen, p)
}

func (g *Game) drawChannelEditor(screen *ebiten.Image, p pointer) {
	c := &g.cfg.Channels[g.selCh]
	x, w := float32(400), float32(screenW-408)

	drawText(screen, fmt.Sprintf("CH%d — %s", g.selCh+1, c.Name), float64(x), float64(tabH+10), sizeTitle, colText)

	f := &form{screen: screen, p: p, x: x, w: w, y: tabH + 52, rowH: 50}

	if d := f.stepper("Type", string(c.Type)); d != 0 {
		c.Type = cycleType(c.Type, d)
	}
	if d, bind := f.sourceRow("Source", c.Source.Label()); d != 0 || bind {
		if d != 0 {
			c.Source = cycleSource(c.Source, d)
			g.autoType(g.selCh, c.Source)
		}
		if bind {
			g.bind = bindChannelSource
		}
	}

	switch c.Type {
	case channels.TypeAnalog:
		if f.toggle("Reverse", c.Reverse) {
			c.Reverse = !c.Reverse
		}
		if d := f.stepper("Expo", fmt.Sprintf("%.1f", c.Expo)); d != 0 {
			c.Expo = clampF(c.Expo+0.1*float64(d), -1, 1)
		}
		if d := f.stepper("Deadzone", fmt.Sprintf("%.2f", c.Deadzone)); d != 0 {
			c.Deadzone = clampF(c.Deadzone+0.05*float64(d), 0, 0.9)
		}
		if d := f.stepper("Trim", fmt.Sprintf("%+d", c.Trim)); d != 0 {
			c.Trim = clampI(c.Trim+10*d, -400, 400)
		}
		if d := f.stepper("Min (ticks)", strconv.Itoa(c.Min)); d != 0 {
			c.Min = clampI(c.Min+10*d, 0, int(crsf.TicksCeil))
		}
		if d := f.stepper("Max (ticks)", strconv.Itoa(c.Max)); d != 0 {
			c.Max = clampI(c.Max+10*d, 0, int(crsf.TicksCeil))
		}

	case channels.TypeSwitch2:
		if f.toggle("Momentary", c.Momentary) {
			c.Momentary = !c.Momentary
		}
		if f.toggle("Reverse", c.Reverse) {
			c.Reverse = !c.Reverse
		}
		if d := f.stepper("Low (ticks)", strconv.Itoa(c.Low)); d != 0 {
			c.Low = clampI(c.Low+10*d, 0, int(crsf.TicksCeil))
		}
		if d := f.stepper("High (ticks)", strconv.Itoa(c.High)); d != 0 {
			c.High = clampI(c.High+10*d, 0, int(crsf.TicksCeil))
		}

	case channels.TypeSwitch3:
		if d, bind := f.sourceRow("Source 2 (low)", c.Source2.Label()); d != 0 || bind {
			if d != 0 {
				c.Source2 = cycleSource(c.Source2, d)
			}
			if bind {
				g.bind = bindChannelSource2
			}
		}
		if f.toggle("Reverse", c.Reverse) {
			c.Reverse = !c.Reverse
		}
		if d := f.stepper("Low (ticks)", strconv.Itoa(c.Low)); d != 0 {
			c.Low = clampI(c.Low+10*d, 0, int(crsf.TicksCeil))
		}
		if d := f.stepper("Mid (ticks)", strconv.Itoa(c.Mid)); d != 0 {
			c.Mid = clampI(c.Mid+10*d, 0, int(crsf.TicksCeil))
		}
		if d := f.stepper("High (ticks)", strconv.Itoa(c.High)); d != 0 {
			c.High = clampI(c.High+10*d, 0, int(crsf.TicksCeil))
		}

	case channels.TypeFixed:
		if d := f.stepper("Value (ticks)", strconv.Itoa(c.Fixed)); d != 0 {
			c.Fixed = clampI(c.Fixed+10*d, 0, int(crsf.TicksCeil))
		}
	}

	if d := f.stepper("Failsafe (ticks)", strconv.Itoa(c.Failsafe)); d != 0 {
		c.Failsafe = clampI(c.Failsafe+10*d, 0, int(crsf.TicksCeil))
	}

	drawText(screen, "Tip: tap BIND, then move the stick / press the button you want.",
		float64(x), float64(screenH-54), sizeSmall, colTextDim)
}

// form lays out labeled controls top-to-bottom within a column.
type form struct {
	screen *ebiten.Image
	p      pointer
	x, w   float32
	y      float32
	rowH   float32
}

func (f *form) ctrlH() float32   { return f.rowH - 12 }
func (f *form) advance() float32 { y := f.y; f.y += f.rowH; return y }
func (f *form) labelY(y float32) float64 {
	return float64(y + (f.ctrlH()-sizeLabel)/2)
}

func (f *form) stepper(label, value string) int {
	y := f.advance()
	drawText(f.screen, label, float64(f.x), f.labelY(y), sizeLabel, colTextDim)
	const sw = 220
	return stepper(f.screen, f.p, f.x+f.w-sw, y, sw, f.ctrlH(), value)
}

// wideStepper is stepper with a caller-chosen value-box width, for long values
// like serial-port names that don't fit the default 220px box.
func (f *form) wideStepper(label, value string, sw float32) int {
	y := f.advance()
	drawText(f.screen, label, float64(f.x), f.labelY(y), sizeLabel, colTextDim)
	return stepper(f.screen, f.p, f.x+f.w-sw, y, sw, f.ctrlH(), value)
}

func (f *form) toggle(label string, on bool) bool {
	y := f.advance()
	drawText(f.screen, label, float64(f.x), f.labelY(y), sizeLabel, colTextDim)
	const tw = 220
	lbl := "OFF"
	if on {
		lbl = "ON"
	}
	return toggleButton(f.screen, f.p, f.x+f.w-tw, y, tw, f.ctrlH(), lbl, on)
}

func (f *form) sourceRow(label, value string) (delta int, bindClicked bool) {
	y := f.advance()
	drawText(f.screen, label, float64(f.x), f.labelY(y), sizeLabel, colTextDim)
	const (
		bindW = 86
		sw    = 220
	)
	bx := f.x + f.w - bindW
	sx := bx - 8 - sw
	delta = stepper(f.screen, f.p, sx, y, sw, f.ctrlH(), value)
	bindClicked = button(f.screen, f.p, bx, y, bindW, f.ctrlH(), "BIND", colAccent)
	return
}

// --- small helpers ---

var typeOrder = []channels.MapType{
	channels.TypeNone, channels.TypeAnalog, channels.TypeSwitch2, channels.TypeSwitch3, channels.TypeFixed,
}

func cycleType(t channels.MapType, d int) channels.MapType {
	i := 0
	for k, v := range typeOrder {
		if v == t {
			i = k
			break
		}
	}
	return typeOrder[wrap(i+d, len(typeOrder))]
}

func cycleSource(s input.Source, d int) input.Source {
	i := 0
	for k, v := range input.AllSources {
		if v == s {
			i = k
			break
		}
	}
	return input.AllSources[wrap(i+d, len(input.AllSources))]
}

func wrap(i, n int) int { return ((i % n) + n) % n }

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
