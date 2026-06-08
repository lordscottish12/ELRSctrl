#!/usr/bin/env bash
# Build elrsctrl for the Steam Deck (linux/amd64) on a NATIVE Linux system.
#
# Use this from WSL2 (Ubuntu) on a Windows dev machine, or inside a
# distrobox/toolbox container on the Deck itself. Ebiten needs cgo plus the
# X11 / OpenGL / ALSA *dev headers* at compile time, which a Windows/Zig cross
# compiler can't supply -- so we build natively where those headers exist.
#
# One-time setup (Debian/Ubuntu, incl. WSL2 Ubuntu):
#   sudo apt-get update
#   sudo apt-get install -y gcc pkg-config xorg-dev libgl1-mesa-dev libasound2-dev
#   # xorg-dev pulls in libx11/xcursor/xrandr/xinerama/xi/xxf86vm dev packages.
#   # Plus Go on PATH (https://go.dev/dl/); the toolchain auto-upgrades to the
#   # version pinned in go.mod, so any recent Go (1.21+) works.
#
# Invoke via `bash build-linux.sh` (the Windows wrapper does this), so the file's
# execute bit / line endings on a /mnt drive don't matter.

set -euo pipefail

# Repo root = parent of this script's directory.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"

# `wsl bash script.sh` (how the Windows wrapper launches us) is a non-login shell,
# so it doesn't source ~/.profile -- a tarball Go install at /usr/local/go/bin
# would be off PATH. Add the standard Go locations here, idempotently, so the
# build works no matter how it's launched.
for godir in /usr/local/go/bin "$HOME/go/bin"; do
  if [ -d "$godir" ]; then
    case ":$PATH:" in
      *":$godir:"*) ;;
      *) PATH="$godir:$PATH" ;;
    esac
  fi
done
export PATH

# --- preflight: toolchain + Ebiten's compile-time headers --------------------
missing=0

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go not found on PATH." >&2
  echo "  Install Go 1.21+ (it auto-fetches the version in go.mod): https://go.dev/dl/" >&2
  echo "  e.g.  wget -qO- https://go.dev/dl/go1.25.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xz" >&2
  echo "        export PATH=\$PATH:/usr/local/go/bin   # add to ~/.profile to persist" >&2
  missing=1
fi

if ! command -v gcc >/dev/null 2>&1; then
  echo "ERROR: gcc not found -- cgo needs a C compiler." >&2
  missing=1
fi

# Probe the X11/OpenGL/ALSA dev packages via pkg-config (all three at once).
if command -v pkg-config >/dev/null 2>&1; then
  if ! pkg-config --exists x11 gl alsa; then
    echo "ERROR: missing Ebiten build deps (X11 / OpenGL / ALSA dev headers)." >&2
    missing=1
  fi
else
  echo "ERROR: pkg-config not found." >&2
  missing=1
fi

if [ "$missing" -ne 0 ]; then
  echo "" >&2
  echo "Install everything with:" >&2
  echo "  sudo apt-get update && sudo apt-get install -y gcc pkg-config xorg-dev libgl1-mesa-dev libasound2-dev" >&2
  exit 1
fi

# --- build -------------------------------------------------------------------
# Stamp a build identifier from git: bN-<shorthash>[-dirty.<diffhash>] (N = commit
# count), baked in via -ldflags so the binary's version string (shown bottom-right in
# the UI and at startup) matches the commit it came from. The runtime has its own VCS
# fallback (internal/version), so this only enriches the string with the count.
# For a dirty tree we append a short hash of the uncommitted diff, so two builds off the
# same commit but different working changes get distinct ids (and rebuilding identical
# code reproduces the same id) — otherwise every dirty build looks the same.
HASH="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
COUNT="$(git rev-list --count HEAD 2>/dev/null || echo 0)"
DIRTY=""
if ! git diff --quiet HEAD 2>/dev/null; then
  DIFFHASH="$(git diff HEAD 2>/dev/null | sha1sum | cut -c1-6)"
  DIRTY="-dirty.${DIFFHASH}"
fi
VERSION="b${COUNT}-${HASH}${DIRTY}"

mkdir -p dist
echo "Building dist/elrsctrl $VERSION for Steam Deck (native linux/amd64)..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-X elrsctrl/internal/version.Version=$VERSION" \
  -o dist/elrsctrl ./cmd/elrsctrl

echo "Done -> dist/elrsctrl"

# Report the highest glibc symbol the binary needs: it must be <= the Deck's
# glibc or it won't start (SteamOS 3.x is ~glibc 2.37). Building on an older
# distro (e.g. Ubuntu 22.04 = 2.35) is the safe side.
if command -v objdump >/dev/null 2>&1; then
  maxglibc="$(objdump -T dist/elrsctrl 2>/dev/null \
    | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)+' | sort -V | tail -n1 || true)"
  if [ -n "$maxglibc" ]; then
    echo "Requires up to $maxglibc -- must be <= the Deck's glibc (SteamOS 3.x ~= 2.37)."
  fi
fi
