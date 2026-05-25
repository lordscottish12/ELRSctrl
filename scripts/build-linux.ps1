# Build dronectrl for the Steam Deck (linux/amd64) from Windows, via WSL2.
#
# The Deck is linux/amd64 and Ebiten needs cgo + the X11/OpenGL/ALSA dev headers
# there. A Windows-hosted cross compiler (e.g. `zig cc`) ships libc but NOT those
# GUI system headers, so it can't build Ebiten. We instead build natively inside
# WSL2 (Ubuntu), which has them, and drop the result in dist\ on the Windows side.
#
# One-time setup, if you don't already have an Ubuntu WSL distro:
#   wsl --install -d Ubuntu-22.04          # 22.04 (glibc 2.35) is a safe Deck target
# Then inside that distro:
#   sudo apt-get update
#   sudo apt-get install -y gcc pkg-config xorg-dev libgl1-mesa-dev libasound2-dev
#   # plus Go on PATH (https://go.dev/dl/); it auto-fetches the version in go.mod.
#
# Then, from the repo root in Windows PowerShell:
#   powershell -ExecutionPolicy Bypass -File scripts\build-linux.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\build-linux.ps1 -Distro Ubuntu-22.04
#
# Alternative (no WSL): build on the Deck itself in a distrobox/toolbox container
# with the same packages and run scripts/build-linux.sh there.

param(
    [string]$Distro = ""   # WSL distro to build in; auto-detected if omitted.
)

$ErrorActionPreference = "Stop"
$env:WSL_UTF8 = "1"        # make `wsl -l` emit UTF-8, not UTF-16, so parsing works.

if (-not (Get-Command wsl -ErrorAction SilentlyContinue)) {
    Write-Error "wsl not found. Install WSL2 + Ubuntu (https://aka.ms/wsl): wsl --install -d Ubuntu-22.04"
}

# Enumerate installed distros, excluding Docker Desktop's internal backends, which
# have no apt/Go/gcc and cannot build this. @(...) forces an array so a single
# match stays a list -- otherwise $distros[0] would index into a scalar string and
# yield its first *character*.
$distros = @((& wsl -l -q) | ForEach-Object { $_.Trim() } |
    Where-Object { $_ -ne "" -and $_ -notmatch '^docker-desktop' })

if ($Distro -ne "") {
    if ($distros -notcontains $Distro) {
        Write-Error "Distro '$Distro' not found. Installed (build-capable): $($distros -join ', ')"
    }
} elseif ($distros.Count -ge 1) {
    $Distro = $distros[0]
} else {
    Write-Error @"
No build-capable WSL distro found (only Docker Desktop's backend is installed).
Install Ubuntu, then re-run:
  wsl --install -d Ubuntu-22.04
  # then inside it: sudo apt-get install -y gcc pkg-config xorg-dev libgl1-mesa-dev libasound2-dev
  #                 and put Go on PATH (https://go.dev/dl/)
"@
}

# Repo root = parent of this script's folder; translate to a WSL path.
$root = Split-Path -Parent $PSScriptRoot
$wslRoot = (& wsl -d $Distro wslpath ($root -replace '\\', '/')).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($wslRoot)) {
    Write-Error "Could not translate '$root' to a WSL path (is the repo on a drive WSL can see?)."
}

Write-Output "Building in WSL distro '$Distro' at $wslRoot ..."
& wsl -d $Distro bash "$wslRoot/scripts/build-linux.sh"
if ($LASTEXITCODE -ne 0) {
    # Note: $ErrorActionPreference='Stop' does NOT trip on a native command's exit
    # code, so we check $LASTEXITCODE ourselves -- otherwise a failed build would
    # silently look like success (the original bug here).
    Write-Error "WSL build failed (exit $LASTEXITCODE). See the messages above for missing Go/headers."
}

Write-Output "Done. dist\dronectrl is on the Windows side -- copy it + config.yaml to the Deck (see README)."
