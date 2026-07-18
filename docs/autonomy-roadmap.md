# Drone autonomy roadmap

> Status: **Active**
>
> Last reviewed: **2026-07-18**
>
> Current milestone: **M0 — DarkStar platform verification and autonomy boundary**
>
> Current implementation: **manual ELRS/CRSF control, analog video, person tracking,
> and pan/tilt auto-aim; autonomous flight has not started**
>
> Active reference platform: **GEPRC DarkStar22, Betaflight Angle mode, analog
> monocular FPV video, ground RTX 3090 PC, and the proven CRSF/ELRS link**

This document is the persistent source of truth for the project's long-term goal:
accept waypoints and fly to them autonomously using visual environmental input, a
persistent 3D reconstruction, and localization within that reconstruction.

It describes direction and graduation criteria. The repository and test evidence
remain the authority on what is implemented today. Agents must not infer that a
roadmap design decision is an implemented feature.

## How to maintain this roadmap

Update this document whenever work materially advances, blocks, or changes it.

- Update **Last reviewed** and **Current milestone** when appropriate.
- Check an item only when the implementation exists or the named owner decision has
  been recorded, and attach concrete evidence for implementation gates.
- Add evidence links beside completed gates: source files, tests, recordings,
  benchmark reports, sanitized Betaflight configuration hashes, Blackbox logs, or
  flight reports.
- Keep benchmark inputs and thresholds versioned. A result without a named dataset,
  configuration, and revision is not evidence.
- Record a new or superseding decision in the decision table before implementing an
  incompatible architecture.
- Do not delete failed or superseded approaches. Summarize the result in the research
  log so later work does not repeat it.
- Do not commit ELRS binding data, credentials, or an unsanitized flight-controller
  dump to the public repository.

## Definition of the goal

The first practical target is a short, low-speed indoor waypoint flight by the
DarkStar22 in a previously mapped, controlled room:

1. Capture and calibrate the existing forward analog camera through the complete
   VTX, receiver, and UVC path.
2. Load or build a persistent metric visual map, initially anchored by printed
   AprilTags.
3. Localize the camera and vehicle in that map with explicit confidence and age.
4. Accept a position and optional yaw waypoint tied to the map UUID and version.
5. Validate the waypoint against a conservative known-safe flight volume.
6. Generate bounded roll, pitch, yaw-rate, and vertical commands.
7. Pass fresh commands through the existing Go safety/sender path and CRSF/ELRS link.
8. Let Betaflight Angle mode retain the onboard attitude, rate, and motor loops.
9. Abort predictably on stale video, lost localization, stale commands, or link loss.

This first result is deliberately tag-assisted. Later milestones add natural-feature
relocalization, a persistent 3D collision map, and operation with fewer or no tags.

### Meaning of camera-primary autonomy

The forward camera is the primary sensor for map-relative localization and scene
memory. That does not remove the flight controller's IMU:

- Betaflight uses its gyroscope in every normal multirotor mode.
- Angle mode also uses the accelerometer to self-level and interpret roll/pitch input
  as a requested attitude.
- Air Mode is a separate low-throttle control-authority feature. The initial
  controller will not depend on it and will avoid near-zero-throttle maneuvering.

The planned Upixel UP-T1-001-Plus combines a downward optical-flow camera and laser
rangefinder. If enabled, it is an auxiliary onboard stabilization and height source;
it is not the persistent map or map-relative localization authority. Results using
its laser range must be described as **sensor-assisted**, not strictly visual-only.

### Initial operating envelope

- Indoor, previously mapped, mostly static, well-lit space.
- DarkStar22 as the primary aircraft; Meteor75 is a later portability target.
- Initial autonomous speed limit of 0.2 m/s.
- Initial height band and angle/throttle limits determined from measured tests, not
  guessed constants.
- Prop-guarded, low-energy craft with no payload mission and approximately 2–5
  minutes of useful test time.
- Initial autonomy is engaged only after a stable human-controlled takeoff and is
  disengaged before landing.
- Unknown or stale space is unsafe.
- The operator explicitly arms and retains an immediate kill/disarm control.
- No operation near uninvolved people, animals, fragile property, or public airspace.

### Explicit non-goals for the first waypoint release

- PX4, ROS 2, a companion computer, or a stereo-camera payload.
- Betaflight's experimental onboard waypoint autopilot on a real aircraft.
- Autonomous arming, takeoff, landing, racing, or acrobatics.
- Autonomous Acro/rate-mode flight.
- Unbounded exploration or flight through unobserved space.
- End-to-end learned motor control.
- Claiming a collision map from monocular or learned depth without conservative
  uncertainty and validation.
- Claiming safety from successful demonstration flights alone.

## Available hardware and current baseline

### Hardware inventory and intent

| Item | State | Roadmap use |
|---|---|---|
| GEPRC DarkStar22 | Available; exact installed firmware, target, wiring, and analog camera/VTX variant still to be captured | Primary development aircraft |
| TAKER F405 20A ELRS AIO | Manufacturer documentation identified; installed configuration not yet verified | Betaflight Angle stabilization and possible spare-UART sensor connection |
| BETAFPV Meteor75 | Available; generation and exact configuration still to be captured | Later ultra-light/no-added-sensor target |
| Analog FPV camera/VTX and UVC receiver | Available and used by the current Linux/Steam Deck path | Initial monocular navigation input; native home-PC capture still required |
| CRSF/ELRS command link | Proven working with the current application | Retained command transport |
| RTX 3090 home PC | Available | Ground perception, localization, mapping, planning, recording, and UI |
| Steam Deck control path | No longer required | May remain compatible, but is not an autonomy dependency or required backup |
| Upixel UP-T1-001-Plus | Planned purchase; owner expects spare 5 V and has considered mounting | Candidate downward optical flow and range aid on DarkStar22, gated by M0/M4 verification |
| Stereo navigation camera | Not owned and not planned initially | Reconsider only after the monocular system is measured |

The DarkStar22 is favored because its larger platform and TAKER F405 board make a
small downward sensor plausible. The Meteor75 is approximately 25 g and its serial
ELRS version exposes only one UART according to the manufacturer, so even a small
sensor creates a much larger integration and flight-time penalty.

### Repository capabilities

| Capability | State | Reuse in autonomy |
|---|---|---|
| Gamepad mapping and 16-channel CRSF transmission | Implemented | Proven command encoding, optional manual control, and actuator transport |
| Disarm, kill, panic chord, stale UI, and input-loss failsafes | Implemented | Preserve and extend with autonomy-command and pose freshness |
| ELRS link and battery telemetry | Implemented | Command link and operator health display |
| Analog 640x480 UVC video capture | Implemented on Linux | Human FPV today; add timestamps, recording, replay, and native home-PC capture |
| YOLO person detection and lightweight 2D tracking | Implemented | Possible dynamic-person mask later; not localization |
| Pan/tilt visual servo at roughly 10 Hz | Implemented | Control-pattern reference only; not vehicle navigation |
| Timestamped calibrated navigation images | Not implemented | Required in M1 |
| Metric camera/vehicle pose and confidence | Not implemented | Required in M2 |
| AprilTag anchor map and relocalization | Not implemented | Required in M2 |
| Persistent natural-feature/3D map | Not implemented | Required in M6 |
| Bounded autonomy-command interface | Not implemented | Required in M0/M3 |
| Waypoint validation and closed-loop controller | Not implemented | Required in M3 |
| Upixel/Betaflight position or altitude hold | Not installed or verified | Candidate assistance in M4 |
| Replay, simulation, and fault injection | Not implemented | Required before motors-on autonomy |

The Go application's independent sender loop and failsafe behavior are assets.
Autonomy must enter through an explicit bounded, sequenced, expiring command
interface; perception or planning may never block or directly own the serial sender.

## Architectural decisions

These decisions remain active until explicitly superseded here. Superseded decisions
are retained as history and must not guide new implementation.

| ID | Decision | State | Rationale |
|---|---|---|---|
| AD-001 | Keep Go manual-only and build autonomy as a separate ROS 2 workspace. | Superseded by AD-011 | The selected Betaflight microdrones and native PC video path favor a smaller sidecar plus the proven Go sender. |
| AD-002 | Use PX4 onboard for stabilization and flight failsafes. | Superseded by AD-012 | Neither available aircraft is a PX4 platform. |
| AD-003 | Use PX4 Offboard setpoints and never synthesize RC channels. | Superseded by AD-013 | Betaflight exposes no equivalent stable Offboard API on these craft; bounded CRSF is the proven interface. |
| AD-004 | Use rigid synchronized global-shutter stereo as the baseline sensor. | Superseded by AD-014 | Stereo payload and transport are a poor fit for the available microdrones and budget. |
| AD-005 | Keep localization, collision, and optional visualization map products distinct. | Accepted | They have different correctness and persistence requirements. |
| AD-006 | Keep global map corrections separate from the smooth local control estimate. | Accepted | Relocalization or map correction must not create a command jump. |
| AD-007 | Run heavy autonomy on the RTX 3090 ground PC during development. | Accepted, revised | The aircraft carries no companion computer; radio/video latency and loss must therefore be explicit failure modes. |
| AD-008 | Use a modular classical baseline before learned replacements. | Accepted | Enables diagnosis, replay, and component-wise regression testing. |
| AD-009 | Treat unknown space as occupied and bind waypoints to a map UUID/version. | Accepted | Prevents stale or unobserved coordinates from being executed. |
| AD-010 | Require an independent RC takeover path for real-flight testing. | Superseded by AD-015 for this low-energy scope | The Steam Deck path is retired; the accepted small-craft test model uses guarded flight, human arming/kill, layered watchdogs, and ELRS loss behavior. Larger/faster aircraft reinstate this requirement. |
| AD-011 | Keep CRSF safety and transmission in Go; run capture/perception/localization as a separate native process and use a narrow local command interface. | Accepted | Reuses proven timing and failsafes without coupling GPU/CV work to the sender loop. |
| AD-012 | Use Betaflight Angle mode on the DarkStar22 for onboard attitude stabilization; do not depend on Air Mode initially. | Accepted, configuration pending | Angle mode gives a bounded attitude-level interface suitable for slow ground-controlled flight. |
| AD-013 | Permit autonomy to send bounded synthetic roll, pitch, yaw, throttle, and mode channels through CRSF/ELRS for these experimental microdrones. | Accepted | It is the only practical control boundary on the available craft; it is not a precedent for larger aircraft. |
| AD-014 | Start with the existing analog monocular camera and AprilTag metric anchors; add natural-feature monocular mapping before reconsidering stereo. | Accepted | Achieves observable scale and relocalization without an aircraft payload or a second video transport. |
| AD-015 | The autonomy process may never arm the aircraft. Preserve explicit operator arm/kill, sender freshness failsafe, ELRS/Betaflight link-loss behavior, guarded test space, and staged props-off/hover gates. | Accepted | Multiple simple barriers are appropriate to the tiny-whoop fun scope even without a second radio path. |
| AD-016 | Plan an Upixel UP-T1-001-Plus on the DarkStar22, subject to exact target, firmware-feature, UART, 5 V, current, mounting, field-of-view, and vibration verification. | Accepted, hardware pending | Downward optical flow and range can reduce altitude/position drift and analog-video control burden. |
| AD-017 | Do not use Betaflight's real-aircraft waypoint autopilot until Betaflight declares it flight-ready and project-specific evidence passes the roadmap gates. | Accepted | Betaflight 2026.6 explicitly labels that autopilot simulation-only. |

## Provisional technology stack

Pin exact versions and interfaces in M0 instead of following moving branches.

- The existing Go application remains the sole CRSF serial-port owner and final
  command/failsafe authority.
- A native home-PC capture/perception sidecar, initially Python plus OpenCV and an
  AprilTag implementation, handles UVC input, calibration, recording, localization,
  mapping, and evaluation. C++ or GPU components may replace measured bottlenecks.
- A loopback-only local interface carries sequence numbers, monotonic timestamps,
  validity deadlines, pose/map identity, and bounded command requests to Go.
- Betaflight remains on the known-working configuration until an export and recovery
  procedure exist. Betaflight 2026.6 is evaluated for UP-T1 position/altitude hold
  only after the exact DarkStar target and build features are verified.
- The existing analog VTX/RX/UVC chain remains the first navigation transport.
- Recordings use a versioned manifest, original frames/video, monotonic capture and
  processing timestamps, commands, health state, configuration hashes, and event
  markers. The exact file/container format is selected in M0/M1.
- AprilTags provide initial metric anchors. Natural-feature monocular SLAM and dense
  reconstruction are evaluated from the same recordings later.
- ROS 2, PX4, Gazebo, and stereo remain future alternatives rather than current
  dependencies.

Licenses must be reviewed before distributing a combined product. ORB-SLAM3 and
several aerial-planning research repositories use GPL-family licenses.

## Target runtime architecture

```text
forward analog camera
        |
   analog VTX ~~ RF ~~> UVC receiver
                              |
                    native capture + timestamps
                              |
                    calibration / undistortion
                              |
          +-------------------+--------------------+
          |                                        |
   AprilTag / monocular pose                session recorder/replay
          |
  smooth pose + confidence <---- persistent map UUID/version
          |
map waypoint --> validation --> trajectory / position controller
                                      |
                       bounded expiring angle/throttle request
                                      |
                         Go autonomy safety supervisor
                                      |
                           CRSF sender --> ELRS link
                                            |
optional downward UP-T1 --> Betaflight Angle/hold loops
                                            |
                                attitude/rate/motor control
```

The optional UP-T1 does not send the persistent-map pose to the ground planner. It
helps Betaflight estimate short-term horizontal motion and height. The forward camera
remains the authority for waypoint coordinates and relocalization.

### Required coordinate frames

- `map`: persistent, metric, globally corrected, z-up frame in which waypoints and
  tag anchors are stored.
- `odom`: locally smooth z-up frame used by the ground position controller.
- `base_link`: vehicle frame used by perception and planning; x forward, y left,
  z up.
- `body_frd`: explicit Betaflight adapter frame; x forward, y right, z down.
- `camera_optical`: calibrated camera frame; x right, y down, z forward.
- `flow_range`: optional downward UP-T1 frame.

Every handedness, axis, sign, and camera-to-body conversion must have executable
tests. No control code may rely on an implicit “looks right” convention.

### Required map products

1. **Anchor map** — AprilTag IDs, sizes, poses, surveyed uncertainty, calibration
   hash, map UUID, and schema version.
2. **Relocalization map** — later natural-feature keyframes, landmarks/descriptors,
   pose graph, scale anchors, calibration hash, and map identity.
3. **Collision/geofence map** — initially a hand-validated conservative safe volume;
   later metric occupancy/distance queries with observation age and vehicle-radius
   inflation.
4. **Visualization map** — optional mesh or Gaussian representation. It is never the
   sole source of free space.

The persistent base map and current-session observations remain distinguishable.
Unknown or stale space cannot become trusted free space merely because a monocular
depth model predicts it.

### Target autonomy state machine

| State | Meaning | Allowed transition on failure |
|---|---|---|
| `DISARMED` | Motors safe; mapping, calibration, and replay may run | Remain disarmed |
| `MANUAL` | Human commands through the existing input path | Kill/disarm |
| `READY` | Video, pose, map, command link, and safety checks pass; autonomy output is inhibited | Back to `MANUAL`/`DISARMED` |
| `AUTO_HOLD` | Ground controller maintains a validated pose after human takeoff | `DEGRADED` or operator kill |
| `AUTO_WAYPOINT` | A valid map-bound waypoint is being tracked | `AUTO_HOLD`, `DEGRADED`, or operator kill |
| `DEGRADED` | New waypoint motion is stopped; bounded neutral/hold/descend policy executes according to verified available sensors | Operator kill; recovery requires explicit health dwell and approval |
| `KILL` | Arm channel and throttle go to configured safe values | `DISARMED` only |

The initial loss policy may be “neutral attitude then kill” rather than landing,
because a blind throttle descent is not automatically safer. Controlled descent is
enabled only after UP-T1 altitude hold and Betaflight failsafe landing are verified.
All age, confidence, command, and angle/throttle thresholds are measured,
configuration-controlled, and logged.

## Milestones and evidence gates

Time ranges are planning estimates, not deadlines. Evidence gates determine
progression.

### M0 — DarkStar platform verification and autonomy boundary (current, 1–2 weeks)

Objective: freeze the real hardware/software boundary and create a testable command
contract without spinning motors.

- [x] Select the DarkStar22 as the primary aircraft and Meteor75 as a later target.
      Evidence: owner inventory/constraints recorded 2026-07-18.
- [x] Select the ground RTX 3090 PC, existing analog video, and proven CRSF/ELRS link.
      Evidence: owner inventory/constraints recorded 2026-07-18.
- [x] Select Betaflight Angle mode for onboard stabilization with no initial Air Mode
      dependency. Evidence: owner decision recorded 2026-07-18.
- [x] Select monocular/tag-assisted localization instead of an initial stereo payload.
      Evidence: size/budget decision recorded 2026-07-18.
- [x] Record the UP-T1-001-Plus as the planned DarkStar sensor candidate, with spare
      5 V and mounting believed feasible but unverified. Evidence: owner assessment
      recorded 2026-07-18.
- [ ] Export and sanitize the exact DarkStar and Meteor Betaflight versions, targets,
      resource maps, modes, channel mapping, failsafe settings, and factory recovery
      artifacts.
- [ ] Record the exact analog camera, VTX, receiver, UVC device IDs, formats,
      resolutions, frame rates, and native home-PC enumeration.
- [ ] Verify from the installed DarkStar wiring and target that a UART, 5 V supply,
      current budget, and required Betaflight build features are available for UP-T1.
- [ ] Define the local pose/health/command schema, map identity, validity deadline,
      units, frames, and command envelope.
- [ ] Specify the autonomy state machine and exact stale-command, video-loss,
      localization-loss, ELRS-loss, and operator-kill behavior.
- [ ] Add pure tests for frame conversions, command clamping, sequence rejection,
      stale-command rejection, and forbidden autonomy arming.

Graduation evidence:

- [ ] Sanitized hardware/configuration reports and recovery instructions exist.
- [ ] A replay/synthetic client can request bounded commands but cannot arm, exceed
      limits, or keep commands alive after its deadline.
- [ ] Removing the autonomy process or its heartbeat produces the specified sender
      state without blocking CRSF transmission.
- [ ] Frame and state-machine behavior has automated tests or a reviewed executable
      specification.

### M1 — timestamped analog sensing, calibration, and replay (2–3 weeks)

Objective: make the complete analog perception input reproducible and quantify its
latency and failure modes.

- [ ] Capture the UVC feed natively on the home PC with a monotonic arrival timestamp
      and increasing frame sequence.
- [ ] Detect and log duplicate, dropped, malformed, and format-changing frames.
- [ ] Record original frames/video plus timestamps, configuration, and session events.
- [ ] Implement deterministic or controlled-rate replay without camera hardware.
- [ ] Calibrate intrinsics and distortion through the complete analog/UVC chain.
- [ ] Calibrate and version the camera-to-`base_link` transform.
- [ ] Measure glass-to-compute latency and jitter with a repeatable visual event test.
- [ ] Add controlled analog degradation: noise, blur, interference, frame repetition,
      dropouts, cropping, and delay.

Graduation evidence:

- [ ] A representative recording sustains the selected frame rate without timestamp
      regression or silent frame reuse.
- [ ] Calibration validation includes reprojection error and named capture settings.
- [ ] Latency distributions, not just averages, are recorded for at least three
      representative link conditions.
- [ ] Every downstream perception test can run from a versioned recording.

### M2 — AprilTag metric localization and persistent anchor map (2–4 weeks)

Objective: produce a metric, confidence-bearing pose from the existing monocular
camera without aircraft modifications.

- [ ] Define a versioned AprilTag anchor-map format with tag size, pose, uncertainty,
      map UUID, calibration hash, and coordinate conventions.
- [ ] Detect tags and estimate camera pose with geometric residual checks.
- [ ] Transform camera pose to vehicle pose using the calibrated rigid transform.
- [ ] Filter and latency-predict pose without hiding measurement age or tag loss.
- [ ] Publish pose, velocity estimate, confidence, visible anchors, residuals,
      tracking state, and reset/relocalization events.
- [ ] Provide a map survey/build tool and visualization.
- [ ] Build an evaluation harness for position/orientation error, lost time, false
      acceptance, relocalization time, and end-to-end latency.
- [ ] Run a natural-feature monocular method in shadow mode only after the tag
      baseline is frozen.

Graduation evidence in the named initial room/flight volume:

- [ ] Static pose error and repeatability fit the documented control/clearance budget;
      initial target is at most 0.10 m position error in the validated central volume.
- [ ] No false accepted relocalization occurs in the release recording set.
- [ ] Tracking loss and recovery are explicit within one frame; stale pose is never
      reported as current.
- [ ] The measured pose-age distribution supports the 0.2 m/s initial speed limit
      with the documented braking/error margin.

### M3 — bounded controller and closed-loop simulation (3–5 weeks)

Objective: execute tag-map waypoints entirely in replay/simulation through the real
Go command-safety boundary.

- [ ] Accept map-versioned position/yaw waypoints with acceptance radius and speed
      limits.
- [ ] Reject waypoints outside the hand-validated safe volume or with a map mismatch.
- [ ] Implement a smooth position controller producing bounded Angle-mode requests,
      yaw rate, and a separately gated vertical command.
- [ ] Integrate the local autonomy interface with the Go state store and sender while
      preserving manual control and all existing failsafes.
- [ ] Implement `READY`, `AUTO_HOLD`, `AUTO_WAYPOINT`, `DEGRADED`, and `KILL`.
- [ ] Simulate aircraft response, analog delay, command delay, and measured
      Betaflight/ELRS behavior sufficiently to tune only conservative initial bounds.
- [ ] Inject camera freeze, tag loss, false pose, pose jump, stale map, controller
      stall, command-link loss, and kill.

Graduation evidence:

- [ ] At least 1,000 seeded randomized simulated missions complete with zero
      configured safe-volume violations.
- [ ] Every injected fault produces the specified bounded state and command output.
- [ ] Replaying a failing seed reproduces the failure sufficiently for diagnosis.
- [ ] Autonomy cannot arm, bypass clamping, block the sender, or keep a stale command
      active.

### M4 — UP-T1 bench integration and assisted hover (2–4 weeks)

Objective: validate the optional DarkStar stabilization aid without using
Betaflight's real-aircraft waypoint autopilot.

- [ ] Preserve a tested factory recovery image/configuration before any firmware
      change.
- [ ] Verify that the exact DarkStar target can build and run the required optical
      flow, rangefinder, altitude-hold, and position-hold features within resource
      limits.
- [ ] Purchase the UP-T1-001-Plus only after M0 electrical/firmware checks pass.
- [ ] Document and inspect the downward mount, field of view, prop/duct occlusion,
      airflow, vibration isolation, wiring strain relief, mass, and center-of-gravity
      effect.
- [ ] Complete props-off power, UART at 115200 baud, sensor quality, range, direction,
      and failsafe tests.
- [ ] Validate range/flow behavior over representative floors, heights, lighting,
      texture, and motion.
- [ ] Tune and validate Betaflight Angle plus altitude/position hold manually at low
      energy; do not enable `AUTOPILOT` on the real craft.
- [ ] Determine hover throttle and safe neutral/kill/optional controlled-descent
      behavior from logs.

Graduation evidence:

- [ ] Props-off logs show valid, correctly oriented, bounded flow/range data and no
      scheduler, UART, power, or receiver regression.
- [ ] Ten manual assisted hovers remain inside the documented position/height bounds
      without unexpected mode exit or flyaway tendency.
- [ ] Sensor disconnect, bad quality, out-of-range floor, and ELRS loss each produce
      the specified behavior.
- [ ] The original known-working Betaflight configuration can be restored.

### M5 — graduated tag-map waypoint flight (3–6 weeks)

Objective: demonstrate conservative waypoint control on the DarkStar22 without
autonomous takeoff or landing.

- [ ] Complete props-off end-to-end tests with live video and CRSF output.
- [ ] Complete guarded/tethered or equivalently contained hover engagement and abort
      tests.
- [ ] Progress through shadow commands, `AUTO_HOLD`, 0.25 m translation, one
      waypoint, multiple waypoints, and a known-map route.
- [ ] Enforce the 0.2 m/s initial speed and measured angle/height limits.
- [ ] Demonstrate operator kill, camera freeze, tag/localization loss, command-process
      exit, ELRS loss, low battery, and optional UP-T1 loss.
- [ ] Record frames, timestamps, pose, commands, health, map/config IDs, software
      revision, Betaflight/Blackbox data where available, and an operator report for
      every flight.

Graduation evidence:

- [ ] Ten consecutive representative low-speed missions complete without safety
      intervention or safe-volume violation.
- [ ] Cross-track and endpoint error stay within the documented margin.
- [ ] Every practical abort/failsafe is demonstrated successfully.
- [ ] No result is described as camera-only if laser range aided the control loop.

### M6 — natural-feature 3D memory and conservative collision mapping (4–8+ weeks)

Objective: move beyond a pure tag map while retaining metric scale, replayability,
and conservative free-space semantics.

- [ ] Integrate a monocular SLAM/reconstruction baseline using AprilTags and/or
      verified range only as scale/pose anchors.
- [ ] Preserve a smooth `odom` estimate while applying global corrections in
      `map`.
- [ ] Serialize keyframes, landmarks/descriptors, pose graph, calibration hash, map
      UUID, and schema version.
- [ ] Build a conservative collision/geofence representation with observation age,
      uncertainty, and vehicle-radius inflation.
- [ ] Separate persistent base map from current-session observations.
- [ ] Validate saved-map relocalization across lighting, viewpoint, and day changes.
- [ ] Revalidate waypoints after every map/calibration version change.
- [ ] Characterize thin obstacles, low texture, analog interference, and monocular
      depth/scale failure modes.

Graduation evidence:

- [ ] Save/load/relocalize preserves scale and waypoint coordinates within the
      measured control budget.
- [ ] Unknown or stale regions cannot be returned as trusted free space.
- [ ] Map correction never creates a discontinuity in commanded local motion.
- [ ] Tag-reduced tests meet the documented relocalization recall while retaining
      zero false accepted localizations in the release dataset.

### M7 — portability and research improvements (ongoing, after the baseline)

Candidate work:

- Port the tag/localization/control stack to the Meteor75 without the UP-T1.
- Reduce tag density and evaluate markerless relocalization.
- Foundation-model or deep SLAM: DPVO/Deep Patch SLAM, MASt3R-SLAM, SLAM3R, VGGT-SLAM.
- Learned metric depth as a multi-view prior, never unqualified free-space truth.
- Active perception that favors texture, parallax, camera field of view, and map
  observability while planning.
- Semantic or hierarchical memory inspired by Hydra/maplab.
- Photorealistic Gaussian mapping for inspection and operator experience.
- Reconsider digital video, stereo, an onboard companion, or PX4 only if measured
  limitations justify the payload and architectural cost.

A research component replaces the baseline only if it improves a frozen dataset and
mission suite without weakening worst-case latency, observability, confidence
reporting, replayability, or fault response.

## Immediate prioritized backlog

Unless the user changes priorities, agents should pull work from this order:

1. Add a non-secret hardware/configuration capture procedure for both aircraft and
   the home-PC UVC receiver; record the exact DarkStar UART/5 V/target facts.
2. Specify and test the bounded local autonomy command/health interface and update the
   Go safety model so an autonomy sidecar can never arm or outlive its deadline.
3. Implement native home-PC UVC recording with monotonic frame sequence/timestamps
   and deterministic replay.
4. Build camera calibration and end-to-end analog-video latency tools.
5. Define the AprilTag anchor-map schema and implement replay-only pose estimation.
6. Build the closed-loop Angle-mode controller simulation and fault-injection suite.
7. Verify UP-T1 firmware/electrical compatibility, then prepare a purchase and
   props-off integration checklist.

Do not prioritize new detector classes, richer OSD work, Gaussian rendering,
Betaflight real-aircraft `AUTOPILOT`, stereo purchases, or motors-on autonomy ahead
of this backlog unless they fix an existing safety issue or the user explicitly
redirects the project.

## Research basis

Primary references behind the active choices:

- [GEPRC DarkStar22 downloads and TAKER F405 wiring guidance](https://geprc.com/downloads/darkstar22/)
- [BETAFPV Meteor75 specifications](https://betafpv.com/products/meteor75-brushless-whoop-quadcopter-1s)
- [Betaflight 2026.6 release notes: optical-flow hold and simulation-only autopilot](https://betaflight.com/docs/wiki/release/Betaflight-2026-6-Release-Notes)
- [Betaflight Position Hold behavior](https://betaflight.com/docs/wiki/guides/current/Position-Hold-2025-12)
- [Betaflight supported sensors](https://betaflight.com/docs/wiki/guides/current/Supported-Sensors)
- [Upixel UP-T1 product/protocol page](https://www.upixels.com/pro_59008423.html)
- [AprilTag 3](https://github.com/AprilRobotics/apriltag)
- [OpenCV camera calibration](https://docs.opencv.org/4.x/dc/dbb/tutorial_py_calibration.html)
- [ORB-SLAM3: visual, visual-inertial, and multi-map SLAM](https://arxiv.org/abs/2007.11898)
- [Deep Patch Visual Odometry](https://proceedings.neurips.cc/paper_files/paper/2023/hash/7ac484b0f1a1719ad5be9aa8c8455fbb-Abstract-Conference.html)
- [MASt3R-SLAM](https://arxiv.org/abs/2412.12392)
- [VGGT](https://openaccess.thecvf.com/content/CVPR2025/html/Wang_VGGT_Visual_Geometry_Grounded_Transformer_CVPR_2025_paper.html)
- [SLAM3R](https://arxiv.org/abs/2412.09401)
- [2026 ScaleMaster monocular scale-consistency benchmark](https://arxiv.org/abs/2602.18174)
- [Hydra persistent hierarchical spatial perception](https://arxiv.org/abs/2201.13360)
- [maplab 2.0 multi-session mapping](https://arxiv.org/abs/2212.00654)

Superseded platform references are retained for historical context:

- [PX4 Offboard mode](https://docs.px4.io/main/en/flight_modes/offboard)
- [PX4 ROS 2/uXRCE-DDS integration](https://docs.px4.io/main/en/middleware/uxrce_dds)
- [nvblox: GPU-accelerated TSDF/ESDF mapping](https://arxiv.org/abs/2311.00626)
- [Voxblox: incremental ESDFs for MAV planning](https://arxiv.org/abs/1611.03631)

## Research and decision log

Append dated entries when an experiment or new fact changes the roadmap.

| Date | Topic | Result / decision | Evidence |
|---|---|---|---|
| 2026-07-18 | Initial autonomy review | Proposed a separate PX4/ROS 2 stack with synchronized stereo and Offboard setpoints. | Repository review and initial research; superseded below after hardware inventory |
| 2026-07-18 | Available-aircraft and budget review | Selected the available DarkStar22 as primary and Meteor75 as later target; no payload mission, 2–5 minute tests, and up to USD 200 hardware budget. | Owner-provided inventory and constraints |
| 2026-07-18 | Control architecture revision | Retain the proven Go CRSF/ELRS sender and use bounded synthetic RC commands into Betaflight Angle mode; retire PX4/ROS 2 as the initial dependency. | Available aircraft lack PX4; owner confirmed ELRS path and Angle-mode preference |
| 2026-07-18 | Navigation sensor revision | Use the existing analog monocular camera with AprilTag metric anchors first; do not purchase stereo initially. | Aircraft payload/transport constraints and owner camera inventory |
| 2026-07-18 | UP-T1 candidate | Plan the Upixel UP-T1-001-Plus for downward flow/range assistance on DarkStar22, subject to firmware, UART, 5 V, mounting, and props-off validation. | Betaflight 2026.6 documentation and owner assessment that 5 V/mounting are likely feasible |
