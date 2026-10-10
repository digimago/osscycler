---
name: world-faults
description: Finding, fixing and guarding against faults in osscycler's built 3D worlds - holes in the terrain, roads missing or torn at junctions and roundabouts, land through the road, steps, and the rider's path leaving the road or hopping between roads - in the world builder (Go, internal/world; map data in internal/scenery). Use when a rider or a check reports such a fault (usually by km mark), when osscycler-world's check fails, and before changing road, junction, cut, verge or path code. Ends with the fix in the builder, a test that fails without it, a guard in the builder's own check where one can catch it, and before/after numbers.
---

# World faults

A fault in a world is found by measuring, located by reproducing it small,
fixed where it arises in the builder, and kept fixed by a test and, where
it can be, by the builder's own check (`World.Check`), so the next rebuild
or the next course catches it before a rider does. Load `world-assets` as
well when the fix makes or changes something drawn (rules for assets,
screenshots, the validator, Godot pitfalls).

Work on a branch from develop, as for any change.
Never touch the rider's data: `~/osscycler` is real (profile, rides,
worlds); build into `_scratch/` or `_worlds/`, and say so before replacing
a world in `_worlds/` the owner rides.

## The order, and why

Cheap and decisive first; read code only once the fault is reproduced
small; fix at the stage that causes it, then make it stay fixed.

1. **Pin the world.** Marks first: the rider can mark flawed spots while
   riding (renderer with `--debug`: `osscycler 3d --debug`, or `godot
   --path renderers/godot -- ... --debug`; space marks, while no menu,
   prompt or editor is open; ctrl+→ / ctrl+← move the view 100 m along
   the course first, shift 1 km, ctrl+↓ back, so a stretch can be looked
   over and marked without riding to it). `scripts/marks.py` lists them from
   `~/osscycler/marks/marks.jsonl` (`--last N`): course, km along it,
   lat/lon, the screenshot (read it: it shows what the rider saw), whether
   the world was rebuilt since, what is under the rider there and the
   non-road runs within 30 m, and the commands for the next steps. A mark
   is the rider's, not a verdict: what is wrong there is still to find.
   Without marks: which world did the rider see (`_worlds/<id>`,
   `~/osscycler/worlds/<id>`, a `.preview/`)? Built by which builder?
   A fault in a world from an older builder may already be fixed:
   rebuild with the current one from the caches first
   (`_bin/osscycler-world -courses _gpx -out _scratch/w -fetch=false
   -check=warn <id>`; `make build` before) and look again. km marks from
   the owner are the HUD's distance along the course (`world.json`
   path index × `step_m`).
2. **Measure before looking.** The build prints its check (holes, land
   over road within 40 m of the path, the riding line off road). Then, for
   the stretch named:
   - `scripts/under_path.py WORLD FROM TO` (riding line; `--centre` for
     the path's own line): road / MISSING / HOLE / LAND / OTHER per metre.
   - `scripts/path_check.py WORLD [FROM TO]`: the worst turns, zigzags,
     spacing, lane changes and grade steps of the rider's path.
   Classify the symptom with the table below before forming a theory.
3. **Look.** A screenshot from the rider's place, before and after the
   km mark, and sideways (`--look 90`, `--look -90`):
   `~/.local/bin/godot --path renderers/godot -- --world "$PWD/_worlds/<id>" --from <m> --shot <scratchpad>/x.png --frames 150 --seed 1`
   (`~/.local/bin` isn't on the PATH of a tool shell; the world path has
   to be absolute; quote IDs with spaces). Then `scripts/near_point.py
   WORLD D`: the surfaces over the spot and their vertices as (ahead,
   right, up), which shows two road ends facing each other, a step, or a
   surface that stops short.
4. **Ask the map.** `scripts/near_ways.py WORLD "<courses>/.osm/<id>-*.json" D`:
   every road near the spot with its tags and end nodes. Decide whether
   the fault is in the data (not fetched: cycle paths, footways, tracks
   and railways never are; or mapped so) or in the builder (the way is
   there, the world doesn't draw it or draws it wrong). Data faults get
   a rule in the builder only when the pattern is general (`use_sidepath`
   roads, roundabouts), never a special case for one place.
5. **Reproduce small.** `go run ./.claude/skills/world-faults/scripts/clip
   -courses _gpx -course <id> -to <m past the fault> -out _scratch/clip
   [-dem _gpx/.dem]`: seconds instead of minutes. Any `-from` works:
   the clip is built in the course's regional frame (`worlds.Anchor`), so
   its 250 m chunk grid and positions compare one to one with the full
   world (test tracks keep their own frame: clip those from 0). Run it with and without `-dem`: a fault only with
   the ground model lies in the cut (cut terrain, verges, the cutter); one
   in both lies in the network, junctions or path. Confirm with
   `under_path.py` on the clip that the fault is there before going on.
6. **Find the stage.** Follow the pipeline below to the first stage whose
   output is already wrong. A temporary print guarded by an environment
   variable (`if os.Getenv("DBG_X") != "" { ... }`, positions relative to
   the spot from `near_point.py`) in the stage's function, run on the
   clip, shows what it produced there: the lines (way, samples, `onPatch`,
   `underPatch`, `again`, `aloft`), the patches (nodes, cuts, overlay),
   the path. Remove it before committing (`git diff` shows it).
7. **Fix at the cause.** Change the rule at the stage that is wrong, not
   a later stage to cover for it (no patching over a missing road with a
   bigger junction). Prefer a rule that names what the world is (a short
   road with roads at both ends is a link, and drawn) over a threshold
   tuned to one place. Keep the owner's earlier verdicts (CLAUDE.md, under
   the step they belong to): a fix that undoes one is a question for the
   owner, not a fix.
8. **Make it stay fixed.** In this order:
   - A regression test in `internal/world` (or `internal/scenery`) that
     fails without the fix and passes with it: check by stashing the fix
     (`git stash push <file>`, run, `git stash pop`). Build the smallest
     input that shows it: hand-made `scenery.Way`s (see
     `TestShortLinksAreDrawn`, `TestChainsJoinARoadSplitByTheMap`),
     `testTerrain`, a course from `course.New`; with a ground model
     (`Options.Elevation`, a function of lat, lon) when the cut matters.
   - A guard in the builder's own check when the fault is of a kind that
     can come back from other data: `World.Check` (internal/world/check.go)
     measures holes, land over road and the riding line off road; a new
     kind of fault that can be seen from above or along the path belongs
     there, with its own limit flag in `cmd/osscycler-world` (as
     `-max-off-road`), a line in its printed check, and a case in
     `TestCheck` that injects the fault. Set the limit above what current
     worlds have (measure the Posbank Loop, the test tracks and the course
     at hand) so builds don't start failing, and below what the fault
     costs; say what the worlds have now.
   - An invariant in the code where a stage can know it is wrong (a road
     line with cuts at both ends and nothing drawn between them, a patch
     whose outline misses a road): fall back to a safe shape, as
     `shapePatches` falls back to an overlay, rather than draw nothing.
   - `TestBuildIsTheSameEveryTime` stays green: nothing in a fix may
     depend on map iteration order (sort keys first) or goroutine timing.
9. **Verify wide.** Marks that led here: `marks.py --world <rebuild>`
   shows each now on a road (or what is left). `go test ./internal/world/... ./internal/worlds/...`,
   then `make check` (judge it by its exit code). Rebuild and compare,
   before and after, the course at hand, the Posbank Loop and the test
   tracks (`make world COURSE=posbank`, `figure-8`, `oval-400`): the
   check's three numbers, size, triangles, build time. Two builds of one
   course must be byte-identical (`sha256sum` the files, or
   `scripts/glb_diff.py A B`). `under_path.py` over the whole course for
   new MISSING/HOLE runs. Screenshots at the fault and at places the fix
   could touch elsewhere (junctions, roundabouts, bridges). The glTF
   validator and 60 fps when geometry changed (`world-assets`).
10. **Record.** Commit the fix with its test (one feature per commit,
    `make check` first); the owner's verdict and the numbers before and
    after go in CLAUDE.md under the step they belong to, as "(owner,
    YYYY-MM-DD: reason)" with the km mark, in a `docs:` commit; a finding
    left for later goes under TODO with its km mark. Rebuild the world the
    owner rides, or tell them it needs one.

## Symptom → where to look

| What is seen (under_path / check / eye) | Likely stage | First things to check |
|---|---|---|
| MISSING: terrain or verge under the rider, road edges facing each other | a road not drawn | `networkLines` (`minLineM`, short links), `drawable`, `chains`, the 150 m `clipM`, `dropCovered`; is the way in the map data (`near_ways.py`)? |
| MISSING beside the road, centre line fine | the path off its road | `keepOnRoad` (`keepM` 10 m), `ridingLine` width, `courseRoads` (own way vs map road), `ownWays`, `continueDeadEnds` |
| HOLE (nothing drawn) at a road edge or junction | the cell cut | `rasterize` (footprint), verges (`verger`, `roadGap`), a junction's free edges, an overlay patch (`shapeOverlay`) over a cut terrain |
| HOLE in open land | terrain or water | `terrain.chunk`, `lod` blocks, the water quads and bed |
| LAND (grass through asphalt) | the cutter or heights | `newCutter`/`clip` (which materials cut: not `ground`), verges not clipped, a road lower than its ground (`heights`, `joinHeights`, `bridgeM`) |
| Torn edge at a junction (gap, slit, step) | patch shape | `networkPatches` → `mergePatches` → `shapeFromNode` (cuts `p0`/`p1` against the road mesh's edges), `roundCorners`, `cutRoads` (`onPatch` samples), apron sizes; overlays stop sidewalks short |
| Torn edge at a roundabout | roundabout | `findRoundabouts`, `absorb`, `fit`, `shape` (roundabout.go) |
| Step or kink in a road's height | heights | `heights`, `joinHeights` (nodes share a height, blended 15 m), `markAloft`, `markDecks` |
| Bridge over nothing, or a road under a deck missing | decks | `markDecks`, `bridgeM` 2.5 m, ways tagged `bridge` the route runs along (`runsAlong`); railways are not fetched |
| Rider jumps sideways, hops roads, wrong carriageway | path / matching | `courseRoads` (`wrongWay`, `against`, `use_sidepath` reach), `smoothRuns`, `connectors` (`openRunM` 18 m), `smoothTurns`, `rideAround`/`passThrough`, `limitDrift`, `evenPace` |
| Fault only in some builds | order | map iteration or goroutine order: sort keys; `glb_diff.py` on two builds |
| Fault only with `-dem` | the cut | everything after `t.cut = true` in `Build`: `markAloft`, verges, `rasterize`, the cell cut in `terrain.chunk`, the cutter |
| Fault only at a chunk edge | chunk grouping | `roadPrims` (stretches split at chunk edges share a sample), `chunkOrder` (clips share the regional frame's grid) |

## The pipeline

`world.Build` (internal/world/world.go), in order. A fault is caused at
the first stage whose output is wrong; everything after only shows it.

1. Course line and samples: `courseLine` (rounded corners, world/smooth.go),
   `sampleRoad`, `applyRoads` (road.go: widths and surfaces from map
   stretches), `groundOffsets` (the ground model's offset to the profile).
2. The network (network.go), `newNetwork`: `networkLines` (map ways
   chained by `chains`, cut to `clipM` of the route, short pieces left out
   unless they link roads at both ends), `finishRoad`, `courseRoads` (the
   map road the course is on, per sample: the map matching's input),
   `heights` (ground model along each road, smoothed), `joinHeights`.
3. `findRoundabouts` (roundabout.go), `dropCovered`, `joinAlongside`.
4. `network.finish` → `path` (the rider's line: Viterbi map matching onto
   the drawn roads), `ownWays` (the course's own ways where it leaves the
   roads), `connectors` (over open ground past `openRunM`),
   `continueDeadEnds`.
5. Junctions: `networkPatches` (map roads only; route lines get none) →
   `mergePatches` → `shapePatches` (`shapeFromNode`, else `shapeOverlay`);
   roundabouts absorb nearby junctions and become patches (`absorb`,
   `fit`, `shape`).
6. `cutRoads` (junction.go: a sample at every cut, samples inside a span
   `onPatch`, roads crossing an overlay `underPatch`), `clearPatches`,
   `tidyStrips`, `markPassesAgain` (road.go: a stretch ridden twice drawn
   once).
7. With a ground model: `t.cut = true`, `markAloft` (terrain.go); then
   `markDecks` (bridges.go).
8. `roadPrims` (world.go): road meshes per chunk (`roadMesh`, road.go;
   verges built with them by `verger`, verges.go) and patch meshes
   (`patchMeshes`); `rasterize` (verges.go) records their footprint.
9. Terrain chunks (`terrain.chunk`, terrain.go: level of detail, water;
   with a cut, cells the footprint covers whole are left out); car parks
   (`parkingLots`); then, with a ground model, the cutter (verges.go): car parks and road
   surfaces (not the `ground` material) are `add`ed, and every terrain primitive and every ground-material one in
   the roads (the verges) is `clip`ped by them, each triangle's cutters
   taken in the order of their coordinates (they arrive in map order).
10. Buildings, plants, fences, the ground map.
11. The manifest's path: `smoothTurns`, `ridingLine`, `keepOnRoad`,
    `rideAround`, `passThrough`, flat roundabouts' lane 0, `limitDrift`,
    `evenPace`, then `settleOnRoad` (settle.go: against the drawn road triangles, sideways only) and `limitDrift` again (riding.go, network.go, roundabout.go). `path.xyz` is the
    final centre line, `path.lane_m` the riding line's offset to its right.

## Tools

All in `scripts/`; Python ones read only `world.glb` and `world.json`
(run with `python3 -I`); they need nothing installed.

- `under_path.py WORLD FROM TO [--step M] [--centre] [-v]`
- `near_point.py WORLD D [--r 9] [--right M] [--max 40]`
- `near_ways.py WORLD OSM_GLOB D [--r 40] [--all]`
- `path_check.py WORLD [FROM TO] [--top 8]`
- `glb_diff.py WORLD_A WORLD_B [--max 12]`
- `marks.py [MARKS_JSONL] [--last N] [--around 30] [--world DIR]`: the
  rider's marks with a first look at each; `--world` checks them against
  a rebuild (did the fix take?). A mark's `rider` is the riding line's
  point in the world's frame (matches `glb.World.riding` to millimetres),
  `along_m` the km mark, `stamp` the build it was made in.
- `glb.py`: the reader they share (frame, triangles, riding line).
- `clip/`: `go run ./.claude/skills/world-faults/scripts/clip -h`. It is
  outside `./...`, so `make check` vets nothing of it (gofmt still checks
  it); after changing `world.Options` or `scenery`, `go build` it once.
- The builder's own check: printed by every `osscycler-world` build and
  by `clip`; limits `-max-holes` (10 m²), `-max-land-over-road` (1 m²),
  `-max-off-road` (1 m per km of course: current worlds have 0 and 0.1
  since 2026-10-10); `-check=warn`
  keeps a world past them (the renderer's on-demand builds use it).

Frames: world positions are x east, y up, z south of `world.json`'s
`origin` (the regional anchor, `worlds.Anchor`: the nearest whole degree
of the course's start; a test track's own start): x = (lon − lon0)·k·cos(lat0),
z = −(lat − lat0)·k, k = 6371000·π/180 (`course.Project`). The scripts
convert both ways (`glb.World.to_xz`, `to_latlon`). The world builder's
own code works in east and north (n = −z).

## Pitfalls met before

- A triangle with no area seen from above (walls, skirts, triangles
  welded to a point) covers nothing; a point-in-triangle test that
  accepts it reports land over road where there is none.
- Verges are `terrain`-material triangles inside the `roads <e> <n>`
  nodes; the terrain itself is in `terrain <e> <n>`. Both count as land.
- `World.Check` only looks within 40 m of the path, and only for what it
  measures: a whole terrain where a road should be passes holes and land
  over road (hence the off-road measure along the riding line).
- The rider rides `lane_m` right of the path: a check on the centre line
  alone misses a riding line on the verge (the Posbank Loop at 5.78 km).
- Clips and worlds share the course's regional frame (2026-10-10), except
  the test tracks: clip those from 0 m.
- Godot runs the C# assembly last built: `dotnet build
  renderers/godot/OsscyclerGodot.sln` (`~/.dotnet/dotnet`) after changing
  the renderer, or the run shows the old code.
- `pkill -f <pattern>` matches the shell running it when the pattern is
  in its own command line; stop a test core by its port or PID.
- Builds are byte-identical only if nothing depends on map iteration
  order: the junction heights' node order and the cutter's order once
  did (2026-10-09, ~200 of 4,746 arrays differed from run to run).
- `-fetch=false` builds from the caches only; a missing stretch is then a
  warning and its scenery is grass, which changes what you're looking at.

## Where things stood (2026-10-10)

What current worlds have, to compare a change against (check within 40 m;
riding line off road):

- Posbank Loop: holes 5.7 m², land over road 0.1 m² (1.0 km), off road
  0 m since 2026-10-10 (`settleOnRoad`, `keepToConnected`, `openRuns`; was about 110 m in 16) (`under_path.py` over the whole course), the
  longest 5.779-5.810 km (31 m), then 7.065, 7.139 and 13.174 km (8-9 m
  each); open. At 5.79 km (a course's own way: a cycle path the map data
  lacks) the path runs about 1.5 m right of the drawn road's middle (the
  road, 3.5 m wide, ends 0.23 m right of the path), so the riding line,
  0.73 m right of the path, lies on the verge; the other
  places are not yet looked at. `path_check.py`: worst turn 66° and worst
  zigzag 2.84 m, both at 18.17 km.
- Figure 8 and the oval: 0, 0, 0.
- Grebbeberg to Rotterdam (98.7 km): holes 31.4 m², land over road
  0.7 m², off road 8 m (0.1 m/km) since 2026-10-10 (was 1,150 m: a canal
  with a road on each bank at 55-62 km, which the match hopped between,
  and short gaps). `under_path.py` also reports OTHER where the road
  passes under buildings or a bridge's structure (97.2, 98.2 km, the
  Erasmus Bridge): the check counts those as on the road.
