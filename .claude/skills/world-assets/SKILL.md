---
name: world-assets
description: How osscycler makes what the 3D world and renderer draw (terrain details, plants, buildings, street furniture, bridges, avatars, HUD graphics) - all in code, never hand-modelled. Use before making or changing any such asset, in the world builder (Go, internal/world) or the Godot renderer (renderers/godot), and before calling one done.
---

# World assets

The owner does no coding or modelling. Everything drawn in the 3D world is
made by code from data (OpenStreetMap, a ground model, the course) and from
real-world measures, and judged by the owner riding past it. This skill
holds the rules for making such assets; CLAUDE.md holds what exists and
what the owner decided about each one.

## Where an asset belongs

- **World builder** (Go, `internal/world`, map data via `internal/scenery`):
  anything tied to a place: terrain, roads, water, buildings, plants,
  fences, bridges, start lines. Built once per course into `world.glb` +
  `world.json` (+ `instances.bin`, `ground.bin`), the same every build.
- **Renderer** (`renderers/godot`, C# and `.gdshader`): anything every
  course shares or that follows the live ride: avatars, the HUD, grass
  grown on the GPU, sky, sun and haze. No asset files: meshes are built in
  C# at load (`ArrayMesh`/`SurfaceTool`), pictures drawn at load (as the
  grass atlas in `Grass.cs`).
- **TUI** (`internal/tui`, `road.go`, `roadworld.go`): its own half-block
  pixel art; keep its colours and land classes (`scenery.LandMap`) the
  ones the 3D world uses, so the two views agree.
- **Test tracks** get their landscape as made-up map data
  (`scenery.TrackScenery`, seeded), through the same code as real places;
  never special-case a track in the asset code.

## How to make one

- **From data first.** OSM tags where they exist (`height`,
  `building:levels`, `building:colour`, `leaf_type`, `width`, `surface`,
  ...); otherwise guess from kind, size and land use, and say so in a
  comment. Check what the map really has before guessing (an Overpass
  query or the cached `<courses>/.osm` files); Dutch map data, for one,
  has no building heights or roof shapes.
- **Real measures, in metres**, glTF axes (x east, y up, z south of the
  course start). Use the real norm where there is one (CROW for Dutch
  roads, sports-field rules for the oval) and cite it in a comment.
- **Seeded, never random**: `splitmix` (`buildings.go`) over a stable key
  (the OSM ID, the grid cell, the line and distance), so a world is the
  same every build and a change shows only what it changed.
- **Low-poly, vertex-coloured.** Colours as linear floats via
  `srgbLinear(0xRRGGBB)` (`cover.go`) on a white material per kind
  (`gltf.Material`, roughness by kind); no textures yet (the road has UVs
  for later). Reuse the shape helpers before writing new ones: `cone`,
  `blob` (`vegetation.go`), `limb`, `blobAt` (`trees.go`), `box`
  (`bridges.go`), `beam`, `quad` (`fences.go`), `tri` (`road.go`),
  `smoothNormals` (`drape.go`).
- **Many alike → templates and instances.** A hidden node
  `template <kind>` (extras `kind: template`) and places in
  `instances.bin` (`x y z scale yaw`, float32 LE), grouped by kind and
  terrain chunk with a draw distance (`draw_m`). The renderer makes a
  MultiMesh per group at its instances' middle (Godot measures visibility
  ranges to a node's position). Variety comes from a few variants and
  seeded scale and yaw, not from more templates.
- **Grouped by area**: one node per 250 m terrain chunk per kind (extras
  `kind`, `chunk`), one primitive per material. Not a node per object.
- **Smooth in the geometry.** Godot has no tessellation shaders; its LOD
  and visibility ranges only take detail away. Bends get enough segments
  where the rider sees them; the far side gets fewer.
- **Detail where the eye is.** The rider sees from 1.6 m at 20-40 km/h,
  1 m in from the right edge of the road. Detail within ~15 m of the road
  matters; beyond 100 m outlines and colour do. Nothing in the rider's eye
  line that reality wouldn't put there (a pine trunk, a tree through a
  road above).
- **Keep things where they can stand.** Plants, props and grass keep off
  every road surface (carriageway, sidewalk, junction, roundabout, car
  park), buildings and water; use the clearance the ground map already
  computes. Things sit on the ground model (or the road's height), never
  float or sink.
- **Sparse human touches.** The owner wants some sign of people, not
  clutter. Not wanted: zebra crossings, bus stops and shelters, every side
  street, roadside parking strips. Ask before adding a new kind of object.
- **Size budget.** Report the world's size, triangles and build time
  before and after. Normals go as bytes (`KHR_mesh_quantization`);
  positions stay float.

## Outside assets and licences

Prefer code. If an outside asset is needed (a font, a model that code
can't match), only CC0 or OFL without asking; anything else, ask the owner.
Check the licence at the source, put it beside the file, and add a credit
to the manifest's credits if the licence wants one.

Data licences checked 2026-10-08: AHN CC0; OpenStreetMap ODbL (credit
"© OpenStreetMap contributors"; a derived database shared alike if
published); Copernicus GLO-30 free with a fixed notice on (modified)
redistribution ("produced using Copernicus WorldDEM-30 © DLR e.V.
2010-2014 and © Airbus Defence and Space GmbH 2014-2018 provided under
COPERNICUS by the European Union and ESA; all rights reserved") plus a
no-liability sentence. Check any imagery source before using it.

## Checks before calling it done

1. `go test ./internal/world/...`, then `make check` (judge by its exit
   code). Godot side: `dotnet build` in `renderers/godot`.
2. Build the worlds it touches: `make world COURSE=posbank` (builds the binaries first), and
   `figure-8` / `oval-400` when the asset can appear on a test track.
   `osscycler-world` prints its own check (holes, land over road within
   40 m of the path, the riding line off road) and fails past
   `-max-holes` / `-max-land-over-road` / `-max-off-road`. A fault it or
   the owner finds: the `world-faults` skill.
3. Khronos glTF validator on the `.glb`: 0 errors, 0 warnings. npm
   `gltf-validator` in a `node:22-slim` container (no node on the dev
   box).
4. Look at it. Screenshot from the rider's place:
   `godot --path renderers/godot -- --world "$PWD/_worlds/posbank" --from <m> --shot <scratchpad>/x.png --frames 120`
   (`--look DEG` turns the view sideways, `--seed` fixes the light); then
   read the PNG. It opens a Godot window briefly on the desktop (headless
   has no renderer). Take shots at the km marks the owner named, before
   and after. From above: `_scratch/glbshade` (a hillshade of a `.glb`).
5. 60 fps (vsync) on the Posbank Loop, through the forest at 5.3-6 km and
   in Rheden; the HUD shows fps.
6. Report size, triangles, build time and fps against before.

## Godot pitfalls (4.7)

- The runtime glTF loader leaves `COLOR_0` unused: `World.Dress` turns on
  `VertexColorUseAsAlbedo`, with `VertexColorIsSrgb = false` (glTF colours
  are linear). A new material name may need its own case there.
- It decodes `KHR_mesh_quantization` but refuses the file for not knowing
  the name: `Quantization.cs` declares it.
- The importer drops vertices no triangle uses: data meant for the
  renderer (as ground heights) goes in a side file (`ground.bin`), not in
  the mesh.
- The camera's field of view is vertical by default; the renderer sets a
  horizontal one (70°).
- Carrying something forward between the core's 4 Hz updates: measure the
  correction from where the last report puts it now, not from the last
  frame, or it stalls at every update (`--trace` shows it).
- Install on the dev box: Godot 4.7.2 .NET in `~/.local/opt`, `godot` in
  `~/.local/bin` is a wrapper that sets `DOTNET_ROOT` and `PATH` itself;
  .NET SDKs in `~/.dotnet`. Relative paths after `--` resolve from `$PWD`.

## The owner's verdicts

The owner looks at the result on a ride and reports by km mark. Record
each verdict in CLAUDE.md under the step it belongs to, as
"(owner, YYYY-MM-DD: reason)", with the km mark and the numbers before
and after, so the rule survives the next change. Work on a branch from
develop, as for any change.
