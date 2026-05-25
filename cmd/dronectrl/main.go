// Command dronectrl turns a Steam Deck (or any gamepad) into an RC transmitter
// for an ExpressLRS TX module, sending CRSF RC frames over a serial port.
//
// Normal use opens the UI:
//
//	dronectrl --config config.yaml
//
// Hardware bring-up (no UI) — prove the link moves a servo before mapping:
//
//	dronectrl --port COM5 --sweep 1
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"dronectrl/internal/config"
	"dronectrl/internal/input"
	"dronectrl/internal/sender"
	"dronectrl/internal/state"
	"dronectrl/internal/ui"
)

func main() {
	log.SetFlags(log.Ltime)

	var (
		cfgPath    = flag.String("config", "config.yaml", "path to the YAML profile (created on first Save)")
		port       = flag.String("port", "", "serial port override, e.g. COM5 or /dev/ttyACM0")
		baud       = flag.Int("baud", 0, "baud override (default from config; 115200 works over the Aeris Link USB/CP2102)")
		addr       = flag.String("addr", "", "CRSF address override: 0xEE (transmitter) or 0xC8")
		rate       = flag.Int("rate", 0, "transmit rate Hz override")
		resetPulse = flag.Bool("reset-pulse", true, "pulse a clean EN reset on serial open (needed for ESP modules on their flashing UART; disable for non-ESP links)")
		fullscreen = flag.Bool("fullscreen", false, "start fullscreen (handy on the Steam Deck)")
		sweepCh    = flag.Int("sweep", 0, "hardware bring-up: sweep this channel (1-16) then exit; requires --port")
		sweepRate  = flag.Int("sweep-rate", 250, "transmit rate Hz for --sweep")
	)
	flag.Parse()

	cfg, err := config.LoadOrDefault(*cfgPath)
	if err != nil {
		log.Printf("config: %v (continuing with defaults where needed)", err)
	}
	if *port != "" {
		cfg.Serial.Port = *port
	}
	if *baud != 0 {
		cfg.Serial.Baud = *baud
	}
	if *addr != "" {
		cfg.Sender.Address = *addr
	}
	if *rate != 0 {
		cfg.Sender.RateHz = *rate
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// --- Hardware bring-up sweep mode (no UI) ---
	if *sweepCh > 0 {
		if cfg.Serial.Port == "" {
			log.Fatal("--sweep needs a serial port: pass --port or set 'serial.port' in the config")
		}
		log.Printf("Sweeping CH%d on %s @ %d baud (addr 0x%02X). Ctrl+C to stop.",
			*sweepCh, cfg.Serial.Port, cfg.Serial.Baud, cfg.AddrByte())
		if err := sender.Sweep(ctx, cfg.Serial.Port, cfg.Serial.Baud, cfg.AddrByte(),
			*sweepCh, *sweepRate, 2*time.Second, *resetPulse); err != nil {
			log.Fatalf("sweep: %v", err)
		}
		log.Print("sweep stopped, channels centered")
		return
	}

	// --- Normal mode: sender goroutine + UI ---
	store := state.New()
	snd := sender.New(sender.Config{
		Addr:         cfg.AddrByte(),
		RateHz:       cfg.Sender.RateHz,
		StaleTimeout: time.Duration(cfg.Sender.StaleMs) * time.Millisecond,
	}, store, cfg.Serial.Port, cfg.Serial.Baud, *resetPulse)

	senderDone := make(chan struct{})
	go func() {
		snd.Run(ctx)
		close(senderDone)
	}()

	reader := &input.Reader{}
	g := ui.New(&cfg, *cfgPath, store, snd, reader)

	ebiten.SetWindowSize(1280, 800)
	ebiten.SetWindowTitle("dronectrl — Steam Deck → ELRS")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetRunnableOnUnfocused(true) // keep transmitting if the window loses focus
	if *fullscreen {
		ebiten.SetFullscreen(true)
	}

	runErr := ebiten.RunGame(g)

	// Window closed (or Ctrl+C): stop the sender, which transmits a failsafe burst.
	stop()
	select {
	case <-senderDone:
	case <-time.After(500 * time.Millisecond):
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}
