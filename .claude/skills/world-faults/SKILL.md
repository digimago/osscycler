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
   `~/osscycler/worlds/<id>`, a `.preview/`)? Built by which version
   (`world.json` `stamp`; `builder` says `graph`, or `classic` or nothing
   for a world from before 2026-10-21, when the classic builder was
   retired: rebuild it)?
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
   km mark, and sideways (look 90, look -90). With the `osscycler-view`
   MCP tools: `view_open` (world: the course id or directory, `seed`: the
   mark's) once, `view_shot` with `m` and `look_deg` per look, and after a
   rebuild `view_reload` to compare the same spots; `view_open` with
   `port` attaches to the rider's own view started with
   `osscycler 3d --debug --control 7431`. From outside: `view_survey`
   with the km mark takes the whole set in one call (straight down with
   the way ahead at the top, slanted from behind, right, ahead and left,
   the rider's eyes 30 m before and looking back from 30 m past; a
   magenta post on the spot; saved in `_scratch/survey/<course>-<m>/`,
   ready to name to the owner), `view_camera` one placement (`from_deg`,
   `elev_deg`, `radius_m`; `mode: rider` to go back). Without them, one Godot start
   per shot (about 10 s):
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
   tuned to one place. Keep the owner's earlier verdicts (CLAUDE.md, and in
   full in docs/HISTORY.md under the step they belong to): a fix that
   undoes one is a question for the owner, not a fix.
8. **Make it stay fixed.** In this order:
   - A regression test in `internal/world` (or `internal/scenery`) that
     fails without the fix and passes with it: check by taking the fix
     out for a moment (copy the file to the scratchpad, edit the fix out,
     run, copy it back). Not `git stash`: the stash is shared by every
     worktree of the repository and holds the owner's own work in
     progress (2026-10-11: a failed `stash push` of an untracked file
     followed by `stash pop` applied the owner's old stash instead). Build the smallest
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
    `make check` first); the numbers before and after, with the km mark, go
    in docs/HISTORY.md (a dated entry at its end), and a verdict that sets
    a rule for later work in CLAUDE.md as "(owner, YYYY-MM-DD: reason)",
    in a `docs:` commit; a finding left for later goes under Next up in
    CLAUDE.md with its km mark. Rebuild the world the
    owner rides, or tell them it needs one.

## Symptom → where to look

| What is seen (under_path / check / eye) | Likely stage | First things to check |
|---|---|---|
| MISSING: terrain or verge under the rider, road edges facing each other | a road not drawn | `networkLines` (`minLineM`, short links), `drawable`, `chains`, the 150 m `clipM`, `dropCovered`; is the way in the map data (`near_ways.py`)? |
| MISSING beside the road, centre line fine | the path off its road | `place` (the riding line narrowed onto the carriageway, `gLaneMarginM`), `ridingLine` width, the matcher's off-road states (`matchCourse`), `ownWays`, `dropCoveredStubs` |
| HOLE (nothing drawn) at a road edge or junction | the land's outline | `gland` (graph_terrain.go: `covered`, `gTuckM`, the smallest cell `gDepth`), a junction's free edges, an overlay patch (`shapeOverlay`); `stitchAcross`, `stitchSeams` |
| HOLE in open land | land or water | `gland` (far blocks `merged`), the water sheets (`gWaterSheet`) |
| LAND (grass through asphalt) | the land's heights | `gland.blend` (the shoulder), `underRoads`, a road lower than its ground (`anchorHeights`, `heightsStraight`, `bridgeM`) |
| Torn edge at a junction (gap, slit, step) | patch shape | `networkPatches` → `mergePatches` → `shapeFromNode` (cuts `p0`/`p1` against the road mesh's edges), `roundCorners`, `cutRoads` (`onPatch` samples), apron sizes; overlays stop sidewalks short |
| Torn edge at a roundabout | roundabout | `findRoundabouts`, `absorb`, `fit`, `shape` (roundabout.go) |
| Step or kink in a road's height | heights | `anchorHeights` (nodes share a height, roads corrected node to node), `heightsStraight` (bridges, tunnels), `markAloft`, `markDecks` |
| Bridge over nothing, or a road under a deck missing | decks | `markDecks`, `bridgeM` 2.5 m, ways tagged `bridge` the route runs along (`runsAlong`); railways are not fetched |
| Path height steps (the check's `-max-path-step`, path_check's grade) | path / matching, or heights | a change onto a road above or below (`gDropM` in `matchCourse`), a tunnel or underpass the world doesn't model, the own way's heights (`gOwnSmoothM`) |
| Rider jumps sideways, hops roads, wrong carriageway | path / matching | `matchCourse`'s costs (`wrongWayCost`, `gAgainstCos`, `gMapBonus`, `gLeaveCost`), `hairpins`, `trace` (spurs), `place` (turns swept), `limitDrift` |
| Fault only in some builds | order | map iteration or goroutine order: sort keys; `glb_diff.py` on two builds |
| Fault only with `-dem` | the ground model | `anchorHeights`, `gland.blend` (banks eased to the road), `markAloft` |
| Fault only at a chunk edge | chunk grouping | `roadPrims` (stretches split at chunk edges share a sample), `chunkOrder` (clips share the regional frame's grid) |

## The pipeline

`world.Build` (internal/world/graph.go), the one builder since
2026-10-21 (the classic, terrain-first builder retired; its stages and
A/B numbers in docs/HISTORY.md, "Two world builders"): the network
first, the land last, each stage settled before the next and never
revisited. A fault is caused at the first stage whose output is wrong;
everything after only shows it.

1. Course line and samples: `courseLine` (rounded corners, world/smooth.go),
   `sampleRoad`, `applyRoads` (road.go: widths and surfaces from map
   stretches), `groundOffsets`.
2. The roads: `networkLines` (map ways chained by `chains`, cut to `clipM`
   of the route, short pieces left out unless they link roads at both
   ends), `joinNearEnds` (a dead end pointing at a road within its half
   width + 3 m joins it), `finishRoad`; `findRoundabouts` (roundabout.go),
   `dropCoveredStubs`, `joinAlongside`.
3. `newGnet` (graph_net.go): nodes shared by lines; roundabouts' ways
   across as lines from `gnet.virtual` on (not drawn). A course that can't
   change roads where it should: a node missing (the map splits nowhere
   there) or a ring not found (`ringNodes`).
4. `gnet.anchorHeights` (graph_heights.go): each road the ground model
   along it, smoothed, every node the mean of its roads, each road
   corrected straight node to node; bridges and tunnels straight end to
   end (`heightsStraight`). A step or kink between nodes: the correction.
5. `gnet.matchCourse` → `trace` (graph_match.go): per 5 m step on a line
   or off. Costs are its constants (`gLeaveCost`, `gRankCost`,
   `gRushCost`, `gJitterM`, `gStepReachM`, `gCloseM`/`gFarCost`,
   `gAcrossCost`, `gMapBonus`, `gDropM`: never onto a road above or
   below). To see the states, print `st` at the end of `matchCourse` for
   a stretch of a clip (and `cands[i]` with `acc` for one step: why one
   won). Hops, U-turns, loops round a roundabout: a transition cost;
   stalls: projections stepping back (`gJitterM`). Hairpins (180° in a
   step) and spurs: `gnet.hairpins` (arrival and departure directions for
   a zero-length leg), `gNodeSlackM` (a node a hair past a line's end),
   `gAgainstCos` (no leg or net move against the route's way), `gSpurM`
   (spurs out and back removed in `trace`); dump the states (`st`) and
   `out.pts` round a spot, and the candidates with `acc` and `trans` for
   one step, to see which rule should have bitten. Then `ownWays` (the
   course's own ways where it leaves the roads).
6. Surfaces: junctions `networkPatches` → `mergePatchesApart` →
   `shapePatches` (`shapeFromNode`, else `shapeOverlay`); roundabouts
   absorb nearby junctions and become patches (`absorb`, `fit`, `shape`;
   railings over a bridged ring, `railMesh`); `cutRoads`, `clearPatches`,
   `tidyStrips`, `markPassesAgain`; `markAloft`, `markDecks` (bridges.go);
   terraces (`markTerraces`, terrace.go); `roadPrims` (road meshes per
   chunk, `roadMesh`, and patch meshes, `patchMeshes`); car parks
   (`parkingLots`).
7. `gpathOut.place` (graph.go): the turns of the line through the
   network (a road change at a node, a road's own kink, the step across
   where the course leaves a road for a path beside it) rounded as wide
   as the drawn carriageway allows (`gTurnDevs`, `gTurnClearM`, the
   window kept to the line's own bends by `gChordTolM`; two turns within
   `gTurnLinkM` swept as one across the link when that stays on the
   carriageway; a turn off the roads swept wide, `gTurnFreeClearM`); the
   riding line narrowed onto the drawn carriageway, at and between its
   points (`gLaneMarginM`). A sharp turn per step (the check's `TurnDeg`,
   `path_check.py`): print each turn's `s`, `free`, `cut`, `dev` and its
   arc's `d0`, `d1`, `r` after the narrowing loop, and where the sweep
   first leaves the road, on a clip; an arc of a metre or two means a
   kept point beside the turn (a second point of the same node, a
   neighbouring turn's window ending short, a re-kept bend), a `dev` of
   1 that the sweep leaves the carriageway at every width. Off road right
   after a turn: the sweep; off road beside a straight road: the lane.
8. `gland` (graph_terrain.go): the land laid round the roads' outline
   (quadtree, marching squares over any leaf, `covered` = the road shrunk
   by `gTuckM`; a shore in 1.25 m cells, `gWaterS`, a road's edge in
   0.625 m; far flat 50 m blocks as one leaf, `merged`, `gFarM`,
   `gFarTolM`; after all chunks, `underRoads` puts every land triangle
   near a road under the lowest road surface along its edges), water from
   its outline (`gWaterSheet`, `ditchBanks`), far terrain in France
   (far.go). Holes at a road edge: a sliver between two surfaces narrower
   than a smallest cell (`gDepth`); land over road: `blend`'s shoulder,
   `landHeight`'s clamp, `underRoads` (shared vertices by position: never
   clamp from one chunk's triangles alone); land over water along a
   shore: the shore cell's inside against its corners (`shoreLeaf`),
   `lowater.py`; land over a car park: the car park's own mesh is the
   outline; far land off the model: `gFarTolM`. `area_check.py` finds the
   cells, `raster_at.py` shows the spot, `slivers.py` the triangles.
9. Buildings, plants, fences, the roadside (walls, guardrails; French
   style), the ground map.
10. The manifest's path: `ridingLine`, `limitDrift`, `tailOntoLap`;
    `path.xyz` is the final centre line, `path.lane_m` the riding line's
    offset to its right.

Met so far (owner's marks from 2026-10-11 on, and the A/B against the
classic builder of 2026-10-12):

- The path turning 101° in one step at a road change (25.95 km), 87°
  (20.55 km), zigzags through a staggered junction (26.65 km): the
  rounding in `place` had no room (two points of one node a hair apart;
  the chord across 20 m cutting the road's own kink nearby; a window
  ending a metre short of the next turn) and, off the roads, was
  narrowed to a metre by the carriageway check. The 2.1 m height step at 26.8 km was the course's own heights
  off the roads taken from the 5 m ground model unsmoothed, under a
  viaduct; the route goes through a cycle tunnel there (owner), which
  the map data lacks: the own way runs under bridges drawn on the
  embankment (open).
  Off road in single metres with the riding line on a road's edge: the
  check counts its 0.25 m cell by the cell's centre (`cellcheck.py`
  shows which metres, and what is under the point itself); the lane is
  narrowed until 0.3 m inside the carriageway (`gLaneMarginM`). The
  course's own way under a bridge the map has (a cycle tunnel the map
  lacks): lowered 3 m under it, not hidden by the bridge's carriageway,
  the bridge a deck, the land a metre under the deck's slab; a sample
  under the ground model is never aloft (`markAloft`, shared). Land over
  road in 2-9 cm slivers at a junction's kerb seam or a bank: the
  shoulder in `gland.blend` (the lowest surface within a cell and the
  tuck); `slivers.py WORLD X Z [R]` lists the cells with the land
  triangle's vertices against the road under them.
  A course built from another checkout's caches (the Velp loop's AHN
  tiles and map data live in `~/src/osscycler/_gpx`): `-dem-cache` and
  `-osm-cache` on `osscycler-world`, `-courses` and `-dem` on `clip`.
  Two courses sharing a stretch: `shared.py` finds where one path runs
  on the other's and compares road and land heights and plants there, as
  `TestCoursesSharingARoadBuildTheSameWorldThere` does in code.

- The course's own way stopping a metre short of the road it joins,
  grass between (10.63, 12.27, 13.17 km): `dropCovered` cut away its end
  lying on the road; the graph builder keeps a way's covered ends
  (`dropCoveredStubs(..., keepEnds)`). Its own ways still meet roads by
  overlapping them, 1 cm under, with no junction: the inner side of a
  bend where the course leaves a road for its own way bulges (10.07 km).
- A one-step sideways swerve (14.42 km): one step "off the roads"
  between two on the same road, every road candidate facing backwards.
  `courseLine` (shared) steps back up to a metre just past a rounded
  corner where it dropped points near a junction turn (its distances
  then exceed the chord between kept points), so the heading at a point
  flips; the graph builder takes the route's way over ±10 m
  (`routeWay`).
- A side road standing over the main road as a wall where it meets it
  (30.26 km): no junction, because the two share no node (the road
  really meets a cycle path the map data lacks); `joinNearEnds` joins a
  dead end pointing at another road within its half width + 3 m.
  Check with `near_ways.py` and the cache whether the end node is shared.
- Land standing through a ditch beside the road in wedges (polder ride,
  1.09-1.43 km): water drawn as 5 m cells. Water within 60 m of the
  route with a simple outline is drawn from it (`gWaterSheet`) and is
  part of the outline the land goes round; dams between ditch pieces
  stay land. Profile across the road (top surface every metre) to see
  land over water; list the map's water near a stretch from the cache
  (`natural=water` pieces, their gaps, driveways).
- To see why the matcher chose a state, print `st` and, for one step,
  every candidate with its `acc` and `from` (a temporary print guarded
  by an environment variable); a clip may not reproduce a matching
  fault, since the states upstream differ: then the full build.

## Tools

All in `scripts/`; Python ones read only `world.glb` and `world.json`
(run with `python3 -I`); they need nothing installed.

- `under_path.py WORLD FROM TO [--step M] [--centre] [-v]`
- `near_point.py WORLD D [--r 9] [--right M] [--max 40]`
- `near_ways.py WORLD OSM_GLOB D [--r 40] [--all]`
- `path_check.py WORLD [FROM TO] [--top 8]`
- `glb_diff.py WORLD_A WORLD_B [--max 12]`
- `pathdump.py WORLD FROM TO`: the path's points with the heading change
  per step (compare the two builders' worlds at one km mark).
- `shared.py WORLD_A WORLD_B`: where A's path runs on B's (two courses
  over one road) and how the worlds agree there: road and land heights,
  plants.
- `lowater.py WORLD FROM TO`: per 200 m, the water points within 40 m of
  the path (every 5 m along it) that land stands over, against all.
- `cellcheck.py WORLD FROM TO`: the metres `World.Check` counts off road
  (by its 0.25 m cell's centre) and what is under the riding point.
- `slivers.py WORLD X Z [R]`: the check's cells round a spot where land
  stands over a road surface, with the land triangle's vertices.
- `marks.py [MARKS_JSONL] [--last N] [--around 30] [--world DIR]`: the
  rider's marks with a first look at each; `--world` checks them against
  a rebuild (did the fix take?). A mark's `rider` is the riding line's
  point in the world's frame (matches `glb.World.riding` to millimetres),
  `along_m` the km mark, `stamp` the build it was made in.
- `area_check.py WORLD FROM TO [--reach 40] [--cell 0.25]`: the holes and
  land-over-road cells themselves, clustered, with km, ahead/right and
  x z (the check gives only totals per 200 m).
- `raster_at.py WORLD X Z [--r 2] [--cell 0.1]`: a text raster of the
  surface on top round a spot and the triangles over it.
- `glb.py`: the reader they share (frame, triangles, riding line).
- `clip/`: `go run ./.claude/skills/world-faults/scripts/clip -h`. It is
  outside `./...`, so `make check` vets nothing of it (gofmt still checks
  it); after changing `world.Options` or `scenery`, `go build` it once.
- The builder's own check: printed by every `osscycler-world` build and
  by `clip`; limits `-max-holes` (10 m²), `-max-land-over-road` (1 m²),
  `-max-off-road` (1 m per km of course: current worlds have 0 and 0.1
  since 2026-10-10), `-max-path-step` (3 m: the most the rider's height
  changes between two path points 5 m apart; Grebbeberg 2.64, the rest
  under 0.8 since 2026-10-11); `-check=warn`
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
  in its own command line (a binary's name, a course's file name later in
  the same chain), and kills the chain. Save a background job's PID at
  launch (`( … ) & echo $! > FILE`) and `kill $(cat FILE)`; if a pattern
  search is unavoidable, exclude the shell: `pgrep -f PATTERN | grep -vx $$`.
- Builds are byte-identical only if nothing depends on map iteration
  order: the junction heights' node order and the cutter's order once
  did (2026-10-09, ~200 of 4,746 arrays differed from run to run).
- `-fetch=false` builds from the caches only; a missing stretch is then a
  warning and its scenery is grass, which changes what you're looking at.

## Where things stood (2026-10-21)

What current worlds have, to compare a change against (`osscycler-world`'s
check within 40 m of the path; builder version 72):

- Posbank Loop: holes 0.9 m² (the worst 0.2 at 27.4 km), land over road
  0, riding line off road 0, height steps at most 0.50 m, sharpest turn
  42° per step (26.6 km; the Schaarweg Z at 30.6 km is open).
- Posbank Loop (reverse): holes 0.9 m², land over road 0, off road 1 m
  (5.2 km).
- La Bresse – Hohneck loop: holes 0.5 m², land over road 0, off road 0,
  height steps at most 1.25 m (17.4 km, the summit car park's climb),
  sharpest turn 54°.
- Figure 8 (with its village and river since 2026-10-22, both ways
  round) and oval: holes, land over road and off road 0; the figure 8's
  height steps at most 0.57 m (0.62 reversed), pace 1.05 (the oval's
  turns 8° per step).

Older numbers, and the classic builder's beside them, are in
docs/HISTORY.md.
