# ELRSctrl

Turn a **Steam Deck** (or any gamepad) into an RC transmitter for an **EMAX Aeris
Link / ExpressLRS (ELRS) TX module**. It reads the Deck's controls, lets you remap
any input to any of 16 RC channels, and streams **CRSF** frames to the module over
a serial port — exactly like an EdgeTX handset drives an external module.

It started as an RC-car controller and grew into a small FPV ground-vehicle cockpit:
it can show a live **analog video feed** with a Betaflight-style **OSD**, read **ELRS
telemetry** back from the module, and run an on-device **neural net that detects
people and auto-aims a pan/tilt turret** at a locked target. All of that is optional
— at its core it's still just a gamepad-to-CRSF transmitter.

```
Steam Deck gamepad ─▶ mapping engine ─▶ CRSF frames ─▶ [serial] ─▶ ELRS TX module ──air──▶ RX ─▶ vehicle
analog camera ──▶ 5.8G VTX ──air──▶ 5.8G RX ─▶ [USB UVC] ─▶ Steam Deck ─▶ Run-screen video + OSD + person detection
```

> ⚠️ **Safety first.** This commands a real vehicle (and can swing a turret on its own).
> - The app boots **DISARMED** and transmits **failsafe** values until you arm it.
> - Arm/Kill are bound to gamepad buttons (default **Menu** = arm, **View** = kill)
>   and are also buttons on the **Run** screen.
> - **Panic kill:** hold **LB+RB together for 500 ms** to force-disarm, regardless
>   of mapping or platform. Always works — even if Steam Input is eating your
>   configured Kill button (the gamepad cursor is hidden while armed, so this
>   chord is your guaranteed escape).
> - It auto-disarms if the gamepad disconnects or the UI stalls, and sends a
>   failsafe burst on exit.
> - **Auto-aim** only ever overrides the pan/tilt channels, only while **armed and
>   locked**, and sits upstream of the failsafe — disarm, Kill, the panic chord,
>   input loss, and a stale UI all still force failsafe and win over it.
> - **Test with the wheels off the ground / motor disconnected first.** Set a
>   sensible throttle **failsafe** (neutral for a bidirectional ESC).

---

## 1. Hardware

### ELRS control link (required)

You need to get **CRSF serial data + power** into the module:

- **Power:** the module's **XT30** input (2S / ~8.4 V recommended). USB only powers
  the MCU — the RF stage needs the battery to actually transmit.
- **Data (pick one):**
  - **USB-C → CRSF (verified on the EMAX Aeris Link):** the module's USB is a
    **CP2102** bridge wired to the main MCU's **UART0** (GPIO 1/3). ELRS ships with
    CRSF on the **JR-bay pin** instead, which the USB bridge can't see — so the pin
    remap below is **mandatory** for the USB path, not optional tuning: it's what puts
    CRSF on the USB port at all. In the module's web UI (join its WiFi AP, then
    `http://10.0.0.1`): set **CRSF Serial RX = 3, TX = 1** (`/hardware.html`), turn
    **UART inverted OFF** (Options tab), and keep the **Backpack disabled**. The port
    shows up as `COMx` (Windows) or `/dev/ttyUSB0` (Linux, cp210x driver — *not*
    `ttyACM0`). Then run elrsctrl at **115200** baud.

    ![Aeris Link web UI: CRSF Serial Pins set to RX=3, TX=1](docs/pin_changes.png)

    *In `http://10.0.0.1/hardware.html` → CRSF Serial Pins: **RX pin = 3**, **TX pin = 1**
    (circled). The yellow "hardware configuration has been customised" banner is
    expected after the change.*

  - **FTDI → JR-bay CRSF pin (fallback, not tested here):** wire a **3.3 V** USB-serial
    adapter's TX to the module's CRSF pin (JR bay, half-duplex). With UART-inverted
    **off** a plain non-inverted adapter works; with it on you'd need an inverting one.

> **⚠️ Cabling — a direct USB-C↔USB-C link to the Deck does *not* work.** The Aeris
> Link's Type-C socket is wired as a plain USB-2 device with **no USB-C CC resistors**,
> so a USB-C *host* (the Deck's only port) never detects it or switches on VBUS — the
> module stays dark and never enumerates (the tell: it won't even power up over the
> cable). A **USB-A host always supplies power + data**, which is why a plain
> **USB-A→C cable from a PC just works**. On the Deck, give it a USB-A path: a
> **powered USB-C hub/dock** with the module in one of its **USB-A** ports (via the
> USB-A→C cable), or a **USB-C→USB-A-female OTG adapter**. The **XT30 battery powers
> the RF stage but does *not* fix this** — it's a USB data/detection problem, not a
> power-budget one.

![Steam Deck driving the Aeris Link through a USB-A hub](docs/steamdeck_usb2_setup.png)

*Working setup on the Deck: a USB-C hub on the Deck's port, module plugged into one
of the hub's USB-A ports via a USB-A→C cable, XT30 battery powering the RF stage.*

> **⚙️ Verified EMAX Aeris Link config (USB-C):** module web UI → CRSF serial
> **pins 3/1**, **UART inverted = off**, backpack off; elrsctrl **`--baud 115200`**,
> address `0xEE`. These are specific to *this* module's CP2102-on-UART0 design.
> **Other ELRS TX modules will differ** — USB-serial wiring, CRSF pins, inversion,
> and sometimes the baud — so treat this as a worked example, not universal settings,
> and always confirm with **sweep mode** before trusting the link.

The app is agnostic to the wiring — it opens a serial port at the configured **baud**
and streams CRSF. Note **115200** is the Aeris Link's handset-serial rate; the
oft-quoted **420000** is ELRS's *over-air* rate, **not** the serial baud.

### FPV video (optional — for the Run-screen feed, OSD & detection)

- An **analog FPV camera + 5.8 GHz VTX** on the vehicle, and a **5.8 GHz UVC receiver**
  on the Deck. Verified with the **Skydroid 5.8G OTG receiver**, which enumerates as a
  standard **USB Video Class** camera (640×480) → a `/dev/video*` node on Linux.
- Connect the receiver through the **same USB-A host path** as the module (powered
  hub/OTG adapter) — the Deck's bare USB-C port won't drive a USB-A device directly.
  On Linux the capture node is `/dev/video0` (the dongle exposes several `/dev/videoN`;
  only one is the capture node — the app rejects the wrong ones with a clear message).

### Pan/tilt turret (optional — for auto-aim)

- Two servos (pan + tilt) on receiver outputs, mapped to two RC channels (typically
  in **rate** mode with a **recenter** button). Auto-aim drives those two channels;
  see [§6](#6-person-detection--auto-aim). Mounts for a camera/VTX box and an X-Shot
  water pistol live in [`3dprinting/`](3dprinting).

---

## 2. Build

Install Go (1.22+): <https://go.dev/dl/>. Then, from the repo root:

```sh
go mod tidy     # resolves deps and writes go.sum
go test ./...   # CRSF, mapping, detection and auto-aim unit tests
go run ./cmd/elrsctrl   # opens the UI on your dev machine (plug in any gamepad)
```

On **Windows** Ebiten needs no C compiler, and person detection compiles to a no-op
stub, so everything builds cgo-free. On Linux/macOS dev machines Ebiten needs the
usual GL/X11/ALSA dev headers (see Ebiten's install docs).

### Build for the Steam Deck

The Deck is `linux/amd64` and Ebiten needs cgo **plus** the X11/OpenGL/ALSA *dev
headers* there. A Windows-hosted cross-compiler (e.g. `zig cc`) ships libc but not
those GUI headers, so it can't build Ebiten — build natively on Linux instead.

**From Windows via WSL2 (recommended):** one-time, inside `wsl`:

```sh
sudo apt-get update
sudo apt-get install -y gcc pkg-config xorg-dev libgl1-mesa-dev libasound2-dev
# + Go on PATH (https://go.dev/dl/); it auto-fetches the version in go.mod.
```

Then from the repo root in PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-linux.ps1   # -> dist\elrsctrl
```

The `.ps1` just runs `scripts/build-linux.sh` inside WSL. That same script also
builds **on the Deck** in a distrobox/toolbox container (same packages,
`bash scripts/build-linux.sh`).

> The build's glibc must be **≤ the Deck's** (SteamOS 3.x ≈ glibc 2.37) or it won't
> start. Building on an older distro (Ubuntu 22.04 = 2.35) is the safe side; the
> script prints the highest glibc the binary needs so you can check.

Person detection links the ONNX Runtime **dynamically at runtime** (it's `dlopen`ed),
so the build needs no ONNX libraries or headers — just the cgo toolchain above. The
runtime files are copied to the Deck separately (see [§6](#6-person-detection--auto-aim)).

---

## 3. Deploy & run on the Steam Deck

> **Cabling reminder:** the Deck's single USB-C port **can't drive USB devices
> directly** — connect the module (and the video receiver) through a powered hub's
> **USB-A** ports (see [§1](#1-hardware)). The right serial node is `/dev/ttyUSB0`
> (cp210x); the Deck's own controller is `/dev/ttyACM0`, a trap that opens but
> swallows every frame.

1. Copy **`dist/elrsctrl`** and a `config.yaml` (start from `config.example.yaml`) to
   `~/elrsctrl/` on the Deck and `chmod +x elrsctrl`. For person detection, also drop
   `libonnxruntime.so` and your model **in the same folder** (see [§6](#6-person-detection--auto-aim)).
2. **Run from Desktop Mode first.** This gives the app clean, raw access to the
   built-in controller. (In Game Mode, Steam Input intercepts non-Steam games and
   only forwards an emulated gamepad — workable for standard sticks/buttons, but
   Desktop Mode is the reliable path, and it's also where you can type the OSD vehicle
   name with a keyboard.) Launch from **Konsole** (`./elrsctrl --config config.yaml`)
   so any error prints in full.
3. Optional: add it as a **non-Steam game** to launch from Game Mode. If the
   controller doesn't respond, set that app's controller layout to a plain
   **Gamepad** template / disable Steam Input for it.
4. Runtime libraries (libGL, libX11/Xrandr/Xcursor/Xinerama/Xi, libasound2) ship
   with SteamOS; it runs under Game Mode via XWayland.

---

## 4. Using it

### Hardware bring-up (do this once, before driving)

Prove the serial link moves a servo/ESC on a bound receiver — no UI, no mapping:

```sh
./elrsctrl --port /dev/ttyUSB0 --sweep 1      # Aeris Link over USB (cp210x); sweeps CH1; Ctrl+C to stop
# Windows dev:  .\elrsctrl.exe --port COM4 --sweep 1
```

If the bound servo sweeps, your wiring/baud/address/CRSF pins are correct. If not,
re-check the verified config above (pins 3/1, inverted off, baud 115200), try another
baud from ELRS's autobaud list (400000/921600), or fall back to the FTDI→JR path.

### Normal use

```sh
./elrsctrl --config config.yaml          # or --fullscreen on the Deck
```

Four touch/mouse screens — switch with the tabs, or (handy on the Deck) with **LB / RB
while disarmed** (when armed, the bumpers stay free for your channel mappings):

- **Run** — the drive/fly screen. The live video feed fills the view with the OSD and
  bounding boxes drawn over it, plus **ARM/DISARM** and **KILL** buttons (top-right).
  The gamepad picks and locks targets here (see [§6](#6-person-detection--auto-aim)).
  With no video device it just shows a placeholder.
- **Monitor** — read-only status: serial port + frames/sec, gamepad, an inbound
  **Telemetry rx** counter (+ live link quality), and bars for all 16 channels (µs +
  raw). Shows when failsafe values are being sent. No arm/kill here.
- **Mapping** — pick a channel on the left; on the right set its **Type**
  (none/analog/switch2/switch3/fixed), **Source**, **Mode** (position or **rate** for
  a turret), **Reverse**, **Expo**, **Deadzone**, **Trim**, **endpoints**, switch
  positions, and **Failsafe**. Tap **BIND** then move a stick / press a button to
  assign it instantly.
- **Settings** — serial **Port** (+ Rescan), **Baud**, **Rate**, **CRSF Address**,
  **Gamepad** picker, **Arm/Kill** bindings and arm mode; and a right-hand **OSD**
  column (vehicle name, crosshair + X/Y offset, arm state, name, video info, telemetry,
  detection) plus the **Video** device picker/toggle. **Save / Reload / Reset** at the
  bottom.

### Default RC-car profile

| Channel | Function | Source |
|---|---|---|
| CH1 | Steering | Left Stick X |
| CH2 | Throttle | Triggers (R2 forward / L2 reverse), neutral failsafe |
| CH3 | Aux1 | A (toggle) |
| CH4 | Aux2 | B (toggle) |

Arm = **Menu**, Kill = **View**. Everything is remappable.

---

## 5. FPV video, OSD & telemetry

**Video.** Plug in the UVC receiver (via the USB-A hub), open **Settings → Video**,
pick the device (`/dev/video0`) and toggle the feed on. The **Run** tab then shows it
letterboxed. Capture and decode run on their own goroutine, fully decoupled from the
transmit loop. If a `/dev/videoN` errors, try the next one — only one node is the real
capture device.

**OSD.** Toggle each element in the **Settings → OSD** column. Elements:
- **Crosshair** — a reticle at the aim point. Use **Crosshair X / Y** (±5 px per click)
  to nudge it onto wherever your payload actually hits — this calibration is also the
  point auto-aim drives targets toward.
- **Arm state** — `ARMED`/`DISARMED`, plus `FAILSAFE` when safe values are going out.
- **Vehicle name** — bottom-center. Type it in the Settings text field with a keyboard
  (or the KDE on-screen keyboard in Desktop Mode), or set `osd.vehicle_name` in the YAML.
- **Video info (debug)** — capture resolution / pixel format / fps, handy for sizing up
  headroom or diagnosing a "cut off" frame (analog blanking vs. the real picture).

**Telemetry.** ELRS is bidirectional — the module relays CRSF telemetry back over the
same serial link. With a **bound, powered RX** connected, the OSD shows a link
indicator (`LINK 92% / RSSI / power`, colored by quality, or `NO LINK` / `NO TELEMETRY`)
and battery voltage/current. The **Monitor** screen's `Telemetry rx:` counter proves
frames are arriving at all — if it stays `0`, the module isn't relaying telemetry
(no RX bound/connected, or telemetry not enabled on the module), independent of the app.

---

## 6. Person detection & auto-aim

On-device person detection (YOLOv8 via ONNX Runtime) draws a box on each detected
person; you lock one with the gamepad, and — if a turret is configured — auto-aim
holds it under the crosshair. Linux/Deck only; on Windows detection is a no-op stub.

### Runtime files (not bundled)

Detection needs two files that aren't in the binary, both placed **next to `elrsctrl`**:

1. **`libonnxruntime.so`** — from the [ONNX Runtime releases](https://github.com/microsoft/onnxruntime/releases),
   asset `onnxruntime-linux-x64-<ver>.tgz` (the plain CPU build). Use a version whose
   API matches the Go binding — **1.26.0 is verified**; a newer release should also
   work but may need a newer glibc/libstdc++ than SteamOS has. Copy the real
   `lib/libonnxruntime.so.<ver>` in as `libonnxruntime.so`.
2. **A model** — a YOLOv8 person/COCO ONNX. Export with
   `pip install ultralytics && yolo export model=yolov8n.pt format=onnx`, or use a
   prebuilt `yolov8n.onnx`. Name it `model.onnx` (the default) or point `detect.model_path`
   at it. (`input_size` must match the export — 640 for a stock export, 320 for a
   smaller/faster one.) The decoder auto-handles both pixel- and normalized-coordinate
   exports.

```
~/elrsctrl/
├── elrsctrl
├── libonnxruntime.so   # auto-found next to the binary
├── model.onnx          # auto-found next to the binary
└── config.yaml
```

Then set `detect.enabled: true` (or toggle **Detection** in Settings), enable video,
and open **Run**. Tune speed with `detect.rate_hz` and `detect.input_size`.

### Targeting (gamepad, while armed)

- **D-pad ◀ / ▶** — cycle the lock through detected people (left-to-right).
- **Lock button** (`autoaim.lock_source`, default **R3** / right-stick click) — autolock
  the person nearest the crosshair; press again to release.
- The locked target gets a **bold red box**; the OSD shows `LOCKED #id`.
- Pressing the turret's **recenter button** (a channel's rate-mode `recenter_source`)
  drops the lock too, so "return to forward" also lets go of the target.
- Disarming clears the lock.

### Auto-aim (opt-in)

Auto-aim is off until you tell it which channels drive the turret — until then, target
lock is purely visual (boxes + highlight, no motion). Configure it in `config.yaml`:

```yaml
autoaim:
  pan_channel: 5      # 1-based RC channel wired to the pan servo; 0 = off
  tilt_channel: 6     # tilt servo; 0 = off
  pan_gain: 4.0       # higher = faster turret slew per unit aim error
  tilt_gain: 4.0
  pan_invert: false   # flip if the turret drives away from the target
  tilt_invert: false
  deadband: 0.03      # normalized aim error below which it holds still (anti-jitter)
  lock_source: r3     # autolock/release button
```

While **armed and locked**, it integrates the pan/tilt channels to put the target's
box-center on your calibrated crosshair. **Tuning:** start with one axis (set only
`pan_channel`) to confirm direction — if it drives *away* from the target, flip
`pan_invert`. If it oscillates, lower the gain; if sluggish, raise it; bump `deadband`
if it jitters when centered.

**Safety:** auto-aim only overrides the configured pan/tilt channels, only while armed
+ locked + the target is visible, and it does so **upstream of the sender's failsafe**.
Disarm, Kill, the LB+RB panic chord, gamepad loss and a stale UI all still force
failsafe and override it; it never touches throttle, steering or a trigger channel.

---

## 7. Config reference

See [`config.example.yaml`](config.example.yaml) for the full annotated format —
`serial`, `sender`, `safety`, `video`, `osd`, `detect`, `autoaim`, and the 16 channel
definitions (with source names and tick values). Anything there can also be set live in
the app's Settings/Mapping screens and saved (the OSD/video toggles, bindings, channel
mapping); the `detect`/`autoaim` paths and gains are YAML-only.

---

## 8. CRSF details

Frames are `RC_CHANNELS_PACKED`: `[addr=0xEE][len=0x18][type=0x16][16ch × 11-bit,
LSB-first][CRC8]`, CRC8 = DVB-S2 (poly 0xD5) over `[type+payload]`, at the module's
handset-serial baud (**115200** on the Aeris Link over USB; 420000 is the over-air
rate, *not* the serial baud — ELRS's handset autobaud accepts 400000/115200/921600…).
Ticks map 172→~1000 µs, 992→1500 µs (center), 1811→~2000 µs. Inbound telemetry
(`LINK_STATISTICS`, `BATTERY_SENSOR`) is parsed the same way. Implementation and unit
tests are in [`internal/crsf`](internal/crsf).

---

## 9. Troubleshooting

- **Module won't power up / no serial port over USB-C:** a **direct USB-C↔USB-C** link
  to the Deck won't power or enumerate the Aeris Link (its Type-C socket has no CC
  resistors). Use a **USB-A host path** — a powered hub/dock with the module in a
  **USB-A** port, or a USB-C→USB-A-female OTG adapter. XT30 power alone won't fix it.
- **No port listed:** check the cable/driver; on Linux ensure your user can access
  the device (`sudo usermod -aG dialout,uucp $USER`, then re-login).
- **Port connects but nothing moves:** verify with `--sweep`; confirm the module is
  powered via XT30, the **baud matches the module** (115200 for the Aeris Link over
  USB), the CRSF serial pins/inversion are set (3/1, inverted off on the Aeris Link),
  address is 0xEE, and the RX is bound. Remember `--sweep N` drives CRSF channel *N*,
  so a servo on RX output 4 only moves with `--sweep 4`.
- **Gamepad not detected / double input on the Deck:** run from Desktop Mode, and in
  Settings use the **Gamepad** picker to select the right device.
- **Vehicle twitches when you tab away:** expected — the app sends failsafe when the
  snapshot goes stale or it loses input. Re-arm to resume.
- **No video / wrong `/dev/videoN`:** connect the receiver via the USB-A hub; in
  Settings cycle the **Video device** (UVC dongles expose several nodes — only one is
  the capture node). Toggle **Video info** to confirm a live resolution/fps.
- **Detection: "cannot open shared object file":** `libonnxruntime.so` isn't beside the
  binary (or `lib_path` is wrong). Put it next to `elrsctrl`.
- **Detection: "platform-specific initialization failed … API version":** the ONNX
  Runtime version doesn't match the Go binding — use **1.26.0** (or the version the
  error says it supports). If it instead mentions `GLIBCXX`/`GLIBC … not found`, the
  ORT build is too new for SteamOS; grab an older-toolchain release.
- **Detection: "model.onnx doesn't exist":** name your model `model.onnx` next to the
  binary, or set `detect.model_path`.
- **Boxes are wrong / nothing detected:** confirm it's a YOLOv8-family export at the
  configured `input_size`; toggle **Video info** to check the feed is live.
- **Telemetry rx stays 0:** the module isn't relaying CRSF telemetry — needs a bound,
  powered RX and telemetry enabled on the module; it's upstream of the app.
- **Auto-aim drives the wrong way / hunts:** flip `pan_invert`/`tilt_invert`; lower the
  gain if it oscillates, raise it if sluggish; increase `deadband` for jitter.

---

## 10. 3D-printable parts

Optional STL files for kitting out the vehicle live in [`3dprinting/`](3dprinting):

- [`CameraAndVtxBox.stl`](3dprinting/CameraAndVtxBox.stl) — a box that holds an
  analog FPV camera and a VTX (video transmitter).
- [`Chassis_Connector.stl`](3dprinting/Chassis_Connector.stl) — a connector that
  mounts a custom vehicle cover/body to the **Traxxas Maxx** chassis.
- [`Waterpistole_holder_02.stl`](3dprinting/Waterpistole_holder_02.stl) — a mount
  for the **X-Shot** water pistol, with a spot for a servo (drive it from a
  `switch2` channel in **pulse** mode for repeated trigger pulls).
