package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
	// Slightly tighter rows: the detection model picker / rate steppers push this column
	// long enough that 50px would run under the hints.
	f2 := &form{screen: screen, p: p, x: x2, w: w2, y: tabH + 52, rowH: 44}

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
	if f2.toggle("Detection (people)", g.cfg.Detect.Enabled) {
		g.cfg.Detect.Enabled = !g.cfg.Detect.Enabled
		g.applyDetect()
	}
	// Model picker: any *.onnx files beside the binary, so you can A/B a fast low-res
	// export against a higher-res one without YAML. Re-export at the size you want with
	// `yolo export model=yolov8n.pt format=onnx imgsz=320`; the app reads each model's
	// input size automatically. The configured model is included even if not on disk yet.
	models := detectModelFiles()
	curModel := filepath.Base(g.cfg.Detect.ModelPath)
	if curModel == "" || curModel == "." {
		curModel = "model.onnx"
	}
	if indexOfStr(models, curModel) < 0 {
		models = append([]string{curModel}, models...)
	}
	if len(models) > 1 {
		mi := indexOfStr(models, curModel)
		if d := f2.wideStepper("Detect model", trim(models[mi], 28), 300); d != 0 {
			g.cfg.Detect.ModelPath = models[wrap(mi+d, len(models))]
			g.applyDetect() // rebuilds the detector — reloads the ONNX, re-reads its input size
		}
	}
	// Detection rate (Hz). Higher = less tracking lag / overshoot, at more CPU; pair a
	// higher rate with a smaller model. Applied live — no model reload.
	if d := f2.stepper("Detect rate Hz", strconv.Itoa(g.cfg.Detect.RateHz)); d != 0 {
		g.cfg.Detect.RateHz = clampI(g.cfg.Detect.RateHz+5*d, 5, 60)
		if g.detectRun != nil {
			g.detectRun.SetRate(g.cfg.Detect.RateHz)
		}
	}
	// Debug log toggles — capture a run for diagnosis without typing env vars on the
	// Deck. (DETECT_DEBUG / AUTOAIM_DEBUG still force them on if set.) Toggling on
	// reports the log-file path so it's discoverable without a terminal.
	if f2.toggle("Detect debug log", g.cfg.Detect.Debug) {
		g.cfg.Detect.Debug = !g.cfg.Detect.Debug
		g.applyDetectDebug()
		g.reportLogTarget(g.cfg.Detect.Debug)
	}
	if f2.toggle("Auto-aim debug log", g.cfg.AutoAim.Debug) {
		g.cfg.AutoAim.Debug = !g.cfg.AutoAim.Debug
		g.reportLogTarget(g.cfg.AutoAim.Debug)
	}

	// Hints below the OSD column.
	logHint := "Debug logs → " + trim(g.logPath, 52)
	if g.logPath == "" {
		logHint = "Debug logs → terminal only (started with --log off)"
	}
	hint := []string{
		"Name: type with a keyboard, or edit osd.vehicle_name in the YAML.",
		logHint,
		"Aeris Link: CRSF pins 3/1, UART-inverted OFF, 115200 (web UI).",
	}
	for i, line := range hint {
		drawText(screen, line, float64(x2), float64(f2.y+16+float32(i)*22), sizeSmall, colTextDim)
	}
}

// reportLogTarget surfaces where debug logs go when a debug toggle is switched on, so
// it's findable on the Deck without a terminal. No-op when toggling off.
func (g *Game) reportLogTarget(on bool) {
	if !on {
		return
	}
	if g.logPath == "" {
		g.setStatus("debug logging on — terminal only (relaunch without --log off for a file)")
		return
	}
	g.setStatus("debug logging on — writing to %s", g.logPath)
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

func indexOfStr(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// detectModelFiles lists the *.onnx files sitting next to the executable (and in the
// working directory), by base name, so the Settings model picker can offer them without
// any YAML — drop a 320 and a 640 export beside the binary and switch between them.
func detectModelFiles() []string {
	seen := map[string]bool{}
	var out []string
	dirs := []string{"."}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if n := e.Name(); strings.EqualFold(filepath.Ext(n), ".onnx") && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}
