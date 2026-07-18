# Agent instructions

## Required project context

Before planning or implementing work in this repository, read:

1. [`docs/autonomy-roadmap.md`](docs/autonomy-roadmap.md) — the source of truth for
   the long-term drone-autonomy architecture, current milestone, safety boundaries,
   and graduation gates.
2. [`CLAUDE.md`](CLAUDE.md) — the current `elrsctrl` architecture, development
   commands, and implementation conventions.
3. [`README.md`](README.md) — the current user-facing behavior and hardware setup.

Do not describe a roadmap capability as implemented unless it exists in the
repository and has passed the roadmap's stated evidence gate.

## Roadmap discipline

- Work from the current milestone and its dependencies unless the user explicitly
  changes priorities.
- Preserve the boundary between the existing Go/ELRS serial sender and the future
  capture/perception/localization sidecar. Autonomy may enter Go only through the
  roadmap's bounded, sequenced, expiring local interface.
- Betaflight Angle mode must retain the onboard attitude/rate/motor loops. Autonomous
  CRSF requests are permitted only for the selected experimental microdrones and must
  pass Go-side clamping, freshness, state, and kill/disarm enforcement.
- The autonomy process must never arm the aircraft. Preserve explicit operator
  arm/kill, existing defense-in-depth failsafes, ELRS/Betaflight loss behavior, and
  the roadmap's staged props-off and controlled-flight gates.
- Do not enable Betaflight's experimental real-aircraft waypoint autopilot.
- Treat camera timestamps, calibration, coordinate frames, estimator confidence,
  analog-link latency, command age, and replayable logs as correctness requirements,
  not optional polish.
- Treat the Upixel UP-T1-001-Plus as planned, not installed or compatible, until the
  exact DarkStar firmware target, UART, 5 V/current budget, mounting, and props-off
  evidence satisfy the roadmap.
- Do not proceed to a real-flight milestone until the preceding simulation, replay,
  fault-injection, and safety gates are satisfied.

When a change materially advances, blocks, or revises the roadmap, update
`docs/autonomy-roadmap.md` in the same change:

- update **Last reviewed**, **Current milestone**, or the relevant checklist;
- link concrete evidence such as tests, logs, benchmark reports, or implementation
  files;
- record changed architectural decisions instead of silently contradicting them;
- keep incomplete or unverified work visibly incomplete.

## Repository hygiene

- Use `rg`/`rg --files` for searches.
- Preserve unrelated user changes in a dirty worktree.
- Use the local Go setup documented in `CLAUDE.local.md` when it exists.
- For the current Go application, run `go vet ./...` and `go test ./...` after
  relevant changes. Do not launch the Ebiten GUI in a headless environment.
