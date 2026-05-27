package ui

import (
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"

	"elrsctrl/internal/config"
)

var baudPresets = []int{115200, 420000, 400000, 921600, 416666, 57600}

func (g *Game) drawSettings(screen *ebiten.Image, p pointer) {
	x, w := float32(16), float32(620)
	drawText(screen, "Serial / Transmit", float64(x), float64(tabH+10), sizeTitle, colText)

	f := &form{screen: screen, p: p, x: x, w: w, y: tabH + 52, rowH: 50}

	// Port. g.ports carries the Device to open plus a friendly Label to show. Build
	// a choice list with a "(none)" sentinel; if a port is configured but not
	// currently present (module unplugged), surface it so the user sees it instead
	// of a misleading "(none)".
	type portChoice struct{ device, label string }
	choices := []portChoice{{"", "(none)"}}
	for _, pi := range g.ports {
		choices = append(choices, portChoice{pi.Device, pi.Label})
	}
	portCur := 0
	for i, c := range choices {
		if c.device == g.cfg.Serial.Port {
			portCur = i
			break
		}
	}
	if g.cfg.Serial.Port != "" && choices[portCur].device != g.cfg.Serial.Port {
		choices = append(choices, portChoice{g.cfg.Serial.Port, g.cfg.Serial.Port + "  (not connected)"})
		portCur = len(choices) - 1
	}
	if d := f.wideStepper("Serial Port", trim(choices[portCur].label, 40), 480); d != 0 {
		g.cfg.Serial.Port = choices[wrap(portCur+d, len(choices))].device
		g.applyPort()
	}

	// Baud.
	baudCur := indexOfInt(baudPresets, g.cfg.Serial.Baud)
	if baudCur < 0 {
		baudCur = 0
	}
	if d := f.stepper("Baud", strconv.Itoa(baudPresets[baudCur])); d != 0 {
		g.cfg.Serial.Baud = baudPresets[wrap(baudCur+d, len(baudPresets))]
		g.applyPort()
	}

	// Rate (applied on next launch — the transmit ticker is fixed at startup).
	if d := f.stepper("Rate Hz (restart)", strconv.Itoa(g.cfg.Sender.RateHz)); d != 0 {
		g.cfg.Sender.RateHz = clampI(g.cfg.Sender.RateHz+10*d, 50, 500)
	}

	// Address.
	if d := f.stepper("CRSF Address", g.cfg.Sender.Address); d != 0 {
		if g.cfg.Sender.Address == "0xEE" {
			g.cfg.Sender.Address = "0xC8"
		} else {
			g.cfg.Sender.Address = "0xEE"
		}
		g.snd.SetAddr(g.cfg.AddrByte())
	}

	// Gamepad.
	padOpts := []string{"(auto)"}
	for _, d := range g.gamepads {
		padOpts = append(padOpts, d.Name)
	}
	padCur := g.gamepadSel + 1
	if padCur < 0 || padCur >= len(padOpts) {
		padCur = 0
	}
	if d := f.stepper("Gamepad", padOpts[padCur]); d != 0 {
		g.gamepadSel = wrap(padCur+d, len(padOpts)) - 1
		g.applyGamepad()
	}

	// Arm mode.
	armModeLabel := "Hold (deadman)"
	if g.cfg.Safety.ArmToggle {
		armModeLabel = "Toggle"
	}
	if f.toggle("Arm mode: "+armModeLabel, g.cfg.Safety.ArmToggle) {
		g.cfg.Safety.ArmToggle = !g.cfg.Safety.ArmToggle
	}

	// Arm / Kill bindings.
	if d, bind := f.sourceRow("Arm button", g.cfg.Safety.ArmSource.Label()); d != 0 || bind {
		if d != 0 {
			g.cfg.Safety.ArmSource = cycleSource(g.cfg.Safety.ArmSource, d)
		}
		if bind {
			g.bind = bindArm
		}
	}
	if d, bind := f.sourceRow("Kill button", g.cfg.Safety.KillSource.Label()); d != 0 || bind {
		if d != 0 {
			g.cfg.Safety.KillSource = cycleSource(g.cfg.Safety.KillSource, d)
		}
		if bind {
			g.bind = bindKill
		}
	}

	// Action buttons.
	by := float32(screenH - 76)
	if button(screen, p, x, by, 180, 54, "Rescan ports", colPanel2) {
		g.refreshPorts()
		g.setStatus("rescanned ports")
	}
	if button(screen, p, x+196, by, 150, 54, "Save", colAccent) {
		g.save()
	}
	if button(screen, p, x+362, by, 150, 54, "Reload", colPanel2) {
		g.reload()
	}
	if button(screen, p, x+528, by, 180, 54, "Reset default", colPanel2) {
		*g.cfg = config.Default()
		g.applyAll()
		g.setStatus("reset to defaults")
	}

	// Connection hint on the right.
	hint := []string{
		"Connecting the Aeris Link:",
		"• Power the module from its XT30 (2S / 8.4V).",
		"• USB-C → in the module web UI set CRSF serial",
		"  pins to 3/1 and turn UART-inverted OFF.",
		"• Pick the port at left; baud 115200, addr 0xEE.",
	}
	hx := float64(760)
	for i, line := range hint {
		col := colTextDim
		if i == 0 {
			col = colText
		}
		drawText(screen, line, hx, float64(tabH+56+i*26), sizeSmall, col)
	}
}

func (g *Game) applyPort()    { g.snd.SetPort(g.cfg.Serial.Port, g.cfg.Serial.Baud) }
func (g *Game) applyGamepad() {
	if g.gamepadSel < 0 || g.gamepadSel >= len(g.gamepads) {
		g.reader.Auto()
		return
	}
	g.reader.Select(g.gamepads[g.gamepadSel].ID)
}

func (g *Game) applyAll() {
	g.applyPort()
	g.snd.SetAddr(g.cfg.AddrByte())
	g.applyGamepad()
}

func (g *Game) save() {
	if g.cfgPath == "" {
		g.setStatus("no config path (run with --config)")
		return
	}
	if err := config.Save(g.cfgPath, *g.cfg); err != nil {
		g.setStatus("save failed: %v", err)
		return
	}
	g.setStatus("saved %s", g.cfgPath)
}

func (g *Game) reload() {
	c, err := config.LoadOrDefault(g.cfgPath)
	if err != nil {
		g.setStatus("reload failed: %v", err)
		return
	}
	*g.cfg = c
	g.applyAll()
	g.setStatus("reloaded config")
}

func indexOfInt(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
