// Package version exposes a short build identifier for display in the UI and
// logs, so a given binary can be matched back to the commit it was built from.
//
// The build scripts stamp a rich string (commit count + short hash + dirty
// flag) via -ldflags "-X elrsctrl/internal/version.Version=...". When that
// stamp is absent — e.g. a plain `go build ./...` during development — String
// falls back to the VCS revision Go embeds in the binary automatically, so a
// build still shows the commit it came from instead of a bare "dev".
package version

import "runtime/debug"

// Version is overridden at build time via -ldflags. Leave empty otherwise so
// String can fall back to the embedded VCS info.
var Version = ""

// String returns the short build identifier, e.g. "b22-e1f3fe6" from the build
// scripts, "e1f3fe6-dirty" from a plain go build, or "dev" if no VCS info is
// available (such as `go run` outside a checkout).
func String() string {
	if Version != "" {
		return Version
	}
	return fromBuildInfo()
}

func fromBuildInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if modified {
		rev += "-dirty"
	}
	return rev
}
