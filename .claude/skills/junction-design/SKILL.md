---
name: junction-design
description: Shaping difficult road junctions in the world builder: side roads joining at steep angles or on a grade, roads meeting at different levels, mouths of lanes and service roads. When to model the land (eased banks) and when to build a retaining wall or terrace, how to keep the mesh watertight, and how to check it. Load it with world-faults when a mark is at a junction or a mouth.
---

# Junction design

A junction is where the general machinery (road frames, junction
outlines, the land laid round them, slivers stitched) most often fails
to add up: pieces meet at different heights, at angles the outline
can't follow, with the land squeezed into strips too thin to draw. The
owner's verdict (2026-10-17, the lane off the Rue du Hohneck): a
construction like this is **designed as one piece**, not left to the
pieces to find each other. "Restart everything for this mouth and make
it look good." Watertight is not negotiable.

Reference: the owner's Street View photos and aerial of La Bresse's Rue
du Hohneck (1.34-1.36 km of the Route des Américains), 2026-10-16/17:
a lane climbing beside the main road behind a straight concrete wall on
the sidewalk's outer edge, the wall's top a wedge up, a straight piece,
a wedge down; the strip of sidewalk a rectangle; the lane's mouth square
to the main road, its first metre flat in line with the sidewalk; the
uphill road's mouth asphalt ramping up to the sidewalk's end with no
kerb. CLAUDE.md, "Regional styles", has the rules and numbers.

## Read the configuration first

Before touching code, measure the place (`world-faults` tools:
`raster_at.py`, `near_ways.py`, the renderer's `--camera x,y,z,yaw,pitch`
for a top-down view with pitch -90 and close looks; cross-sections with
`xsec.py` in the scratchpad or `near_point.py --right`). Write down:

1. **Which roads meet, and how**: class (`classRank`), ridden or not
   (`RouteOn`), the angle at the node, whether a road runs *alongside*
   another (within a few metres for more than a few samples) or
   *across* it (a mouth). Owner's licence: **unridden side roads may be
   reshaped for aesthetics** (squared mouths, moved by metres); ridden
   roads keep their line (the sim follows the GPX).
2. **The step between their surfaces** where they lie side by side, from
   the samples' `ele` or the cutter's `highest`:
   - under 0.35 m (`abreastStepM`): one surface, or the land eased
     between them (`gland.blend`, the island rule at the lower level).
   - 0.35-4 m: a **terrace** (`terrace.go`): a retaining wall on the
     lower road's sidewalk edge, level fill behind to the upper road.
   - over 4 m, or the roads cross: a **deck** (`bridges.go`) or a tunnel
     (`heightsStraight` with the tunnel rule); never a wall that tall
     beside a lane.
3. **Grade**: a minor road leaving a major one on a slope stays level
   with the major road's surface (its sidewalk's height past the
   carriageway, ramped as a dropped kerb) until it has crossed the
   major road's band, a metre on, then eases into its own grade
   (`anchorHeights`, `gMouth*`). A mouth that ramps from the kerb line is
   wrong (a 40 % ramp in a lane came from heights smoothed by sample
   count, not distance: `smoothByDistance`).
4. **Angle**: a side road meeting at under 60° (`mouthSquareDeg`) has a
   mouth the outline can't make: square it (`squareMouths`, the French
   style's `squareMouths`): straight out from the node for 4 m, back on
   its own line within 10 m. New points take ids below -2e9
   (`joinNearEnds` uses -1e9 down: a shared id tied a bridge to a stub).

## Land or wall

Model the land when the step is small or the ground model shows a bank
with room for it (a bank of 1:1.5 needs 1.5 m of width per metre of
step): `gland` eases the land from the road's edge over 8 m; sunken
lanes keep their banks; `relief` stays off within the blend.

Build a wall when there is no room: two roads a step apart within
`terraceReachM` (3 m) of each other's edges, a road cut into a slope
rising 2.2 m within 3 m (`riseWall`, the stone retaining wall with
backfill), a bank in town behind a sidewalk (`riseTown`, the low garden
wall). Which wall:

- **Terrace** (`terrace.go`, concrete panels): between a road and a
  higher road alongside. One unit: a rectangle of sidewalk from the kerb
  to the wall (or no sidewalk: the wall at the carriageway's edge), the
  wall on its outer edge, 0.25 m thick, its top the upper surface plus
  a 0.15 m parapet, shaped as a trapezoid: knee high (`terraceEndM`) at
  both ends, rising at `terraceSlope` to the plateau. Level fill behind
  it to the upper road. At each end, where a mouth's asphalt meets the
  slab, an **asphalt filler** (owner, 2026-10-17: filler slabs patch the
  gap between road surfaces and a sidewalk; extend the road with a few
  triangles over the land either side of the in-between sidewalk): the
  road's surface ramps up to the slab's end over `terraceApronM`, as
  wide as slab, wall and fill together; where the road's own sidewalk
  goes on, the slab joins it flush instead. Three stages, because the
  land must know every surface: `markTerraces` on the samples before
  the meshes (band, sidewalk width, the surface behind, the road's own
  sidewalk off), `terracePrims` draws slab, kerb and fillers **with the
  road meshes** (so the land is laid round them, tucked under them and
  clamped by `underRoads`; drawn with the roadside, the land stood 4 cm
  over the slab's edge), `terraceWall` the wall and the fill with the
  roadside, the fill clipped by the cutter like a verge. Never across a
  mouth (a road crossing the sidewalk's line) nor where the surface at
  the face is level with the road.
- **Retaining wall** (`retaining`, dressed stone, battered): a bank
  rising beside a road, fill behind to the slope; square ends in town.
- **Low garden wall** (`riseTown`): only behind a sidewalk; along a lane
  without one the bank is a lawn (a wall and its flat fill made a
  terrace with a grey rim below a house).

What is wrong and must not be built: a wall standing in a road's
asphalt (place it from the *drawn surfaces*, never from a centre line
and a guessed width); a wall across a junction or a mouth; a wall whose
top the road behind pokes through (take the *highest* surface within
reach behind it); a wall ending as a block in mid-air or diving into a
lawn (close the ends, taper to the ends' height); a terrace whose land
fill lies over another road (`overRoad`).

## Watertight

Every piece shares its edges or is tucked under its neighbour:

- A new surface starts *at* the edge it meets (a wall's face 2 cm inside
  the slab's edge, the slab's inner edge on the kerb line, the fill from
  the coping's back), with an end face across every open end (kerb
  faces, wall ends, the slab's ends, the fill's edge down to the land).
- Strips of land narrower than the land's finest cell (0.625 m,
  `gDepth`) cannot be drawn: don't leave any. Sidewalks along a
  junction's span run on until the junction covers their kerb line and
  a frame more, and the cutter trims them to the outline
  (`walksOnSpans`). `stitchSlivers`, `stitchAcross` and `stitchSeams`
  close what is left (wedges from a shared vertex, seams under 0.6 m
  between sidewalk and asphalt); they are a safety net, not a design:
  if a design relies on them, redo the design.
- Anything drawn later than the land (the roadside) that the land must
  go round or under belongs in the road meshes instead, or the land is
  laid as if it weren't there. Fills of land (behind walls, terraces)
  are clipped by the cutter (`t.surfaces.clip`): a sample-based
  overlap test (`overRoad`) misses thin junction triangles.
- Heights: the land ends just under every road surface (`gSinkM`,
  `underRoads`, which probes a land triangle's edges, centre, corners
  and the road corners inside it); the land fill of a wall at the upper
  surface minus 2 cm.
- Check: `osscycler-world` prints holes, land over road, off road, the
  path's height steps; `area_check.py WORLD FROM TO` (the check's cells,
  with `--reach 40` for the full corridor), `raster_at.py` at each
  spot with `--cell 0.05` for hairlines. A junction is done at 0 m² of
  land over road and no hole larger than the land's cell (a few cm² of
  land notch at a rounded corner is the limit, say so).
- Two builds byte-identical (`cmp` the files; sort every map before
  iterating, `TestBuildIsTheSameEveryTime`), the Dutch worlds unchanged
  unless the change is meant for them (`glb_diff.py` against a build of
  the last commit: Posbank Loop graph and classic, figure-8, oval-400).

## Working order

1. Measure (above), photo and aerial beside it. Say what the place is.
2. Decide land / terrace / retaining wall / deck by the step and room.
3. If the mouth's angle or grade is the cause, fix the *line and
   heights* first (`squareMouths`, mouth levelling), rebuild, look.
4. Build the structure as one unit, suppressing what it replaces.
5. Rebuild, `area_check`, rasters at every end and corner, top-down and
   the photos' angles. Repeat until watertight.
6. Tests: the structure on a hand-made course (`graphCourse`, ways with
   a node per point) that fails without the fix; Dutch worlds compared;
   `make check`; versions bumped (`BuilderVersion`, `GraphVersion`).
7. Record the verdict and numbers in CLAUDE.md (`docs:` commit).
