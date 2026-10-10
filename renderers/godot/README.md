# osscycler's 3D renderer (Godot spike)

A Godot 4 (C#) renderer for the worlds `osscycler-world` builds: it loads
a world's `world.glb` at runtime and shows the course from the rider's
eyes, placed by distance along the road (`world.json`'s path).

This is a spike. It follows a core's ride over the API (the state stream,
with the API token, to a core on this machine) and shows the world of the
course being ridden; without a core it rides a world at a fixed speed.

## Run

Needs Godot 4.7 with .NET (the "mono" build) and a .NET SDK 8 or later on
`PATH`.

```sh
make world                                   # builds _worlds/posbank
godot --path renderers/godot -- --world "$PWD/_worlds/posbank" --from 6700

# Follow a ride: start one from the TUI, and the renderer shows it.
godot --path renderers/godot -- --core 127.0.0.1:7420 --worlds "$PWD/_worlds"
```

The renderer's own arguments come after `--`:

| Argument | |
|---|---|
| `--core ADDR` | follow the ride on this core; the world is `<worlds>/<course id>` |
| `--worlds DIR` | where the worlds are (default `~/osscycler/worlds`) |
| `--token-file FILE` | the API token (default `$OSSCYCLER_API_TOKEN`, then `~/osscycler/api-token`) |
| `--world DIR` | without `--core`: the world to ride (default `<worlds>/posbank`) |
| `--from M` | without `--core`: start this far along the course |
| `--speed KMH` | without `--core`: riding speed (default 25) |
| `--shot FILE --frames N` | save a PNG after N frames, then quit |
| `--trace FILE` | write the time and distance shown every frame |
| `--fov DEG` | horizontal field of view (default 70, as the TUI's road view) |
| `--relief K` | scale the world's heights (default 1; the HUD's grade stays the real one) |
| `--seed N` | the ride's light and haze (default: today's, so rides vary by day) |
| `--look DEG` | turn the view this far to the right (checking what lies beside the road) |
| `--keys K@N,...` | press these keys at these frames, e.g. `"?@60,x@120"` (checking the head) |
| `--debug` | space marks the spot as flawed: a line in `marks.jsonl` and a screenshot (only while no menu, prompt or editor is open; `space@N` in `--keys`); ctrl+→ / ctrl+← look 100 m further or back along the course (shift: 1 km), ctrl+↓ back to the rider: with a core only the view moves (the ride stays where it is), without one the fixed-speed ride jumps. The renderer isn't counted as a screen then, and quits without asking |
| `--marks DIR` | where the marks go (default `~/osscycler/marks`); the `world-faults` skill's `marks.py` reads them |

## Keys

The renderer is a head of its own, as the TUI (`src/Head/`: keys in,
commands to the core out, tested with the HUD's tests). So far: `?` `h`
F1 the keys for what is on; during a ride `+` / `-` trainer difficulty
in 10 % steps, `x x` abort (on a track: end); in a workout `+` / `-`
intensity, `n` skip, `x x` abort; `x` or enter closes a result; `q`
quits (the core goes on). Without a ride a world rests behind the
dashboard's tiles. The start menu opens at start when nothing is under
way (`m` reopens it): free ride (the numbers, or round a test track),
courses, workouts, activities; `r` the rides (COURSES, HISTORY: race a
ride again, ACTIVITIES: race the course ride in it or save its FIT file
to Downloads, `s` saves), `w` the workouts (Fixed power first: type the
watts; `f` FTP). The profile (asked for while the core lacks weight or
FTP; `p` edits it), manual control (`g` grade, `l` brake level, `+` / `-`
step, `x` back to free riding), `e` end the ride (save, `d d` discard),
spin-down calibration (`c` when the trainer asks, `C` any time: a 10 s
countdown, then pedal up and coast). A suggested value in a prompt is
taken with enter; typing replaces it. `o` arranges the tiles of the
screen on now and which strips show (as the TUI's `o`), `z` switches the
digits between large and medium; kept on this machine in
`~/osscycler/hud.json` (`--layout`). `v` switches the view between behind
the rider (the default) and the rider's eyes; it is a preference in the
rider's profile on the core (also the last step of `p`), in `hud.json`
without a core. In WORKOUTS `n` and `e` open the workout editor (the TUI's
form by keys: ← → between values, `+` / `-` step, type or enter to edit,
`t` type, `a` `c` `d` add, copy, delete, shift+↑/↓ move, `s` save, `esc
esc` discard). With the TUI on the same core either screen can start or
end what the other shows; each closes its own questions that the other
made stale.

## Cyclists

The rider (in the chase view) and the ghost are a cyclist on a road bike
made in code (`src/Avatar/`): `Pose` puts the joints for the rider's
height and the crank angle (legs by two-bone IK from the pedals, arms to
the hoods; tested with the HUD's tests), `Cyclist` builds the bike and
body from Godot's primitive meshes and turns the cranks at the cadence
and the wheels at the speed. They ride the riding line, nose up the
grade, leaning into bends. The ghost is see-through magenta, carried
forward between the core's reports at a speed estimated from them, a
rider's width aside when close, faded near the eyes. `--ghost M` puts a
ghost ahead without a core.

## HUD

What the TUI's ride screen shows, as 2D panels over the view (`src/Hud/`):
the tiles across the top, the TUI's choice and defaults per screen
(dashboard, course ride, workout; `tui/layout.go`), in its colours (the
grade heat scale, power zones, green ahead of the ghost, red behind),
laid out for a 1080-pixel-high screen and scaled to the real one. The
font is Atkinson Hyperlegible Next (`fonts/`, SIL Open Font License 1.1,
`fonts/OFL.txt`), with tabular figures. Formatting, colours, the 5 s
cadence and the tiles are plain C# apart from Godot, tested with

```sh
dotnet test renderers/godot/tests/Hud
```

## Notes

- Godot's runtime glTF loader leaves vertex colours unused (4.7); the
  terrain's land cover is in them, so `World.Dress` turns them on, and
  gives the terrain its own shader (`shaders/terrain.gdshader`: the land
  cover with broad patches and, near the rider, fine mottling).
- Grass, ferns, flowers and heather near the rider are grown on the GPU,
  not stored (`Grass.cs`, `shaders/ground.gdshaderinc`): every instance
  of a MultiMesh finds its own spot from its number in a world-fixed grid
  around the camera, and what grows there from the world's ground map
  (`ground.bin`: land use and clearance per 1 m cell; the clearance keeps
  plants off roads to within centimetres). Layers cross-fade: modelled
  clumps of grass, ferns and heather to 12-14 m, dense cards to 30 m,
  larger and sparser cards to 70 m, wide ones to 120 m. Every layer
  places its plants on one 0.4 m grid (a coarser layer takes one fine
  cell per square, nested), so a plant hands over to the next layer at
  its own spot, fading as a screen door rather than popping in. Plants
  are lit with an up normal on both faces (Godot turns a back face's
  normal over: cards seen from behind went dark) and take the tone of the
  ground patch under them. The cards' pictures are drawn at load (an
  atlas of grass, dense grass, a fern, grass in flower, heather in and
  out of flower); their edges are smoothed by alpha to coverage with the
  project's 4x MSAA. Heather flowers from early August to late
  September by the ride's date (`--seed`'s day of the year); the world's
  heather mounds only come in from 45 m (`shaders/heather_far.gdshader`). The maps
  reach the shaders as textures of a 256 m window round the rider,
  refreshed every 32 m. Posbank Loop: a steady 60 fps through the forest
  at 36 km/h.
- The rider rides the world's riding line (`path.lane_m`), on the side
  `road.keep` says; the ride itself follows the centre line. The view
  faces the line 6 m ahead: aiming 25 m ahead turned it well into every
  bend, away from the way it moved, which looked like drifting. It tilts
  with 15 % of the grade over the next 30 m, as the TUI's road view
  (riders keep their head nearer level than the bike; tilting with the
  aim point had the view looking up a climb), and leans with 30 % of the
  bike's lean in bends (tan = v² / (g r)).
- Between the core's updates (4 a second) the rider is carried forward at
  the ride's speed and corrections blend out over 0.4 s, as in the TUI.
  A correction is measured from where the last report puts the rider at
  that moment, not from the last frame, or every update stalls a frame.
- The API client is generated from `proto/` at build time (Grpc.Tools).
- Godot's camera field of view is vertical by default; the renderer sets a
  horizontal one, as the TUI's (70° vertical was about 102° across on a
  16:9 screen: the world looked flat and far away).
- Haze by distance: clear within 60-140 m, full by 360-460 m (the world
  ends 300 m from the road), the sun's height and direction and the sky's
  tint picked from the seed.
- Vegetation: a MultiMesh per group of `instances.bin`, the templates
  hidden, each group at its plants' middle (draw distances are measured to
  a node's position).
- Godot runs in the project's directory; relative path arguments are
  taken from the directory the command was given in (`$PWD`).
- Map data © OpenStreetMap contributors; elevation from AHN. A world's
  `world.json` lists its credits.
