# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project roadmap

Before planning or implementing work, read
[`docs/autonomy-roadmap.md`](docs/autonomy-roadmap.md). It is the source of truth for
the long-term drone-autonomy architecture, current milestone, safety boundaries, and
evidence gates. Do not describe roadmap capabilities as implemented unless the code
and required evidence exist. When work materially advances or changes the plan,
update the roadmap in the same change.

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

**Mapping engine (`internal/channels`).** `Engine.Apply(chans, inputState, armed,
now)` → `[16]uint16` CRSF ticks. Channel types: `analog` (reverse/expo/deadzone/
trim/endpoints; `Mode` = `position` (default, source → absolute value) or `rate` —
integrating/velocity control where the source sets how fast the channel moves and
centering the stick *holds* position, for e.g. a turret. Rate mode integrates only
while `armed` (so stick-as-cursor motion on the setup screens can't drift it),
clamps to the endpoints, uses `SweepSecs` (time for a full Min→Max sweep at full
deflection) as the speed control instead of `Scale`, and an optional
`RecenterSource` button snaps back to center while held), `switch2` (button →
low/high; `PressMode` = toggle / momentary / pulse — pulse runs a 50/50 square wave
at `PulseHz` while held, used for repeated trigger pulls e.g. a nerf-gun servo.
Engine is stateful for rising-edge toggling, the pulse timer, and rate-mode
position), `switch3` (two buttons → low/mid/high), `fixed`, `none`. This package +
`crsf` hold the only unit tests; keep them green when changing mapping math.

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

## Analog video feed (`internal/video`)

The Skydroid 5.8 GHz OTG receiver is a **UVC** device → on Linux a **V4L2** node
(`/dev/video*`). `internal/video` captures it and feeds the **Run** screen. The capture goroutine decodes each frame (MJPEG via
`image/jpeg`, or YUYV→RGBA) and hands the **latest** frame to the UI through a
single-slot `Buffer` that mirrors the `state.Store` pattern (drop frames, never
block). The UI uploads it to an `*ebiten.Image` (`WritePixels`) once per UI frame and
letterboxes it. All capture/decode lives on the UI/Draw path — **the CRSF sender
goroutine is never touched.** `config.Video` ({enabled, device}) drives a device
picker + on/off toggle on the Settings screen; `Game.applyVideo` (much like
`applyPort`) owns the capture lifecycle.

A Betaflight-style **OSD** (`internal/ui/run.go` `drawOSD`) overlays the feed:
center crosshair, arm-state text, vehicle name, and a debug readout
(resolution/format/fps via `Capture.Info()` + UI-measured fps). Each element is an
independently toggleable `config.OSD` field, configured in the Settings tab's right
column. The vehicle name is the UI's one text field — `Game.editName` focuses it,
`updateNameEdit` captures keystrokes (`ebiten.AppendInputChars` + Backspace,
Enter/Esc to commit); on the bare Deck use a keyboard / the KDE on-screen keyboard,
or set `osd.vehicle_name` in the YAML. OSD text uses an outline pass
(`drawTextOutlined*`) for legibility over video.

V4L2 is hand-rolled directly on `golang.org/x/sys/unix` (a small MMAP-streaming
loop: QUERYCAP → S_FMT → REQBUFS → mmap → QBUF/DQBUF → STREAMON), Linux-only behind
`//go:build linux`, with a no-op stub (`capture_stub.go`) so Windows dev still
builds. **Don't replace it with `github.com/vladimirvivien/go4vl`:** go4vl is cgo
*and* references V4L2 constants newer than the Deck build toolchain's kernel uABI
headers (Ubuntu 22.04 / linux-libc-dev 5.15, kept old for glibc ≤ 2.37), so it won't
compile there — and being cgo, it can't be cross-checked from the Windows box. The
hand-rolled path is pure Go: `CGO_ENABLED=0 GOOS=linux go build ./internal/video/`
type-checks it from Windows, and `internal/video`'s tests assert the V4L2 struct
sizes (104/208/20/88) so an ABI layout slip fails a test instead of the Deck.

## Person detection (`internal/detect`) — Phase 1 of target-lock/auto-aim

Run-screen person detection for the water-pistol turret. A `Runner` goroutine pulls
the latest `video.Frame` from the video `Buffer` at a capped rate, runs a YOLOv8-family
person detector (default export is **yolo11n** — drop-in for yolov8n: same output
layout, faster + more accurate on CPU), tracks targets across frames (see the kalman
tracker below), and publishes `[]Track` through a single-slot `TrackBuffer` (drop,
never block) — fully decoupled from the UI and the transmit loop. The UI overlays boxes
on the Run feed, mapping frame px → screen px via the rect `drawImageFit` now returns.

**Tracking (`kalman.go`).** The sole tracker is a constant-velocity (fixed-gain
alpha-beta) filter: each track carries a filtered center/size + velocity, so it matches
detections against the *predicted* box (holds ID through a fast/panned zero-IoU jump)
and **coasts forward** along its velocity through a missed frame instead of freezing.
Gains are **per-axis** — a turret target moves almost entirely horizontally, so it
learns + coasts horizontal velocity far longer (`predDecayX` high) than vertical
(`predDecayY`/`velocityGainY` low), since vertical box motion is mostly artifact (subject
clipped at the frame edge when close, or the scene sliding under tilt). A
prediction-trust cap (velocity decays each missed frame, track dropped after
`MaxMissed`) stops a lost target extrapolating off to nothing. Association is
**ByteTrack** two-stage: the detector emits a low tier down to `conf_low`; stage 1
matches tracks to high-conf (`conf`) dets, stage 2 re-anchors leftover tracks to
low-conf dets (low dets never spawn, so weak boxes continue a lock but can't create a
phantom). The earlier greedy IoU tracker (+ its A/B toggle) was retired once this was
validated on the Deck.

Inference is **ONNX Runtime via `github.com/yalue/onnxruntime_go`**, which **dlopens**
`libonnxruntime.so` at runtime, so the build needs only cgo (no ORT headers/libs at
compile time). It's **Linux-only** (`detector_onnx.go`, `//go:build linux`) with a
no-op stub (`detector_stub.go`) so the Windows dev build stays cgo-free — same split
as `internal/video`. Tracking/NMS/IoU and the YOLO output decode are pure Go and
unit-tested (the decode test feeds a synthetic tensor, so it needs no model/lib).

Best-effort: if `detect.enabled` is off, or the model / `libonnxruntime.so` is
missing, detection just stays off (status shows why) — like video. `config.Detect`
= {enabled, model_path, lib_path, input_size (fallback only — see below), conf (0.4),
rate_hz (10)}. `Game.applyDetect` owns the runner lifecycle and is re-run by `applyVideo`
so a device change rebinds to the new Buffer. The **Settings** screen has a **model
picker** (lists `*.onnx` files beside the binary via `detectModelFiles`, switches
`model_path` + rebuilds) and a live **rate stepper** (`Runner.SetRate`, no model reload —
the run loop resets its ticker). Trade resolution for Hz here: a smaller export = faster
= less tracking lag, at the cost of small/far-target detection (fine for a short-range
water pistol). `input_size` is now **auto-read from the model's input tensor** in
`NewDetector`, so swapping a 320 ↔ 640 export needs no config edit (`config.input_size`
is only a fallback for dynamic-axis exports).

**Deck runtime setup (the binary doesn't bundle these):** drop a YOLOv8 person/COCO
ONNX (`yolo export model=yolov8n.pt format=onnx`, or a prebuilt `yolov8n.onnx`) and
ONNX Runtime's `libonnxruntime.so` (from the onnxruntime linux-x64 release) onto the
Deck, and point `detect.model_path` / `detect.lib_path` (absolute) at them. Model
must be a YOLOv8-family export (person = COCO class 0 → output channel index 4); its
input size is read from the model, so any square export (320/416/640) works as-is — drop
several beside the binary and pick one in Settings. **Don't** assume detection works
without these two files present. `detector_onnx.go` auto-sniffs whether box outputs are pixel
(standard Ultralytics export) or normalized 0..1, and `resolveModelPath`/
`resolveLibPath` look next to the executable so files can just sit beside `elrsctrl`.

**Target lock + auto-aim (`internal/ui/autoaim.go`).** While armed, `updateTargeting`
lets the operator pick a person: the **D-pad** cycles left/right through visible
tracks and `config.AutoAim.LockSource` (default `r3`) autolocks the nearest / releases.
`updateAutoAim` then drives the configured pan/tilt channels (`AutoAim.PanChannel`/
`TiltChannel`, 1-based; 0 = off) to hold the locked target's box-center on the
crosshair, with its aim point the calibrated crosshair (`aimPointFrame` inverts the
letterbox; reuses `OSD.Crosshair{X,Y}`). The control law (`stepAim`) is a **PD** velocity
command: proportional `gain·err` slews toward the target, derivative `damp·errRate`
(filtered apparent target velocity) brakes to kill overshoot. It's a delayed ~10 Hz
visual-servo loop, so it **advances once per fresh tracker output** (`tracksSeq` change),
not every UI frame — stepping every frame would integrate a stale error / differentiate
noise, and a hot gain limit-cycles (the original pure integrator at gain 4 oscillated).
It advances **while coasting (`Missed > 0`) too**, driving onto the kalman tracker's
*predicted* box so a brief occlusion keeps tracking instead of freezing mid-slew; the
tracker's velocity decay caps how far that rides. The aim point in
the box is `boxAimPoint(box, AimHeight)` — horizontal center, `AimHeight` down from the
top (default 0.25 = upper torso, so a weak/lobbing jet doesn't fall to the feet) — and
it's **EMA-smoothed** before the controller sees it, so the detector's box jitter is
filtered out and a *tight* `Deadband` can give precise aim without re-hunting on noise.
A small marker on the locked box (`drawDetections`) shows that aim point. Defaults are
gentle (`PanGain`/`TiltGain` 2, `Deadband` 0.05, `Damp` 0.5, `AimHeight` 0.25); all five
are live-tunable on the Mapping screen when the pan/tilt channel is selected
(`drawAimTuning`). `normalize()` one-time-upgrades the superseded hot defaults (gain 4 /
deadband 0.03) so an old saved profile self-heals.
It **overrides channel values in `Update` after `engine.Apply`, before the snapshot**,
and only while armed + locked + target-visible — so the sender's failsafe/disarm
always wins and only the turret channels ever move. Unset pan/tilt = lock is
visual-only (boxes + highlight, no motion). The coordinate/control math is pure Go
and unit-tested in `autoaim_test.go`. Back paddles (L4/L5/R4/R5) are intentionally
avoided — Desktop-mode Steam hides them behind raw hidraw; the D-pad needs none of that.

**TEST AIM button (Run screen).** Under ARM/KILL when a pan/tilt channel is assigned:
tapping it runs `updateTurretTest` — a 4-second sweep driving pan/tilt to
up→right→down→left (each held ~1 s at 0.8 of range), overriding only those channels
exactly like auto-aim (so failsafe/disarm still win). The OSD names the current
direction; tap TESTING… to abort. It honors the same invert flags auto-aim uses
(`turretTestPos`/`aimDeflect` mirror the integrator's steady state), so if the turret
moves the *wrong* way under test, auto-aim would chase a target the wrong way too — the
fix is to flip that axis's invert in Mapping. The test needs the TX armed (disarmed =
failsafe), but because the gamepad cursor is hidden while armed (so you couldn't tap to
start it), it **self-arms** when started disarmed and disarms again when it ends
(`turretTest.armedByTest`); a manual pre-test arm is left armed. `applyArmKill` forces
arm while the test is active so it works in hold-to-arm mode too, but every disarm path
(Kill button, panic chord, gamepad disconnect, manual disarm) calls `cancelTurretTest`
first, so a kill always wins. Note self-arm makes *all* channels live for ~4 s, not just
pan/tilt — keep the sticks/trigger neutral during the test.

**Diagnosing lock loss.** Two debug logs trace the "locks on, moves, then loses the
target" symptom, each toggled in **Settings** (right column — no env typing on the
Deck) or forced on via env: **Detect debug log** / `DETECT_DEBUG` logs per-inference
`dets`/`tracks` (id, center, missed, age) from the detect `Runner` — distinguishing
"detection found nothing" from "tracker dropped the id". **Auto-aim debug log** /
`AUTOAIM_DEBUG` logs the lock lifecycle (acquire/release/LOST) and a throttled per-frame
aim readout (box center, aim point, `err`, pan/tilt pos) — watch whether `|err|` trends
to 0 (closing in) or grows (driving the wrong way → invert). The toggles are
`config.Detect.Debug` / `config.AutoAim.Debug`; the detect one pushes live to the
running goroutine via `Runner.SetDebug` (atomic, no ONNX-session rebuild), the env var
ORs on top of either. Logs tee to **both stderr and a file** (`main.setupLog` →
`io.MultiWriter`): `elrsctrl.log` next to the binary by default (truncated each launch),
overridable with `--log <path>` or disabled with `--log off`. The resolved path is
passed into `ui.New` and shown on the Settings screen (a hint line + a status line when
a debug toggle is switched on), so on the Deck you just copy the file off over the
network — no terminal needed. Lines carry a `log.Ltime` timestamp prefix only.

## Inbound telemetry (`internal/crsf` parser + `internal/sender` reader)

ELRS is bidirectional: the module relays CRSF telemetry **back** over the same
handset UART. The sender spawns one **reader goroutine per port open**
(`readTelemetry`, bound to that `*serial.Port`; it ends when the port is
closed/reopened and Read errors). It's a pure side channel on the full-duplex
port — **the transmit loop (`tick`/`writeFrame`) is never touched or blocked.**
`crsf.Parser` resyncs on the CRC (`Push`), and `LINK_STATISTICS` (link quality /
RSSI / TX power — the "robust connection" signal) + `BATTERY_SENSOR` decode into
`state.Telemetry` (the reader is the sole writer, so it read-modify-writes). The
Run-screen OSD shows a link/`NO LINK`/`NO TELEMETRY` indicator + battery (toggle
`osd.telemetry`); the Monitor `Telemetry rx:` counter (`store.TelemetryFrames`)
proves frames are arriving at all. Telemetry only flows with a **bound, powered RX**
and the module relaying CRSF telemetry over the handset UART — if `rx` stays 0,
that path isn't sending, independent of the app.
