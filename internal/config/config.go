// Package config loads and saves the YAML profile: serial/transmit settings,
// safety bindings, and the 16-channel mapping. A first run with no file yields a
// sensible RC-car default that the user can remap in the UI.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"elrsctrl/internal/channels"
	"elrsctrl/internal/crsf"
	"elrsctrl/internal/input"
)

// SerialConfig selects the port and baud.
type SerialConfig struct {
	Port string `yaml:"port"` // e.g. "COM5" or "/dev/ttyACM0"; empty = pick in UI
	Baud int    `yaml:"baud"` // handset-link UART baud (115200 over the Aeris Link USB/CP2102; not the 420000 over-air rate)
}

// SenderConfig controls how frames are transmitted.
type SenderConfig struct {
	RateHz  int    `yaml:"rate_hz"`          // frames per second (e.g. 250)
	Address string `yaml:"address"`          // "0xEE" (transmitter) or "0xC8"
	StaleMs int    `yaml:"stale_timeout_ms"` // snapshot age -> failsafe
}

// SafetyConfig configures arm/kill, separate from the 16 channels.
type SafetyConfig struct {
	ArmSource  input.Source `yaml:"arm_source"`  // button to arm
	KillSource input.Source `yaml:"kill_source"` // button to instantly disarm
	ArmToggle  bool         `yaml:"arm_toggle"`  // true: press toggles; false: hold (deadman)
}

// VideoConfig selects the analog FPV capture device shown on the Run screen.
// Disabled by default; zero values are valid so older config files keep loading.
type VideoConfig struct {
	Enabled bool   `yaml:"enabled"` // open the capture device on launch / when toggled on
	Device  string `yaml:"device"`  // V4L2 node, e.g. "/dev/video0" (Linux only)
}

// OSDConfig configures the Betaflight-style overlay on the Run screen. Each
// element is independently toggleable; VehicleName is shown bottom-center.
type OSDConfig struct {
	VehicleName string `yaml:"vehicle_name"`
	Crosshair   bool   `yaml:"crosshair"`
	ArmState    bool   `yaml:"arm_state"`
	ShowName    bool   `yaml:"show_name"`
	VideoInfo   bool   `yaml:"video_info"` // debug: capture resolution / format / fps
	Telemetry   bool   `yaml:"telemetry"`  // ELRS link quality / RSSI / battery

	// Crosshair offset in pixels from the video center, to calibrate the reticle to
	// a not-quite-centered payload (e.g. a zip-tied water pistol). 0,0 = centered.
	CrosshairX int `yaml:"crosshair_x"`
	CrosshairY int `yaml:"crosshair_y"`
}

// Config is the full persisted profile.
type Config struct {
	Serial   SerialConfig       `yaml:"serial"`
	Sender   SenderConfig       `yaml:"sender"`
	Safety   SafetyConfig       `yaml:"safety"`
	Video    VideoConfig        `yaml:"video"`
	OSD      OSDConfig          `yaml:"osd"`
	Channels []channels.Channel `yaml:"channels"`
}

// AddrByte parses the configured CRSF address, defaulting to the transmitter
// address (0xEE) if malformed.
func (c Config) AddrByte() byte {
	s := strings.TrimSpace(strings.ToLower(c.Sender.Address))
	s = strings.TrimPrefix(s, "0x")
	if v, err := strconv.ParseUint(s, 16, 8); err == nil {
		return byte(v)
	}
	return crsf.AddrTransmitter
}

// Default returns the built-in RC-car profile.
func Default() Config {
	chans := channels.NewProfile()

	// CH1 — Steering: left stick X, proportional.
	chans[0].Name = "Steering"
	chans[0].Type = channels.TypeAnalog
	chans[0].Source = input.SrcLeftStickX

	// CH2 — Throttle: combined triggers (R2 forward / L2 reverse), neutral failsafe.
	chans[1].Name = "Throttle"
	chans[1].Type = channels.TypeAnalog
	chans[1].Source = input.SrcTriggers
	chans[1].Failsafe = int(crsf.TicksMid) // neutral = stop for a bidirectional ESC

	// CH3 — Aux1: A button, toggle switch.
	chans[2].Name = "Aux1"
	chans[2].Type = channels.TypeSwitch2
	chans[2].Source = input.SrcA

	// CH4 — Aux2: B button, toggle switch.
	chans[3].Name = "Aux2"
	chans[3].Type = channels.TypeSwitch2
	chans[3].Source = input.SrcB

	return Config{
		Serial: SerialConfig{Port: "", Baud: 115200},
		Sender: SenderConfig{RateHz: 250, Address: "0xEE", StaleMs: 250},
		Safety: SafetyConfig{
			ArmSource:  input.SrcMenu,
			KillSource: input.SrcView,
			ArmToggle:  true,
		},
		OSD: OSDConfig{
			Crosshair: true,
			ArmState:  true,
			ShowName:  true,
			Telemetry: true,
		},
		Channels: chans,
	}
}

// LoadOrDefault reads path. A missing path (or empty string) returns the default
// profile with no error so first-run works.
func LoadOrDefault(path string) (Config, error) {
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Default(), fmt.Errorf("parse config %s: %w", path, err)
	}
	c.normalize()
	return c, nil
}

// Save writes the profile to path as YAML.
func Save(path string, c Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// normalize fills in missing/invalid values after loading so the rest of the app
// can assume a complete profile.
func (c *Config) normalize() {
	if c.Serial.Baud <= 0 {
		c.Serial.Baud = 115200
	}
	if c.Sender.RateHz <= 0 {
		c.Sender.RateHz = 250
	}
	if c.Sender.StaleMs <= 0 {
		c.Sender.StaleMs = 250
	}
	if strings.TrimSpace(c.Sender.Address) == "" {
		c.Sender.Address = "0xEE"
	}
	if c.Safety.ArmSource == "" {
		c.Safety.ArmSource = input.SrcMenu
	}
	if c.Safety.KillSource == "" {
		c.Safety.KillSource = input.SrcView
	}

	// OSD: an all-zero struct means the profile predates the OSD — turn on the
	// standard elements so the feed isn't bare. A profile that sets any OSD field
	// (incl. a vehicle name) is respected as configured.
	if c.OSD == (OSDConfig{}) {
		c.OSD.Crosshair = true
		c.OSD.ArmState = true
		c.OSD.ShowName = true
		c.OSD.Telemetry = true
	}

	// Always exactly 16 channels.
	if len(c.Channels) > crsf.NumChannels {
		c.Channels = c.Channels[:crsf.NumChannels]
	}
	for len(c.Channels) < crsf.NumChannels {
		c.Channels = append(c.Channels, channels.DefaultChannel("CH"+strconv.Itoa(len(c.Channels)+1)))
	}
	// Backfill endpoints that may be zero from older/sparse files.
	for i := range c.Channels {
		ch := &c.Channels[i]
		if ch.Min == 0 && ch.Max == 0 {
			ch.Min, ch.Max = int(crsf.TicksMin), int(crsf.TicksMax)
		}
		if ch.Low == 0 && ch.High == 0 {
			ch.Low, ch.High = int(crsf.TicksMin), int(crsf.TicksMax)
		}
		if ch.Mid == 0 {
			ch.Mid = int(crsf.TicksMid)
		}
		if ch.Failsafe == 0 {
			ch.Failsafe = int(crsf.TicksMid)
		}
		if ch.Type == "" {
			ch.Type = channels.TypeNone
		}
		if ch.Source == "" {
			ch.Source = input.SrcNone
		}
		if ch.Source2 == "" {
			ch.Source2 = input.SrcNone
		}
		if ch.RecenterSource == "" {
			ch.RecenterSource = input.SrcNone
		}
		// Migrate the old "momentary: true" bool to PressMode. One-way: the bool
		// is then cleared so saving rewrites it in the new form. Empty PressMode
		// is fine — the engine treats it as toggle, which keeps the YAML clean.
		if ch.PressMode == "" && ch.Momentary {
			ch.PressMode = channels.PressMomentary
		}
		ch.Momentary = false
	}
}
