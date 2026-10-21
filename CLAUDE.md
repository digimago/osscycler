# Trainer platform: project context

Self-built indoor cycling platform. Priority order: telemetry core first, renderer second, world content last.

This file keeps what steers future work: rules, decisions, the owner's verdicts that still apply, and what is open. How each piece came to be, its measurements, and the faults found and fixed on the way are in [docs/HISTORY.md](docs/HISTORY.md) (the full notes up to 2026-10-20, then dated entries). Look there before reworking something already built: the reasons and the numbers to compare against are there. When work is done, add its detail and numbers to HISTORY.md; put a line here only for a rule, a decision, a verdict that constrains later work, or what is left open.

Items marked VERIFY have not been checked against the spec or the trainer.

## Owner context

- Go developer, Linux-first (PC, Raspberry Pi), macOS second. No Windows builds (owner's decision 2026-10-07).
- Values data ownership and self-hosting: local-first recording, no mandatory cloud, open formats (FIT).
- Rides with real shifters and cassette on a direct-drive trainer, so no virtual shifting is needed.
- Use case: casual free rides, occasional ERG sessions, optional group rides later.
- Does no coding or modelling: worlds are procedural (GPX + ground model + OSM), every asset made in code.

## Hardware under test

- Trainer: Tacx Flux 2 Smart (T2980), over ANT+ FE-C: manufacturer 89, model 2980, firmware 4.503, ANT device 47508, trans type 5. Per retail specs also Bluetooth (FTMS to be confirmed), electromagnetic brake, 7.6 kg flywheel, max 2000 W, max simulated grade 16 %.
  - Pages 0x10 and 0x19 alternate at ~4 Hz with 0x50/0x51 and Tacx 0xF0; decoding checked against live values.
  - Reports "resistance calibration required" and "user config required" until a spin-down and page 0x37 are done. Whether a spin-down survives a power cycle is still to check (spin down, unplug, reconnect, see whether the flag returns). The screens show the request as a quiet hint, not a warning.
  - Control verified: 0x37 clears "user config required"; 0x33 grade, 0x31 ERG (holds within a few W across cadences) and 0x30 basic resistance all work.
  - Page 0x46 requests for 0x50/0x51 are answered; requests for 0x47 command status are ignored in every mode, although the profile requires a reply: confirm setpoints by EVENT_TRANSFER_TX_COMPLETED and observed behaviour.
  - It can't drive the flywheel, so descents can't feel lighter than flat at riding speed (checked 2026-10-10). The sim computes speed from the real grade regardless; descents are sent as grade × 0.5 × difficulty, as Zwift.
  - Spin-down over FE-C works: the trainer alternates 0x02 and 0x10 (no power or cadence meanwhile), ignores the FE-C cancel (acknowledges, keeps calibrating) and gives up exactly 64 s after the request. It reports 0xFFFD where the spec says 0xFFFF for "not reported" (spin-down time, zero offset): `fec` treats ≥ 0xFFFD as not reported. Report the cancel honestly ("ignored the cancel").
  - Rx fail rate 7-15 % on the dev box; try stick placement.
  - It also broadcasts as separate ANT+ power and speed/cadence sensors; not needed, FE-C carries all of it.
- Radio: Tacx ANT+ stick `0fcf:1008` (Dynastream ANTUSB2). On Linux the kernel's `usb_serial_simple` binds it as `/dev/ttyUSB*`; the transport uses that tty raw (`ant.OpenSerial`: no cgo, no libusb). `deploy/udev/99-ant-usb.rules`: group `ant`, `/dev/ttyANT`, `uaccess`. Keep it on a USB extension cable, away from USB 3 ports and other 2.4 GHz radios.
- Heart rate: ANT+ HRM on channel 1 (device type 120, period 8070, RF 57), searching in low priority forever so it never interrupts the trainer channel (message 0x63 accepted by the ANTUSB2).

## Architecture

```text
Trainer, HRM <-> ANT+ stick <-> telemetry service <-> sim core -> renderer (swappable)
                                                     |
                                                  recorder (FIT) -> export (intervals.icu, Garmin)
```

- Core in Go: telemetry, sim, rides, workouts, recorder, export, the API. Runs headless on Linux, also on a Raspberry Pi or a container host with the stick attached. Stable without the stick: it polls for it and never restart-loops.
- Renderers are separate processes over the API (local or LAN): the TUI and the 3D view (Godot 4, C#). Trainer control stays inside the core; the network carries state out and requests in.
- Worlds are built on the client that draws them (`osscycler-world`), never on the core (which may be a Pi by the trainer). The core gives only a course's GPX.
- World package: engine-neutral glTF (`world.glb`) plus a manifest (`world.json`: path, credits, stamp, instances) and side files (`instances.bin`, `ground.bin`). The renderer only loads glTF and follows the state stream. Godot 4 with C# first (stable API, runtime glTF, gRPC); Three.js the fallback; Bevy is out (API churn, Rust).

## What exists, and the rules that come with it

Packages live under `internal/` (names below mean `internal/<name>`); only `gen/` (protobuf bindings) is importable. Detail and numbers: HISTORY.md.

### Core

- `ant`, `fec`, `hrm`: framing, channels, the Linux tty transport, raw `FrameLog`; FE-C and HRM page codecs, checked against the specs.
- `telemetry`: `Hub` conflates (readers get the newest state, never a backlog, never block the producer). Sensors are disabled / searching / connected / lost; readings clear when lost or stale (5 s). `Stick` reopens the stick after an unplug and sums its counters; commands without a stick answer `ErrTrainerUnavailable`. `Switch` puts a simulated rider (`Fake`) in the trainer's place while the rider asks (`SetSimulated`); nothing is recorded while simulated. The fake rider's power follows trainer grade (30 W per %) and ERG targets; difficulty never changes simulated speed (as Zwift).
- `api`: gRPC, schema-first (`proto/osscycler/v1/telemetry.proto` → `gen/`, committed; `go generate ./`). Bearer token on every call (≥ 16 chars, constant-time compare); TLS required beyond loopback on server and client; default `127.0.0.1:7420`, 60 Hz cap per client. Rules about riding (what a ride needs, onboarding, who drives the trainer) live in the core and the API enforces them; renderers only show them. Tests run a real server over bufconn.
- `course`: GPX → course resampled every ≤ 10 m, elevation despiked (running median, Tukey end points) then smoothed over 60 m; `Position` follows the GPX track itself. Laps: a course whose end comes back to its start, or ends on itself elsewhere heading the same way (`lapOnItself`; an out-and-back or a crossing is no lap), is lapped (`DetectLap`, `Lapped`: run-in, lap, 100 m tail; past the end a lap back). Reverses: every course from a GPX or included, the test tracks too, gets `<id>-reverse` after it (`Reversed`, `Forward`, `Opposite`); a reversed track uses the forward track's made-up surroundings; a file named as a reverse wins. Test tracks (`Tracks`: `oval-400`, `figure-8`, at -25.5, -20.0, no map, `Builtin`, `Loop`) and included routes (`Included`: embedded GPX in `course/routes/`, real places, map data fetched) come with every binary; a GPX with their IDs is skipped with a warning.
- `sim`: power → speed (gravity, rolling resistance, drag; CdA 0.32, Crr 0.004, ρ 1.225, drivetrain 97.6 %); drag area scales with height and weight (`CdAFor`, as Zwift); `-cda` forces it. Computed per profile change, never per tick. `TrainerGrade`: climbs × difficulty, descents × 0.5 × difficulty, capped. Default difficulty 50 %. Keep the sim free of renderer and USB dependencies (unit-tested, replayable).
- `ride`: course rides. Armed on start, clock from the first power > 0, finish interpolated at the line. 4 Hz ticks; the grade goes 2 s ahead (owner, 2026-10-10: 1 s felt 1-2 s late on the Flux) when it changes by ≥ 0.1 % or every 5 s; flat after the ride, then the trainer is left alone. Loops and lapped courses: each lap timed at its line, the run-in untimed, the ghost the best lap; while a workout or manual control drives the trainer the loop yields and only moves the rider. Timing lines are timer portals (owner, 2026-10-20): joining a course anywhere else is untimed until one is crossed. `TurnAround` (`r r`, the second `r` within 6 s as its question takes a while to read): the ride so far gets no result, the rider rides on along the other way from the same spot, untimed until a line; refused on a run-in out (owner, 2026-10-20: ending the ride there was a surprise), with a hint to turn round once on the lap. Courses, workouts and manual control exclude each other (except on a loop).
- `workout`, `erg`: blocks as fractions of FTP (Warmup, SteadyState, Ramp, Cooldown, IntervalsT, FreeRide, MaxEffort), the native text format (lossless both ways with `.zwo`; the `.zwo` layout checked against a real file 2026-10-14; sets in real files are plain blocks). `Library`: IDs never build paths (safe pattern), atomic writes. ERG targets = FTP × fraction × intensity; free segments go flat; 15 s grace then pause; cadence aims checked within 5 % on the 5 s cadence.
- `control`: manual ERG (25-1000 W), grade (-10 % to max) or brake level (0-100 %); one driver at a time; sends flat once on release.
- `record`: FIT at 1 Hz to local disk, independent of any renderer (a viewer crash never loses a ride); on by default (`-no-record`). One activity per riding session, auto-pause after 3 s still, saved after 5 min still or on core shutdown (the core waits). `name.fit.part`, flushed every second, fsync every 30 s and at every lap end (a finished effort must survive a crash), sealed to `name.fit`; crashed `.part` files recovered on start. Files mode 600 (heart rate is health data). Course laps are `virtual_activity` with positions (`-record-gps=false` leaves them out), the rest `indoor_cycling`. Finished course rides and laps append to `results.jsonl`; untimed and paused rides never count. `EndActivity`: save or discard, the rider's choice. Our own FIT encoder (no dependency), checked with Garmin's FIT SDK. Exports over the API (`ExportActivity`); only names in the recorder's pattern are opened, and an existing file is never overwritten.
- `history`, ghosts, `replay`: PBs per stretch (start within 1 m, distance within 5 m: an edited GPX starts afresh); the ghost replays the PB's recorded power through the physics, scaled to its time; a paused ride or lap is never a PB, a ghost or raceable. `osscycler-replay` (development only) re-rides recordings; `internal/replay/testdata` is a real fake-rider ride (how to refresh it: HISTORY.md).
- `effort`: summaries (NP, power curve, Zwift's six power zones, five heart rate zones). The profile's `max_hr_bpm` is never filled in for the rider (owner, 2026-10-11: a ride's highest heart rate is no estimate of a maximum); zones describe rides, never medical or training advice.
- `profile`, `rider`: `~/osscycler/profile.json` (atomic, mode 600): weight, FTP, difficulty, bike, height, max HR, `view`, `world_build`, `world_fetch`. Flags given explicitly force a value for that run and are never saved. A broken profile stops the core rather than being overwritten. `StartRide` needs the weight, `StartWorkout` the FTP (`FAILED_PRECONDITION`); onboarding is the stack's, renderers show it.
- `home`: `~/osscycler` (visible on purpose: riders drop GPX and .zwo in; `OSSCYCLER_HOME` moves it): `profile.json`, `api-token`, `courses/`, `workouts/`, `rides/`. Every default points there, so a release runs with no flags. Token precedence: `-token-file`, `$OSSCYCLER_API_TOKEN`, the folder's file. Only the default folder ever takes 0.1.0's XDG files (a test once moved the owner's real profile).
- Pause and heads: `SetPaused` holds rides (clocks stand), workouts and manual control, trainer flat; starting something carries on. `esc` during a ride, workout or manual control always opens the menu: it pauses when not paused (and leaving the menu carries on), and on a ride already paused it leaves the pause as it was (owner, 2026-10-20: now and then `esc` did nothing during a ride). Screens connect as heads; `unattended` pauses what is under way 30 s after the last head went and ends it (saving) 30 min later. `q` off the menu opens the menu; quitting with something under way and no other head asks first.
- `metrics`: OpenTelemetry, pushed over OTLP/HTTP only when `OTEL_EXPORTER_OTLP_*` names an endpoint; observed at export time, nothing on the radio path. Rider data (power, HR, position) never goes into metrics.
- `scenery`: OpenStreetMap along each course from Overpass, one query per 5 km stretch on a fixed tile grid, only for courses being ridden (or built), the rider's stretch first, one fetch at a time, retried 1/5/15/60 min then until ridden again; answers with a `remark` never cached; 429s wait for a slot. Cache `<courses>/.osm/<id>-<stretch>-<hash>.json` (a reverse shares its forward course's). Caching is king (owner, 2026-10-07: the public server is often overloaded). `-osm=false` cache only, `-osm-url` another server (`maps.mail.ru/osm/tools/overpass/api/interpreter` answered when the others failed). The query takes land use, buildings, highways within 30 m (no paths, tracks, footways, cycleways), car parks, city-limit signs, places, and the coastline per degree tile. Credit OSM (ODbL) in the start menu only, never on rides (owner, 2026-10-10; the OSMF guidelines allow a menu screen).
- `dem`: ground models in 1 km tiles, cached in `<courses>/.dem/` and shared between courses, read by our own GeoTIFF reader: the AHN DTM (PDOK WCS, RD New) and France's IGN RGE ALTI (Géoplateforme WMS, Lambert-93, Licence Ouverte 2.0, credited); `-dem auto` tries AHN, then RGE ALTI, then the profile.
- `scenery.BDTopo`: in France, buildings from IGN's BD TOPO (WFS by 0.02° tile, `<courses>/.bdtopo/`, Licence Ouverte, credited) instead of OpenStreetMap's (the fallback): eave and ridge measured by lidar, capped where it saw a tree; a missing tile counts as incomplete.

### Screens

- TUI (`cmd/osscycler-tui`, Bubble Tea v2): dashboard, start menu, pickers (COURSES, HISTORY, ACTIVITIES; WORKOUTS), course ride with road view and look-ahead strip, workouts and the form editor ($EDITOR for text), calibration, profile, manual control, graph and summaries. Keys: `r` rides (during a course ride `r r` turns round), `w` workouts, `g`/`l` grade/level, `x x` aborts, `+`/`-` difficulty or intensity, `p`/space pause, `esc` pause into the menu, `?`/`h`/F1 help, `o` tiles, `z` digit size, `t` graph, `v` road view. Hints drop the least important rather than wrap. Layout in `tui.json` on the TUI's machine; a broken file is reported, never overwritten.
- 3D view (`renderers/godot/`, Godot 4.7.2 .NET, C#): a full head, a replacement for the TUI rather than an extension (owner, 2026-10-09): everything by keyboard, every screen reachable by arrows, enter and esc (so a gamepad or remote can map onto it). The head's logic is plain C# apart from Godot, tested with `dotnet test`; Godot only draws. HUD as 2D overlays (not things in the world), Atkinson Hyperlegible Next (OFL; lacks ★ and ◆, drawn), laid out for 1080 px and scaled. Chase view by default, the view a profile preference. Per head on purpose: tile arrangement, digit size (`hud.json`), notices, double-press confirmations (each key confirms only itself). `--debug` marks flaws with space (`~/osscycler/marks/`), ctrl+arrows jump along the course; `--control PORT` for the MCP tools (below). It builds worlds itself (`WorldSource`, a preview first), queues builds of other courses while idle from the caches only (`world_build`, `world_fetch` in the profile), and covers the view while a ride's world isn't on screen. Measure before optimising (`--stats`, `--hide`); the HUD, not the world, was what cost frames.
- Both heads at once: rides, workouts, profile and trainer control live in the core, so either starts or ends what the other shows, and each closes what the other made stale.
- `osscycler`: one command (finds or starts the core, the 3D view where installed and a display is there, else the TUI; `3d`, `tui`, `core`, `-core HOST:PORT`, `-fake`, `--headless`).
- `make demo`: the full stack with the mock rider and synthetic demo content, `-tour` an autopilot. Demo content stays synthetic or open data (no owner rides, no Zwift workouts).

### World builder

- `osscycler-world` (`make world COURSE=id`, `make world-ab`): `world.Build`/`BuildGraph` → glb + manifest. Two builders side by side (`world/engine.go`): `graph`, the default since 2026-10-12, and `classic`, the fallback (also where the graph builder fails or its world fails the check). The graph builder works road network first, land last (owner, 2026-10-11): junctions, the roads between them, what lies relative to the roads (water, car parks), then the land eased from their edges, seeding last. The aim is a facsimile world for a smooth, continuous ride, not a faithful copy of terrain a GPX may or may not fit. Retire the classic builder once the graph builder has been ridden and beats it on the A/B (latest numbers in HISTORY.md).
- Worlds are byte-identical build to build (`TestBuildIsTheSameEveryTime`; after touching the road network or the cut, two builds and `sha256sum`). Placement is seeded by place (map IDs, positions), never by line numbers or order, so adding a route moves nothing else. Real places are built in a regional frame (`worlds.Anchor`, the nearest whole degree of the start) with the ground model's own heights, so routes over the same roads build the same world there; the sim rides the world's heights where a world has a ground model (results record the profile source). Real places are a stepping stone (owner, 2026-10-09): the aim is a world of our own designed for riding, with one road network per area and routes as paths over it.
- Every build checks itself (`World.Check`: holes, land over road, riding line off road, path height steps, sharpest turn, pace) and fails past the limits (`-max-holes` 10 m², `-max-land-over-road` 1 m², `-max-off-road` 1 m/km, `-max-path-step` 3 m).
- Stamps: a world is current when its manifest's stamp matches the course as loaded and the builder's version (`worlds.BuilderVersion`, `GraphVersion`); bump the version when a change alters output. `-if-stale`, `-if-cached` (never builds a world with missing data stamped current), `-preview`, `-progress`; ready-built worlds (Posbank Loop, test tracks) ship with releases.
- The course's own ways (where a route leaves the fetched roads: cycle paths and footways are never fetched) stay (owner, 2026-10-13): no road-first rerouting of planned routes and no fetching paths; the work goes into how own ways join and cross roads. Recorded rides (timestamps on every point) are followed as ridden; where a planned route can't be ridden as drawn (steps, a road below), the builder may keep it rideable.

### Regional styles

- `world/style.go` (owner, 2026-10-15: the Vosges world looked like a Dutch village in the mountains): a `style` per region, chosen by the ground model that covered the course (AHN → `nl`, RGE ALTI → `fr`, elsewhere Dutch), choosing among shared shapes and placing: trees per land use and altitude band, town and street trees, bushes, avenues, hedges, building colours, paving colours, fences, roadside furniture, far terrain. Species carry a region and a world carries only its region's templates, so a change for one region leaves the others byte-identical: check that with both builders.
- France (judged against the owner's Street View shots of La Bresse, the Route des Américains, the D430 and the summit road):
  - Altitude bands set the tree mix and a canopy no tree passes (the Vosges: oak, beech, spruce and fir below 600 m; beech-fir with planted spruce to 1050; stunted to 1150; above, beech kept low with mountain beech and rowan; on the crest's pastures bushes). A forest tagged needle- or broadleaved is that kind in most of its stands (0.7-0.9) and the other kind in the rest, in parcels; Dutch forests stay pure as tagged.
  - Spruce and fir keep low branches in a stand; trees at a road's level are pruned to 4.5 m clearance over it, trees on a bank keep their branches (owner, 2026-10-15).
  - Roadside (`world/roadside.go`, French only): a guardrail where the land falls 3.5 m within 8 m of the edge (one run along a slope, not where a car park or a road lies beside within 1.5 m of its level); where it rises, a retaining wall of coursed stone that fits the bank behind it, runs on over short gaps and eases into the bank, or above 3.6 m rock with netting; out of town a bank above pasture, fields or heath stays grass unless it rises 4 m within 3 m; markings to the French norm on numbered and wider roads; snow poles above 800 m, bend posts, kilometre markers; nothing of it ever stands on a drawn surface; walls and rock face their road.
  - Sidewalks end square; at a service road's mouth the sidewalk runs on as a dropped kerb; at a side street the junction's sidewalk runs round the corner; no guessed sidewalks below tertiary. Unridden side roads may be reshaped for looks (owner, 2026-10-16: solve it properly, no 'open' leftovers): sharp mouths redrawn square, a lane beside the main road behind a wall drawn as one designed terrace (`world/terrace.go`; the `junction-design` skill has the method).
  - Mountain farms are several low gabled sections set into the slope, not one block (owner, 2026-10-18). Houses pale grey, cream, pale yellow, peach under red-brown or dark grey tiles; thuja hedges; low rendered garden walls with a white railing.
  - Grass dries towards straw above 1150 m (`dryAboveM`). Distant terrain out to 5 km where the route sees it (`style.farM`; none in the Netherlands) under exponential haze at the owner's chosen level (owner, 2026-10-18: some haze on the mountains is prettier than very clear air).
  - Surfaces (road grain, cracks, patches, dressed stone, rock, setts, grates) are drawn by the renderer's procedural shader, not modelled (owner, 2026-10-15); meso relief (hummocks on slopes) in the graph builder.
- The sun is placed by the world's latitude, the ride's day and a local solar time, mostly mid-day with one ride in four in low morning or evening light (owner, 2026-10-15: no sun stands in the north-east in France).

### Owner's verdicts on the world's look (still in force)

- Roads are a given of the landscape; the route only maps the rider onto them. Junctions are things of their own; roundabouts too (small ones crossed straight, island ones ridden round with the traffic; rings wider than their roads). The rider never changes onto a road metres above or below.
- Riding line: as on a closed road, the middle of the rider's half, cutting towards the centre in bends, never across it; the eyes 1 m in from the right edge in eye view. Sharp turns at path crossings are rounded wider where the land allows.
- The camera aims 6 m ahead along the riding line, tilts with 15 % of the grade, leans with 30 % of the bike's lean (capped at 35°).
- Not wanted: zebra crossings, every side street, roadside parking strips, bus stops and shelters; dirt tracks and cycle paths stay out of the map query; motorways are never junctions (later as viaducts and tunnels). Public transport only where it makes the view (railway crossings, a passing train, viaducts), far later.
- Sidewalks as the map tags them, or guessed from built-up land, never where `foot=no`/`use_sidepath`; a sidewalk narrows to a wall at a house rather than stopping.
- Vegetation (Dutch): woods about 250 trees/ha in stands of varied size, with forest and open forms; no conifers in a town's greenery (only woods and gardens); by town water birch, alder, poplar, few willows; street trees maple, ash, rowan, plane, red oak, ginkgo, now and then chestnut or cherry; roadside hedges mostly clipped hornbeam; bushes each their own species; dunes by the sea are sand with marram, sea buckthorn and thicket, not heather; trees lower near the coast (not stacked with poor ground); pollard willows on ditch banks, poplars along minor country roads.
- Fields sown as ploughed, maize, wheat or potatoes (not the real crop), crops never within 8 m of the road (a grass headland); meadows fenced only where there is no ditch, the fence behind any avenue trees (owner, 2026-10-19: trees stand in the verge outside a meadow's fence); field entrances are a dirt strip to a gate 8 m off the road. Ditch water keeps a bank and verge from the road.
- Railings where the map says bridge, a roundabout's bridged ring pieces included (owner, 2026-10-21, La Bresse's roundabout over the Moselotte), not along every edge over water: Dutch quays and canals stay open.
- Courses are ridden both ways: check every reported spot looking back along the course as well (owner, 2026-10-17).
- Grass and heather near the rider grow on the GPU (cards far, modelled clumps near), fading, never popping.
- Atmosphere: haze by distance to hide the corridor's edge; sun, haze and sky seeded by the date.
- Licences: check every data or imagery source before use (AHN, OSM, Copernicus GLO-30 and RGE ALTI checked; `world-assets` skill).

## Decisions so far

- Public on GitHub under GPL-3.0 (owner's decision 2026-10-07). Security reports through GitHub's private vulnerability reporting (SECURITY.md). Actions pinned to commits (update by looking up the new tag's commit). Releases are immutable once published (2026-10-07): the workflow only drafts them (files attached, notes from `notes/<tag>.md`), the owner checks and publishes; a fix after publishing is a new version, never a moved tag. The `check` workflow runs on every push and pull request with no secrets (read-only token, `pull_request`, never `pull_request_target`): lint first, then the race tests and `dotnet test` side by side; the race detector stays on the world packages and the world tests stay serial (owner, 2026-10-14).
- Releases so far: v0.1.0, v0.2.0, v0.2.1 (2026-10-07), v0.3.0 (2026-10-09), v0.4.0 (2026-10-10: the 3D view and world builder, `osscycler` as one command, ready-built worlds). Notes in `notes/`.
- ANT+ FE-C first, BLE FTMS as a second transport behind the same interface.
- State stream at about 60 Hz over gRPC server streaming; control via unary requests. Timestamps from the core's monotonic clock; renderers interpolate.
- The LAN API needs authentication from day one (it can change trainer resistance): bearer token, TLS beyond loopback.
- Operational metrics pushed over OTLP/HTTP only when configured; no pull endpoint (2026-10-07: the core is often off, and scraping would make that `up=0` noise). Traces only when multiplayer brings a second service; logs stay on slog.
- Workouts: our own text format is native; Zwift `.zwo` for import and export, round-trips lossless. Never bundle or redistribute Zwift's workouts or plans; mention Zwift only descriptively ("reads Zwift .zwo files"), no logos or implied affiliation. File formats aren't protected by EU copyright (CJEU C-406/10); no Zwift patent on the format found (2026-10-06, not a formal search; get one before any commercial release).
- Course rides are FIT sub_sport `virtual_activity` (2026-10-07). Uploads to intervals.icu / Garmin Connect come much later; files only for now.
- Container USB: `--device /dev/ttyANT` suffices with the tty transport; avoid `--privileged`.
- Packages: Linux amd64/arm64 `.tar.gz`, `.deb`, `.rpm` (nfpm) with the udev rule, the `ant` group, `osscycler@.service` for a headless Pi, the 3D view and ready-built worlds; a macOS universal `.tar.gz` (unsigned, no 3D view); the GPL text and `THIRD_PARTY_NOTICES.txt` in every archive; no Windows.

## Protocol notes

- FE: device type 17, channel period 8192 (about 4 Hz), RF 57 (2457 MHz).
- Specs: free from <https://developer.garmin.com/ant-program/downloads/> under the ANT+ Adopter Agreement, which forbids redistribution: kept locally in `_antdocs/` (git-ignored). Relevant: FE profile rev 5.0 (D000001231), Common Data Pages rev 3.1 (D00001198), ANT Message Protocol and Usage rev 5.1. The old thisisant pages are behind a Cloudflare block; link developer.garmin.com.
- The network key never goes in the repo (ANT+ Shared Source License): from `$ANT_PLUS_NETWORK_KEY`, `_secrets/ant-network-key`, or built into release binaries with `-ldflags -X main.builtinNetworkKey` (owner's decision 2026-10-07: every ANT+ product ships its key; the agreement, accepted by the owner, exists so adopters can ship). `cmd/osscycler-core/key_test.go` fails if any committed file contains it in any formatting. `ant.NetworkKey` redacts itself when printed; `-key-check` reports the source without printing it. Optional before a commercial release: written confirmation from Garmin's ANT+ support.
- A libusb (`gousb`) transport is only needed for macOS or hosts without the `usb_serial_simple` driver.

## Sim core notes

- Speed from power each tick with rolling resistance, drag and gravity on the current grade; parameters (mass, CdA, Crr, air density) configurable.
- Smooth grade changes before sending them, accounting for the trainer's lag; in ERG handle the trainer's ramp.

## Build order

1. Done: telemetry spike (stick, FE-C pages, grade and ERG, raw logs).
2. Done: sim physics, GPX courses, timed course rides, replays. A hilly course is still to replay against a real Flux ride.
3. Mostly done: state schema, authenticated API, TUI, HRM, calibration, ride and control RPCs.
4. Done: FIT recorder and file export.
5. In progress: the 3D world and renderer (see Next up).
6. BLE FTMS transport.
7. Later: multiplayer (its own server design).

## Next up

1. The 3D world (build step 5), what is left:
   - The graph builder: ride it, then retire the classic one. Path: the Z through the Schaarweg at Rheden station (Posbank Loop 30.6 km, 56°), the own way's centre up to 0.6 m off the swept path in tight bends (13.18, 18.0 km), the cycle tunnel under the N348 (26.8 km, a tunnel the map lacks), junction surfaces sized for the course's sweep. Land: tunnels and motorways under the route as edges with no coupling to the ground.
   - Areas: one road network and land per area built per tile, courses as paths over it (a course's own ways are still sampled by its own distance; courses starting in different degree squares get different frames).
   - The horizon: distant terrain to 5 km is built in France; in the Netherlands none yet (a coarser ground model, AHN 25 m or Copernicus, with the haze to match); land colour beyond the corridor from data (Corine); the polder's flat disc where culled land would show.
   - Outside the Netherlands, within Europe only (owner, 2026-10-08): height sources in order: a national lidar DTM where open (France's RGE ALTI done; to do Flanders, Wallonia, German DGM1, France LiDAR HD, England, Denmark, swissALTI3D; licence per source, and a regional style for each), else the GPX profile with Copernicus GLO-30 for the land, else shaped from the profile. Document the limits for riders when worlds ship: GPX elevation gives the road's shape, not its height; GLO-30 is a surface model at 30 m; without a model hillsides come out level across.
   - Still to do in the world: tunnels (portals), motorway viaducts, railways (not fetched), generated buildings beyond 100 m, nicer plant models, AHN DSM building heights, wadis (`intermittent=yes` water and infiltration basins as dry hollows, shared code), a green sliver where a side road joins a junction (Posbank Loop 1.65 km), the bend bulging where the course leaves a road for its own way (10.07 km), splitter islands kerbed and raised, `courseLine` stepping back up to 1 m past a rounded corner (both builders; a builder version), a notch at the Velp loop's 16.82 km roundabout, height steps on the Grebbeberg course (38.0, 39.0, 89.0 km).
   - Open marks (TODO 16a in HISTORY.md, each reading confirmed with the owner 2026-10-13): junction surfaces as straight road strips up to the junction with only the area between filled (owner's direction; Delaunay flips done, new vertices still to do); Zandvoort 15.25 km sidewalk slabs in the parallel carriageway; the Velp loop's roundabouts at 2.47 and 3.50 km (ring outline, road cuts and sidewalks disagree: island or apron, to decide); Posbank Loop 13.17 km ragged own-way join; Velp loop 2.62 and 5.85 km, where roads part: a raised, kerbed verge with a rounded nose.
   - France, open (owner's marks 2026-10-18 on the Hohneck loop): raised side roads ending in a slab wall (0.10, 0.12, 1.47-1.55, 2.02, 4.42, 4.67 km); the splitter island at 0.10 km (raised kerbed islands, set aside); the summit car park's surface showing the blend as faint bands, its gravel in asphalt grey; for the owner to decide: a 2.5 m wall trigger in the woods too (takes the forest edge's wall at 19.07 km, but cuts the woods' walls from 2,508 to 1,784 m). Still to make: planted roundabout islands, box-pruned limes, onion-domed spires, timber-clad gables, the regraded bank between a hairpin's legs, a truer low-poly spruce, kilometre markers' numbers.
   - To judge by riding: the crop cards (maize first), the dune bushes' look, the reversed courses' worlds.
   - 3D view: a graphics preferences pane (owner, 2026-10-18) for settings that trade looks for GPU time, bush shadows first (on; they cost 0.7 ms GPU on the Dunes course).
   - TUI later: tree density and road width from OSM; place signs in each country's own look; the test tracks' made-up landscapes in the road view.
2. Next trainer session:
   - The figure 8's climb and its descent; a workout on a track (ERG hand-over and back); racing a lap right after a ride; turning round on a course.
   - First real course rides (a hilly ride of the owner's): grade feel, believable speeds and times, then `make replay` on one.
   - Manual control: `l` at a few levels (0x30); `g -5` against `g 0` is answered (no lighter at speed).
   - A weight change mid-session over 0x37, the RX fail rate per channel in the metrics, the stick pulled and replugged during a ride.
   - A course ride (`virtual_activity`) uploaded to Garmin Connect and intervals.icu.
   - The packages and `osscycler@.service` on the Pi.
3. Before a wider release: macOS signing and notarisation, and a libusb transport so the core drives the stick on macOS. Map data at scale (see Open questions).

## TODO (owner's list, in this order unless said otherwise)

- 13. Planned routes and recorded rides: rerouting rules suit planned routes but must not break recorded rides imported as courses (follow those as ridden; timestamps on every point could be the switch). How a carried stretch rides (stairs at 30-50 % for a few metres): walking pace, perhaps.
- 13b. After a one-way route (a planning session of its own): a corridor into a made-up world until a new ride is picked, more world generated from real terrain and roads, or a fixed world of appended real terrain with set intersections.
- 14. Heart rate zones from threshold heart rate (Friel), from a guided 20-minute test or picked out of a ride, as an alternative to the maximum. Descriptive only: no training advice or warnings.
- Faster world builds themselves (the Dunes course 48 s from the caches), and the course picker showing which worlds are ready.
- TUI: power and HR zone colours on the dashboard.
- Workouts: repeat groups of 3+ blocks would only be an editing convenience (real files write sets as plain blocks).
- Recorder: a workout_step / lap per workout segment.
- Ghosts: choose another ghost than the PB (last ride, a friend's file); workout ghosts (power against the last attempt); several ghosts, other riders with multiplayer, kit colours from the profile; ghost cadence from the recording (a proto change).
- HRM: R-R intervals (page 4) for HRV later.
- Courses: GPX without elevation, looked up from a ground model. Zwift GPX exports (Watopia lies in the Pacific, no OSM there): a synthetic scenery could come later.

## Known course data

- `_gpx/` holds the owner's own exported rides (local only). One has a real 14.8 % ramp; its first point is an altimeter glitch despiking removes.
- Included: `posbank` (Posbank Loop, BRouter fastbike with AHN heights; lapped: run-in 2.32 km to the Diepesteeg, lap 27.31 km) and `hohneck-loop` (La Bresse – Hohneck loop, owner, 2026-10-19: a planned route and the French world builder's test run; BRouter with RGE ALTI heights; up the D34 and the Route des Américains, along the Route des Crêtes to the Hohneck, round the summit's car park and back, lapped round La Bresse's second roundabout: run-in 0.66 km, lap 33.94 km, 34.70 km). Making a new included route: waypoints on through roads (one beside the road makes the router go there and back), checked against OSM roads with an Overpass query rather than guessed; `TestIncludedRoutesHaveNoUTurns` fails any turn over 135° within 30 m whose way on runs back over the way in (a hairpin onto another road is fine); the file credits OSM and the height source. Bundled: the Posbank Loop, the Hohneck loop and the test tracks; ready-built worlds only for the Posbank Loop and the test tracks.

## Open questions

- Course rides with GPS on upload: check they come through as indoor/virtual rides on intervals.icu / Garmin Connect. Courses made from the owner's own rides start at home: mind privacy where uploads are public, or ride with `-record-gps=false`.
- Whether the Tacx stick enumerates cleanly under libusb (only for a macOS/libusb transport).
- Whether the grade's look-ahead should depend on the size of the change (2 s is settled).
- How many routes and what content fidelity are realistic; world content is the dominant cost.
- Map data at scale: the public Overpass server's guideline for regular use by an application (all users together) is under ~100 queries and 10 MB a day; one course is ~10 MB once per rider. The public server is fine for now (owner, 2026-10-09); before a wider user base, read OSM from a regional extract (Geofabrik `.osm.pbf`) or run our own Overpass (`-osm-url`).

## Working conventions for Claude Code

- Prefer small, testable packages under `internal/`; only `gen/` is importable. Moving a package out of `internal/` later is non-breaking; moving one in is not.
- Never hardcode the ANT+ network key or any API token.
- Log raw frames behind a debug flag; hardware debugging needs them.
- Ask before adding heavy dependencies. Approved: gRPC + protobuf (buf for codegen), Bubble Tea v2 + Lip Gloss v2, OpenTelemetry metrics (`otel`, `sdk/metric`, `otlpmetrichttp`); in the Godot renderer only: `Grpc.Net.Client`, `Google.Protobuf`, `Grpc.Tools`, and xunit for its tests.
- Build tools run pinned through `go run` (buf, staticcheck, actionlint, nfpm, lipo), so nothing needs installing.
- Directories starting with `_` are local only and git-ignored (`_antdocs/`, `_bin/`, `_logs/`, `_secrets/`, `_gpx/`, `_scratch/` for throwaway programs).
- One feature per commit (owner's preference); docs-only changes get their own `docs:` commit.
- Branches (owner's decision 2026-10-07; DEVELOPMENT.md has the steps): a branch from `develop` and a pull request into `develop` (the default branch); never push to `main`. `main` holds releases only: a pull request from `release/X.Y` or `hotfix/X.Y.Z`, merged with a merge commit, tagged `vX.Y.Z` on that merge, then `develop` brought up to `main`. Rulesets: no deletion or force push on either, `check` required on both, `v*` tags can't move.
- `make check` must pass before a commit or push (gofmt, vet, staticcheck, race tests): judge it by its exit code, never by filtered output (on 2026-10-07 two commits went out with a data race hidden by a `grep`).
- Tests and manual runs never touch the rider's real data: `t.TempDir()`, and for a manual run of the core set both `HOME` and `OSSCYCLER_HOME` to a scratch folder.
- Hardware-free checks: the `-fake` core end to end over the API (throwaway drivers in `_scratch/`), Garmin's FIT SDK (`@garmin/fitsdk`, outside the repo) for FIT files, the glTF validator, Godot screenshots, Docker for packages (Debian, Fedora, real systemd in a privileged Fedora container) and for Prometheus with its OTLP receiver.
- Two world builders live side by side: change one without changing the other's output (the classic builder's worlds byte-identical unless the change is meant for it; shared code such as junctions, plants and the check serves both), say which builder a fault or a number is from, and run `make world-ab COURSE=id` before and after a change to either.
- Designing a junction, a mouth or a terrace that has to look right rather than merely close (the La Bresse lane): load the `junction-design` skill.
- A fault in a built world (holes, roads missing or torn at junctions, land through the road, the rider off the road or hopping roads): load the `world-faults` skill and work in its order, ending with the fix in the builder, a test that fails without it and, where it can catch it, a guard in `World.Check`.
- Making anything the 3D world or renderer draws (plants, buildings, props, bridges, avatars, HUD graphics): load the `world-assets` skill (where an asset belongs, how it is made in code, licences, checks, Godot pitfalls).
- Looking at worlds during a session: the renderer's `--control PORT` and `cmd/osscycler-mcp` (`.mcp.json` as `osscycler-view`; never in release archives): `view_open`, `view_shot`, `view_goto`, `view_state`, `view_stats`, `view_reload`, `view_hide`, `view_keys`, `view_mark`, `view_close`. A look costs well under a second against about 10 s per Godot start. A fresh worktree needs Godot's import once (`godot --headless --path renderers/godot --import`) or the HUD font doesn't load. `--camera x,y,z,yaw,pitch` fixes the camera for top-down views (pitch -90) and close looks. The view tools find Godot as `$OSSCYCLER_GODOT`, else `godot` on the PATH: on a Mac with Homebrew's plain Godot first on the PATH, set `OSSCYCLER_GODOT` to the .NET one.
- README screenshots: the real TUI model fed constructed states, written as ANSI and turned into PNG by our own renderer (`_scratch/shots`, `_scratch/ansipng`; Charm's freeze lacks glyphs).
- The 3D work came into develop as one squashed pull request for 0.4.0; the private `world` branch and repository are kept for history only, don't build on them. From then on 3D work goes like any other: a branch from develop and a pull request.
- CLAUDE.md and docs/HISTORY.md are tracked and ship with the source (owner's decision 2026-10-07: the owner works on several machines; no shame in AI-assisted development, the README says so). Keep both free of personal details: no body weight, no names of the owner's own routes, nothing that places the owner's home (the history was rewritten once to take such things out).
- Keep this file short: done work goes to HISTORY.md with its numbers; here only rules, decisions, verdicts that constrain later work, and what is open.
