module elrsctrl

// Direct deps:
//   - github.com/hajimehoshi/ebiten/v2  window, gamepad, touch, rendering (no cgo on Windows)
//   - go.bug.st/serial                  custom-baud (420000) serial + port enumeration
//   - gopkg.in/yaml.v3                   config profiles
//   - golang.org/x/image/font/...        UI text rendering
//   - golang.org/x/sys/unix              V4L2 analog-video capture ioctls (Linux-only; pure Go, no cgo)
//
// The go directive below is the floor required by these deps (x/image). With a
// newer Go installed, the build just works; older toolchains will auto-fetch it.

go 1.25.0

require (
	github.com/hajimehoshi/ebiten/v2 v2.9.9
	github.com/yalue/onnxruntime_go v1.31.0
	go.bug.st/serial v1.6.2
	golang.org/x/image v0.41.0
	golang.org/x/sys v0.43.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	github.com/ebitengine/gomobile v0.0.0-20250923094054-ea854a63cce1 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/go-text/typesetting v0.3.0 // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
