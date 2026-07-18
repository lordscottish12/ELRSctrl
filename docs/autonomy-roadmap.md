# Drone autonomy roadmap

> Status: **Active**
>
> Last reviewed: **2026-07-18**
>
> Current milestone: **M0 — autonomy foundation and platform decisions**
>
> Current implementation: **manual ELRS/CRSF control, analog video, person tracking,
> and pan/tilt auto-aim; autonomous flight has not started**

This document is the persistent source of truth for the project's long-term goal:
accept waypoints and fly to them autonomously using visual environmental input, a
persistent 3D reconstruction, and localization within that reconstruction.

It describes direction and graduation criteria. The repository and test evidence
remain the authority on what is implemented today. Agents must not infer that a
roadmap design decision is an implemented feature.

## How to maintain this roadmap

Update this document whenever work materially advances, blocks, or changes it.

- Update **Last reviewed** and **Current milestone** when appropriate.
- Check an item only when the implementation exists and its stated evidence has been
  collected.
- Add evidence links beside completed gates: source files, tests, benchmark reports,
  rosbag names, PX4 logs, or flight reports.
- Keep benchmark inputs and thresholds versioned. A result without a named dataset,
  configuration, and revision is not evidence.
- Record a new or superseding decision in the decision table before implementing an
  incompatible architecture.
- Do not delete failed approaches. Summarize the result in the research log so later
  work does not repeat it.

## Definition of the goal

The first production-shaped target is conservative indoor waypoint flight in a
previously mapped, mostly static environment:

1. Load or build a persistent visual map.
2. Localize the vehicle in a stable metric coordinate frame.
3. Accept a position and optional yaw waypoint tied to that map's identity/version.
4. Validate that the waypoint and route occupy observed free space with adequate
   vehicle clearance.
5. Generate a dynamically feasible trajectory and send bounded setpoints to an
   onboard flight controller.
6. Replan around newly observed obstacles and transition safely on stale perception,
   estimator degradation, or communications loss.

“Visual-only” means that cameras are the only sensors used to perceive and localize
against the external environment. It does **not** remove the flight controller's IMU
from the high-rate attitude and motor stabilization loops. Free flight without an
onboard inertial attitude loop is out of scope and unsafe.

### Initial operating envelope

- Indoor, mapped, mostly static spaces.
- Low-speed flight, initially no faster than 0.5 m/s.
- A small, guarded or otherwise low-energy test vehicle.
- Unknown space is occupied.
- A safety pilot has an independent RC takeover path.
- No operation near uninvolved people, animals, fragile property, or public airspace.

### Explicit non-goals for the first waypoint release

- Aggressive racing or acrobatics.
- Autonomous flight implemented as synthesized CRSF stick/channel commands.
- Reliance on a photorealistic Gaussian/NeRF map as the only collision map.
- Unbounded exploration of unknown space.
- End-to-end learned motor control.
- A pan/tilt navigation camera without synchronized joint-state compensation.
- Claiming safety from successful demonstration flights alone.

## Current repository baseline

| Capability | State | Reuse in autonomy |
|---|---|---|
| Gamepad mapping and 16-channel CRSF transmission | Implemented | Manual operation and backup control |
| Disarm, kill, panic chord, stale UI, and input-loss failsafes | Implemented | Preserve; add autonomy-specific and PX4 failsafes |
| ELRS link and battery telemetry | Implemented | Operator health display; insufficient for flight state |
| Analog 640x480 UVC video capture | Implemented | Human FPV/OSD only; not the navigation sensor |
| YOLO person detection and lightweight 2D tracking | Implemented | Candidate dynamic-person mask; not localization or obstacle mapping |
| Pan/tilt visual servo at roughly 10 Hz | Implemented | Turret feature only; must not control vehicle attitude or position |
| Timestamped calibrated navigation images | Not implemented | Required in M1 |
| Pose/velocity estimation and coordinate-frame tree | Not implemented | Required in M2 |
| Persistent sparse map and relocalization | Not implemented | Required in M2/M3 |
| Metric TSDF/occupancy/ESDF map | Not implemented | Required in M3 |
| Waypoint validation and trajectory planning | Not implemented | Required in M4 |
| PX4 offboard integration | Not implemented | Required in M0/M4 |
| Simulation, rosbag replay, and fault injection | Not implemented | Required before real flight |

The current Go application is healthy and deliberately simple. Its independent sender
loop and failsafe behavior are assets, but its channel snapshot, frame buffer, and
person-tracker abstractions do not provide the timing, geometry, uncertainty, or
flight-controller interfaces required for autonomy.

## Architectural decisions

These decisions remain active until explicitly superseded here.

| ID | Decision | State | Rationale |
|---|---|---|---|
| AD-001 | Keep the Go `elrsctrl` application as operator console/manual-control software; build autonomy as a separate ROS 2 workspace. | Accepted | Avoid embedding SLAM/planning in an immediate-mode UI and preserve a working control tool. |
| AD-002 | Use PX4 onboard for attitude, rate, motor control, arming checks, and primary flight failsafes. | Accepted | High-rate stabilization must not depend on video, a desktop UI, or a radio round trip. |
| AD-003 | Send autonomous position/velocity/trajectory setpoints through PX4 Offboard/ROS 2, never autonomous raw RC channels. | Accepted | Provides bounded interfaces, estimator integration, proof-of-life, and link-loss actions. |
| AD-004 | Use a rigid, synchronized global-shutter stereo camera as the baseline navigation sensor. | Accepted, hardware pending | Stereo preserves visual-only environmental sensing while making metric scale observable and reducing motion-blur/rolling-shutter failure modes. |
| AD-005 | Keep three map products: sparse relocalization map, metric collision map, and optional photorealistic map. | Accepted | Localization, safety planning, and visualization have different correctness requirements. |
| AD-006 | Keep `map -> odom` global corrections separate from smooth `odom -> base_link` control state. | Accepted | Loop closures must not inject pose jumps into the flight controller. |
| AD-007 | Run heavy autonomy on the RTX 3090 during development; move watchdogs and necessary local autonomy onboard after profiling. | Accepted | Current GPU capacity is adequate; sensing, timing, and safe compute placement are higher priorities. |
| AD-008 | Use a modular classical baseline before learned replacements. | Accepted | Enables diagnosis, deterministic replay, shadow evaluation, and component-wise regression testing. |
| AD-009 | Treat unknown space as occupied and bind each waypoint to a map UUID/version. | Accepted | Prevents planning through unobserved space or executing stale coordinates against a different map. |
| AD-010 | Require an RC takeover path independent of the autonomy computer/process during real-flight testing. | Accepted | A software kill path on the same failing computer is not independent. |

### Provisional technology stack

Pin exact versions and compatibility in M0 rather than following moving `main`
branches.

- Ubuntu Linux and ROS 2, with Jazzy as the preferred general baseline unless a
  required NVIDIA/PX4 package forces a documented alternative.
- PX4 stable firmware, QGroundControl, uXRCE-DDS or a well-defined MAVLink bridge.
- Modern Gazebo/PX4 SITL for simulation.
- C++ for latency-sensitive runtime nodes; Python for evaluation and research
  adapters; Go remains the existing console.
- `rosbag2` plus PX4 ULog for synchronized replay and diagnosis.
- ORB-SLAM3 stereo as the first localization baseline.
- Stereo depth plus nvblox or an equivalent TSDF/ESDF implementation as the first
  collision map.

Licenses must be reviewed before distributing a combined product. ORB-SLAM3 and
several aerial-planning research repositories use GPL-family licenses.

## Target runtime architecture

```text
rigid timestamped stereo camera
        |
        +--> rectification/stereo depth --------+
        |                                        |
        +--> visual odometry / SLAM              v
                    |                    local TSDF/occupancy --> ESDF
                    |                               |
                    +--> smooth odom pose           |
                    |        |                      |
persistent sparse <-+        +--> PX4 external      |
keyframe map                    vision estimate      |
        |                                             |
        +--> relocalization --> map->odom             |
                                                      v
map-versioned waypoint --> validation --> global route --> local trajectory
                                                             |
                                                      bounded setpoints
                                                             |
                                                         PX4 Offboard
                                                             |
                                               attitude/rate/motor control
```

### Required coordinate frames

- `map`: persistent, globally corrected frame in which waypoints are stored.
- `odom`: locally smooth frame used by trajectory tracking.
- `base_link`: vehicle body frame.
- `camera_left`, `camera_right`: calibrated, rigid camera frames.
- Optional render/semantic frames must derive from these and may not create a second
  competing pose authority.

### Required map products

1. **Relocalization map** — keyframes, landmarks/point maps, descriptors, pose graph,
   calibration hash, map UUID, and serialization version.
2. **Collision map** — metric TSDF/occupancy plus ESDF or equivalent conservative
   distance queries, vehicle-radius inflation, observation age, and unknown space.
3. **Visualization map** — optional mesh or Gaussian representation. It may assist
   inspection and place recognition but is never the sole source of free space.

The persistent base map and current-session observations must remain distinguishable.
New obstacles become occupied immediately; clearing a persistent obstacle requires
repeated consistent evidence and a versioned map update.

### Target autonomy state machine

| State | Meaning | Allowed transition on failure |
|---|---|---|
| `DISARMED` | Motors safe; mapping/replay may run | Remain disarmed |
| `MANUAL` | Safety pilot controls vehicle | Disarm or manual landing |
| `READY` | Camera, pose, map, planner, PX4, and takeover link pass preflight checks | Back to `MANUAL`/`DISARMED` |
| `OFFBOARD` | Valid trajectory and proof-of-life are reaching PX4 | `BRAKE`/`LAND` on degraded input |
| `BRAKE` | Stop within currently observed free space while assessing recovery | `OFFBOARD` only after explicit recovery; otherwise `LAND` |
| `LAND` | Controlled landing under PX4/onboard supervision | Disarm after landing |

Thresholds for stale images, stale pose, covariance, ESDF age, and link timeout must be
measured and configuration-controlled. They must not be scattered as unexplained
constants across nodes.

## Milestones and evidence gates

Time ranges are planning estimates for one experienced full-time developer, not
deadlines. Safety/evidence gates determine progression.

### M0 — autonomy foundation and platform decisions (current, 1–2 weeks)

Objective: establish the flight-control boundary and a repeatable simulation platform.

- [ ] Select and document the PX4-supported airframe and flight controller.
- [ ] Select the navigation-camera class and document synchronization, shutter,
      calibration, mass, power, and transport requirements.
- [ ] Decide initial compute topology: ground RTX 3090, command/data links, and what
      must run onboard for link-loss handling.
- [ ] Create the ROS 2 autonomy workspace and package boundaries.
- [ ] Pin ROS 2, PX4, Gazebo, bridge, CUDA, and driver versions in a reproducible
      environment.
- [ ] Bring up PX4 SITL, QGroundControl, ROS 2 communications, and a minimal Offboard
      proof-of-life/setpoint example.
- [ ] Define frames, waypoint message/schema, map UUID/version fields, and autonomy
      health/status messages.
- [ ] Define the independent RC takeover and PX4 Offboard-loss behavior.

Graduation evidence:

- [ ] A repeatable setup command or container starts simulation and communications.
- [ ] A simulated vehicle accepts a bounded setpoint and triggers the configured PX4
      failsafe when proof-of-life is removed.
- [ ] Frame conventions and safety-state transitions have automated tests or a
      reviewed executable specification.

### M1 — timestamped sensing, logging, and simulation (2–3 weeks)

Objective: make every perception result reproducible and its latency measurable.

- [ ] Publish synchronized stereo images and `camera_info` with capture timestamps.
- [ ] Calibrate intrinsics, stereo extrinsics, and camera-to-body transform; version
      calibration by hash.
- [ ] Record images, camera metadata, transforms, PX4 state, setpoints, health, and
      link data in rosbag2; retain PX4 ULogs.
- [ ] Implement deterministic or controlled-rate replay.
- [ ] Simulate image noise, blur, dropped frames, transport latency, and clock offset.
- [ ] Measure capture-to-pose and pose-to-setpoint latency distributions.

Graduation evidence:

- [ ] A representative recording sustains the target 30–60 FPS without timestamp
      regression or silent frame reuse.
- [ ] Calibration validation is documented with reprojection/error metrics.
- [ ] The complete perception pipeline can run from a recorded bag without hardware.

### M2 — metric localization baseline and bake-off (3–5 weeks)

Objective: produce smooth, confidence-bearing metric pose/velocity and persistent
relocalization candidates.

- [ ] Integrate ORB-SLAM3 stereo as the baseline.
- [ ] Publish pose, velocity, covariance/confidence, tracking state, reset events, and
      map identity.
- [ ] Feed only the smooth local estimate to PX4; isolate loop-closure corrections in
      `map -> odom`.
- [ ] Build an evaluation harness for ATE, RPE, scale drift, lost time, relocalization
      time, latency, CPU/GPU load, and VRAM.
- [ ] Evaluate EuRoC, TartanAir, UZH-FPV, and project-specific recordings.
- [ ] Benchmark at least one learned challenger, initially DPVO/Deep Patch SLAM or
      MASt3R-SLAM, without replacing the baseline.

Graduation evidence on a versioned ten-minute project route:

- [ ] No unrecovered tracking loss in the defined initial operating envelope.
- [ ] Metric drift and trajectory error remain below the route's documented clearance
      budget; the initial target is approximately 1% path-length error or 0.10 m,
      whichever is stricter for the test area.
- [ ] p95 pose latency is below 80 ms, with a stretch target below 60 ms.
- [ ] Saved-map relocalization succeeds within 2 seconds with zero false accepted
      relocalizations in the release dataset.

### M3 — collision mapping and persistent memory (3–5 weeks)

Objective: construct a conservative metric world model suitable for planning.

- [ ] Produce synchronized stereo depth with per-pixel validity/uncertainty.
- [ ] Integrate TSDF/occupancy and ESDF queries with observation age.
- [ ] Inflate obstacles by vehicle radius plus localization/control margin.
- [ ] Mask people and other confirmed dynamic objects from the static base map while
      retaining them as current occupied space.
- [ ] Serialize relocalization maps and collision submaps with map UUID, schema
      version, calibration hash, and creation/update metadata.
- [ ] Implement base-map/session-overlay separation and conservative change handling.
- [ ] Provide map inspection and waypoint-free-space query tools.

Graduation evidence:

- [ ] Safety-critical obstacles in the versioned test set are represented after
      inflation; thin-obstacle limitations are explicitly characterized.
- [ ] Unknown or stale voxels cannot be returned as trusted free space.
- [ ] Save/load/relocalize preserves map scale and waypoint coordinates.
- [ ] Loop closure or map correction does not create a discontinuity in commanded
      local motion.

### M4 — waypoint planning and closed-loop simulation (4–6 weeks)

Objective: execute safe waypoint missions entirely in simulation/HIL.

- [ ] Accept map-versioned position/yaw waypoints with acceptance radius and speed
      limits.
- [ ] Reject waypoints outside observed connected free space or with inadequate
      clearance.
- [ ] Implement global route search and a dynamically feasible local trajectory.
- [ ] Replan at a bounded rate using the current ESDF/occupancy map.
- [ ] Publish PX4 Offboard setpoints at 20–50 Hz with watchdogs and health gating.
- [ ] Implement `READY`, `OFFBOARD`, `BRAKE`, and `LAND` transitions.
- [ ] Inject camera freeze, pose dropout/jump, false relocalization candidate, stale
      ESDF, blocked planner, command-link loss, and RC takeover.

Graduation evidence:

- [ ] At least 1,000 seeded randomized simulated missions complete with zero
      collision-envelope violations.
- [ ] Every injected fault produces the specified bounded response and PX4 mode/state.
- [ ] Replaying a failing seed reproduces the failure deterministically enough to
      diagnose it.

### M5 — graduated real flight in a known map (4–8 weeks)

Objective: demonstrate conservative waypoint flight without skipping safety stages.

- [ ] Complete props-off hardware-in-the-loop tests.
- [ ] Complete guarded/tethered vision hover and abort tests.
- [ ] Progress through 0.5 m translation, one waypoint, multiple waypoints, and a
      known-map route at no more than the initial speed limit.
- [ ] Demonstrate manual takeover, camera-loss response, localization-loss response,
      command-link loss, and low-battery behavior in controlled conditions.
- [ ] Record a rosbag, ULog, software revision, map ID, configuration, and operator
      report for every flight.

Graduation evidence:

- [ ] Twenty consecutive representative low-speed missions complete without safety
      intervention or clearance violation.
- [ ] Cross-track/endpoint error stays inside the documented safety margin.
- [ ] Independent takeover and each practical failsafe are demonstrated successfully.

### M6 — unknown obstacles and multi-session memory (3–6+ weeks)

Objective: operate repeatedly in a changing mapped environment.

- [ ] Replan around newly observed static and dynamic obstacles.
- [ ] Limit speed using visible free-space/stopping-distance and estimator confidence.
- [ ] Detect persistent scene changes without immediately corrupting the base map.
- [ ] Revalidate saved waypoints after map updates.
- [ ] Test lighting, viewpoint, furniture/layout, and partial-map changes across days.
- [ ] Evaluate stronger visual place recognition such as AnyLoc or SALAD with mandatory
      geometric verification.

Graduation evidence:

- [ ] No stale waypoint is executed without map/version validation.
- [ ] Added obstacles become unsafe immediately; removed obstacles become free only
      after the configured repeated-evidence policy.
- [ ] Appearance-change testing meets documented relocalization recall while retaining
      zero accepted false localizations in the release set.

### M7 — research improvements (ongoing, after the baseline)

Candidate work:

- Foundation-model or deep SLAM: DPVO/Deep Patch SLAM, MASt3R-SLAM, SLAM3R, VGGT-SLAM.
- Learned metric depth as a stereo/multi-view prior, never unqualified free-space truth.
- Active perception that favors texture, parallax, camera field of view, and map
  observability while planning.
- Semantic or hierarchical memory inspired by Hydra/maplab.
- Photorealistic Gaussian mapping for inspection and operator experience.
- Learned local planning or control in shadow mode before bounded activation.

A research component replaces the baseline only if it improves a frozen dataset and
mission suite without weakening worst-case latency, observability, confidence reporting,
replayability, or fault response.

## Immediate prioritized backlog

Unless the user changes priorities, agents should pull work from this order:

1. Document the candidate airframe, PX4 flight controller, stereo camera, command
   link, and independent takeover topology.
2. Scaffold the ROS 2 workspace with `bringup`, `interfaces`, `px4_bridge`,
   `localization`, `mapping`, `planning`, `safety`, and `evaluation` packages.
3. Pin and script PX4 SITL + Gazebo + ROS 2 communications.
4. Add the minimal Offboard proof-of-life/setpoint/failsafe integration test.
5. Define and test coordinate frames, map identity, waypoint, pose-health, and
   autonomy-status interfaces.
6. Build rosbag2/ULog recording and replay before integrating a SLAM implementation.
7. Integrate and benchmark ORB-SLAM3 stereo on recorded/simulated images.

Do not prioritize new detector classes, richer OSD work, Gaussian rendering, learned
control, or real propeller testing ahead of this backlog unless they fix an existing
safety issue or the user explicitly redirects the project.

## Research basis

Primary references behind the current choices:

- [ORB-SLAM3: visual, visual-inertial, and multi-map SLAM](https://arxiv.org/abs/2007.11898)
- [Deep Patch Visual Odometry](https://proceedings.neurips.cc/paper_files/paper/2023/hash/7ac484b0f1a1719ad5be9aa8c8455fbb-Abstract-Conference.html)
- [MASt3R-SLAM](https://arxiv.org/abs/2412.12392)
- [VGGT](https://openaccess.thecvf.com/content/CVPR2025/html/Wang_VGGT_Visual_Geometry_Grounded_Transformer_CVPR_2025_paper.html)
- [SLAM3R](https://arxiv.org/abs/2412.09401)
- [2026 ScaleMaster monocular scale-consistency benchmark](https://arxiv.org/abs/2602.18174)
- [nvblox: GPU-accelerated TSDF/ESDF mapping](https://arxiv.org/abs/2311.00626)
- [Voxblox: incremental ESDFs for MAV planning](https://arxiv.org/abs/1611.03631)
- [Fast-Planner](https://github.com/HKUST-Aerial-Robotics/Fast-Planner)
- [EGO-Planner](https://arxiv.org/abs/2008.08835)
- [Hydra persistent hierarchical spatial perception](https://arxiv.org/abs/2201.13360)
- [maplab 2.0 multi-session mapping](https://arxiv.org/abs/2212.00654)
- [SplaTAM](https://openaccess.thecvf.com/content/CVPR2024/html/Keetha_SplaTAM_Splat_Track__Map_3D_Gaussians_for_Dense_RGB-D_CVPR_2024_paper.html)
- [Learning High-Speed Flight in the Wild](https://arxiv.org/abs/2110.05113)
- [PX4 Offboard mode](https://docs.px4.io/main/en/flight_modes/offboard)
- [PX4 ROS 2/uXRCE-DDS integration](https://docs.px4.io/main/en/middleware/uxrce_dds)

## Research and decision log

Append dated entries when an experiment or new fact changes the roadmap.

| Date | Topic | Result / decision | Evidence |
|---|---|---|---|
| 2026-07-18 | Initial autonomy review | Preserve `elrsctrl` as the manual console; start a separate PX4/ROS 2 autonomy stack using stereo visual localization, explicit collision mapping, and gated simulation-to-flight milestones. | Repository review; references above |
