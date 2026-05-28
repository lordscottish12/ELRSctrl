# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`elrsctrl` turns a Steam Deck (or any gamepad) into an RC transmitter for an
ExpressLRS (ELRS) TX module. It reads gamepad controls, maps them to 16 RC
channels, and streams **CRSF** frames over a serial port to the module — like an
EdgeTX handset driving an external module. Native UI is **Ebitengine**
(immediate-mode, touch + mouse). Single Go binary; primary target is the Steam
Deck (`linux/amd64`), developed on Windows.

## Commands

```sh
go mod tidy                         # after changing imports
go build ./...                      # full build (incl. UI; no C compiler needed on Windows)
go vet ./...
go test ./...                       # unit tests (crsf + channels)
go test -run TestSwitch2Toggle ./internal/channels/   # a single test
go run ./cmd/elrsctrl              # open the UI on a dev machine
go run ./cmd/elrsctrl --port COM5 --sweep 1   # hardware bring-up: no UI, sweep CH1
```

Build for the Steam Deck (linux/amd64) — Ebiten needs cgo **and** the X11/OpenGL/
ALSA dev headers on Linux, which a Windows/Zig cross-compiler can't supply, so we
build natively in **WSL2 (Ubuntu)**. One-time WSL setup: `sudo apt-get install -y
gcc pkg-config xorg-dev libgl1-mesa-dev libasound2-dev` + Go on PATH. Then from the
repo root in PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-linux.ps1   # -> dist/elrsctrl
```

The `.ps1` is a thin wrapper that runs `scripts/build-linux.sh` inside WSL; that
same `.sh` also builds in a distrobox/toolbox on the Deck itself. Build glibc must
be ≤ the Deck's (SteamOS 3.x ≈ 2.37), so prefer an older Ubuntu (22.04 = 2.35).

> **Heads-up (any environment):** don't `go run` the GUI (`./cmd/elrsctrl`) in a
> headless/CI/agent environment — it opens a window and hangs. Verify logic with
> `go test` and the no-window `--sweep` path instead. Personal per-machine setup
> (e.g. a Go toolchain that isn't on PATH) goes in a gitignored `CLAUDE.local.md`.

## Architecture

The control pipeline, left to right:

```
gamepad (Ebiten) → mapping engine → state.Snapshot → sender goroutine → CRSF → serial → ELRS module
```

**Two-goroutine model — keep these decoupled.**
- The **Ebiten loop** (main goroutine) runs `ui.Game.Update` (~60 fps): polls the
  gamepad, evaluates the mapping, and publishes a `state.Snapshot` to the shared
  `state.Store`. `ui.Game.Draw` renders.
- The **sender goroutine** (`internal/sender`) ticks independently at the
  configured rate (default 250 Hz), reads the latest snapshot, builds a CRSF
  frame, and writes it to the serial port. It owns the port lifecycle and
  reconnection. Decoupling transmit rate from UI fps is intentional — **nothing in
  the UI or a future feature should ever block or slow the transmit loop.**

**Immediate-mode UI convention (`internal/ui`).** All pointer interaction happens
in `Draw`, not `Update` — widgets draw and report clicks in the same pass, so the
dynamic layouts (esp. the per-channel editor in `mapping.go`) can't drift between
two passes. `Update` only polls input, resolves bind mode, and publishes the
snapshot. Every version-sensitive Ebiten/`text/v2`/`vector` call is isolated in
`draw.go` (primitives + immediate-mode widgets: `button`, `toggleButton`,
`stepper`, `channelBar`, `drawText*`); add new rendering helpers there, not inline.

**Safety model — defense in depth.** The sender, not the UI, makes the final
decision: it transmits each channel's **failsafe** value (not live values) whenever
`!Armed`, `!InputOK` (gamepad gone), or the snapshot is stale (UI stalled). The app
boots disarmed; arm/kill are gamepad bindings in `config.Safety` (default Menu =
arm toggle, View = kill) and also Monitor-screen buttons. A **hardcoded panic-kill
chord** — LB+RB held ≥ 500 ms while armed — always disarms regardless of binding,
so a stuck/intercepted Kill button (e.g. Steam Input eating Menu/View on the Deck)
isn't a trap: the cursor is hidden while armed, so this chord is the guaranteed
escape. Gamepad disconnect auto-disarms; exit sends a short failsafe burst. When
touching throttle logic, preserve "neutral/off is the failsafe."

**Mapping engine (`internal/channels`).** `Engine.Apply(chans, inputState)` →
`[16]uint16` CRSF ticks. Channel types: `analog` (reverse/expo/deadzone/trim/
endpoints), `switch2` (button → low/high; `PressMode` = toggle / momentary /
pulse — pulse runs a 50/50 square wave at `PulseHz` while held, used for repeated
trigger pulls e.g. a nerf-gun servo. Engine is stateful for rising-edge toggling
and the pulse timer), `switch3` (two buttons → low/mid/high), `fixed`, `none`. This
package + `crsf` hold the only unit tests; keep them green when changing mapping
math.

**CRSF (`internal/crsf`).** RC_CHANNELS_PACKED: `[addr=0xEE][len=0x18][type=0x16]
[16ch × 11-bit, LSB-first][CRC8]`, CRC8 = DVB-S2 (poly 0xD5) over `[type+payload]`.
Serial baud is the **handset-UART** rate set in config — **115200** over the Aeris
Link's CP2102 (420000 is ELRS's *over-air* rate, **not** the serial baud; ELRS's
handset autobaud also accepts 400000/921600). Ticks: 172≈1000µs, 992=1500µs
(center), 1811≈2000µs.

**Config (`internal/config`).** One YAML profile (serial, sender, safety, 16
channels). `LoadOrDefault` returns the built-in RC-car profile on a missing file;
`normalize()` backfills any missing fields after load — extend it when adding
config fields so older files keep loading.

**Input sources (`internal/input`).** `Source` is the closed set of mappable
controls; `AllSources`/`AnalogSources`/`ButtonSources` drive the UI steppers and
bind mode. Add new controls here (label + analog/button/bipolar classification) and
they flow through automatically.

## Hardware & platform notes

- The app just opens a serial port and streams CRSF — agnostic to **how** CRSF
  reaches the module. **Verified path (EMAX Aeris Link, over USB-C):** the module's
  USB is a CP2102 on the main MCU's UART0, so in its web UI (`http://10.0.0.1`) set
  CRSF serial pins **RX=3/TX=1**, **UART inverted OFF**, backpack disabled, and run
  at **115200** baud. Opening that port auto-resets the MCU, so `serial.Open` pulses
  a clean EN reset (gated by `--reset-pulse`, default on). The FTDI→JR-bay CRSF pin
  is an untested fallback for modules that don't expose CRSF on USB (no code change
  needed). Module is powered via XT30 (2S). On Linux/the Deck the node is
  `/dev/ttyUSB0` (cp210x); `/dev/ttyACM0` is the Deck's own Steam Controller — a trap
  that opens cleanly but swallows every write. The Deck also can't drive the module
  over a **direct USB-C↔USB-C** cable (the module's Type-C socket has no CC resistors,
  so a USB-C host won't power or enumerate it) — it needs a **USB-A host path**
  (powered hub / OTG adapter).
- On the Steam Deck, run from **Desktop Mode** for clean raw gamepad access (Game
  Mode + Steam Input intercepts non-Steam games). The Settings screen has a gamepad
  picker for when Steam's virtual pad also appears.

## Roadmap: analog video feed (planned, not yet implemented)

Goal: with an analog FPV video receiver + USB capture dongle connected to the
Deck's USB-C, show the live feed in the app (e.g., as the Monitor background with
channel bars / arm state as a HUD overlay).

Suggested approach when implementing:
- The capture dongle presents as a **UVC** device → on Linux a **V4L2** node
  (`/dev/video*`). Capture with a V4L2 library (e.g. `github.com/vladimirvivien/go4vl`),
  Linux-only behind a build tag (`//go:build linux`), with a no-op stub elsewhere so
  Windows dev still builds.
- New `internal/video` package exposing a `Capture` interface and a goroutine that
  decodes frames (MJPEG via `image/jpeg`, or YUYV→RGBA) and hands the **latest**
  frame to the UI through a mutex-guarded buffer — mirror the `state.Store` pattern
  (drop frames, never block).
- In the UI, upload the frame to an `*ebiten.Image` (`WritePixels`) once per UI
  frame and draw it before the channel bars. Keep all V4L2/decoding off the transmit
  path — **the CRSF sender goroutine must remain untouched and unblocked.**
- Add device selection (`/dev/videoN`) and an on/off toggle to the Settings screen.
