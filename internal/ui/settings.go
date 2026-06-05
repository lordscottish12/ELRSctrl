package ui

import (
	"fmt"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"

	"elrsctrl/internal/config"
	"elrsctrl/internal/video"
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

	// Video (analog FPV feed shown on the Run screen). Mirrors the port picker:
	// a device + label list with a "(none)" sentinel, plus a configured-but-absent
	// fallback so an unplugged dongle still shows instead of silently reverting.
	type vidChoice struct{ device, label string }
	vchoices := []vidChoice{{"", "(none)"}}
	for _, dev := range video.ListDevices() {
		vchoices = append(vchoices, vidChoice{dev, dev})
	}
	vidCur := 0
	for i, c := range vchoices {
		if c.device == g.cfg.Video.Device {
			vidCur = i
			break
		}
	}
	if g.cfg.Video.Device != "" && vchoices[vidCur].device != g.cfg.Video.Device {
		vchoices = append(vchoices, vidChoice{g.cfg.Video.Device, g.cfg.Video.Device + "  (not present)"})
		vidCur = len(vchoices) - 1
	}
	if d := f.wideStepper("Video device", trim(vchoices[vidCur].label, 36), 320); d != 0 {
		g.cfg.Video.Device = vchoices[wrap(vidCur+d, len(vchoices))].device
		g.applyVideo()
	}
	if f.toggle("Video feed", g.cfg.Video.Enabled) {
		g.cfg.Video.Enabled = !g.cfg.Video.Enabled
		g.applyVideo()
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

	// --- Right column: OSD (Run-screen overlay) ---
	x2, w2 := float32(700), float32(560)
	drawText(screen, "OSD (Run overlay)", float64(x2), float64(tabH+10), sizeTitle, colText)
	f2 := &form{screen: screen, p: p, x: x2, w: w2, y: tabH + 52, rowH: 50}

	// Vehicle name: click the field to focus, click anywhere else to commit. Actual
	// typing is captured in Update (g.updateNameEdit) while g.editName is set.
	if f2.textRow("Vehicle name", g.cfg.OSD.VehicleName, g.editName) {
		g.editName = !g.editName
	} else if p.clicked {
		g.editName = false
	}
	if f2.toggle("Crosshair", g.cfg.OSD.Crosshair) {
		g.cfg.OSD.Crosshair = !g.cfg.OSD.Crosshair
	}
	// Nudge the crosshair to the payload's real aim point (±5 px/click).
	if d := f2.stepper("Crosshair X", fmt.Sprintf("%+d px", g.cfg.OSD.CrosshairX)); d != 0 {
		g.cfg.OSD.CrosshairX = clampI(g.cfg.OSD.CrosshairX+5*d, -600, 600)
	}
	if d := f2.stepper("Crosshair Y", fmt.Sprintf("%+d px", g.cfg.OSD.CrosshairY)); d != 0 {
		g.cfg.OSD.CrosshairY = clampI(g.cfg.OSD.CrosshairY+5*d, -350, 350)
	}
	if f2.toggle("Arm state", g.cfg.OSD.ArmState) {
		g.cfg.OSD.ArmState = !g.cfg.OSD.ArmState
	}
	if f2.toggle("Vehicle name shown", g.cfg.OSD.ShowName) {
		g.cfg.OSD.ShowName = !g.cfg.OSD.ShowName
	}
	if f2.toggle("Video info (debug)", g.cfg.OSD.VideoInfo) {
		g.cfg.OSD.VideoInfo = !g.cfg.OSD.VideoInfo
	}
	if f2.toggle("Telemetry", g.cfg.OSD.Telemetry) {
		g.cfg.OSD.Telemetry = !g.cfg.OSD.Telemetry
	}

	// Hints below the OSD column.
	hint := []string{
		"Name: type with a keyboard, or edit osd.vehicle_name in the YAML.",
		"",
		"Aeris Link: power from XT30 (2S); in the module web UI set CRSF",
		"serial pins 3/1, UART-inverted OFF; pick the port at left, 115200.",
	}
	for i, line := range hint {
		drawText(screen, line, float64(x2), float64(f2.y+16+float32(i)*22), sizeSmall, colTextDim)
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
