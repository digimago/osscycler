// The ground near the rider, for shaders that grow things on it (the
// grass cards and clumps): every instance of a MultiMesh finds its own spot
// from its number, in a grid of cells fixed to the world around the camera
// (so plants stay put as the camera moves through the grid), jittered, on
// the terrain's height; what grows there comes from the ground map. The
// textures are a window around the rider that Grass.cs refreshes as it
// goes; their texel centres lie on the ground map's cell middles.

// The land (Grass.cs's codes: 1 grass, 2 meadow, 3 forest, 4 built-up,
// 5 farmland, 6 orchard, 7 heath), a byte per texel.
uniform sampler2D ground_map : filter_nearest, repeat_disable;
// How far the nearest road edge, wall or car park is (0.1 m steps, so
// 25.5 m at 1.0): interpolated, it puts the edge of the grass where the
// road's edge is.
uniform sampler2D clear_map : filter_linear, repeat_disable;
// The terrain's height (metres, relief applied).
uniform sampler2D height_map : filter_linear, repeat_disable;
uniform vec2 win_origin; // the window's corner, glTF x and z
uniform float win_size;  // its side, metres
uniform vec2 centre;     // the camera, x and z

#include "res://shaders/noise.gdshaderinc"

// The layer: its grid, and the ring round the camera it fills. Every layer
// places its plants on one fine grid (base): a layer of k-cell squares
// takes one of the fine cells in each (picked by the square, nested: the
// 6-cell layer's cell is one the 3-cell layer took), and a plant's look,
// size and yaw come from its fine cell. So where layers cross-fade, a
// plant hands over to the next layer at its own spot (keep's complement:
// one layer's plant goes as the next one's comes), not to another plant
// elsewhere. Each plant fades over a short stretch as a screen door
// (dither) rather than popping in; growing them from the ground instead
// looked like cards folding up.
uniform float base = 0.4; // the fine grid, metres
uniform int k = 1;        // the layer's cell, in fine cells: 1, 2, 3 or 6
uniform int side = 150;   // instances per row of the grid
uniform float inner = 0.0;
uniform float reach = 30.0;
uniform float fade = 5.0;    // thinning out towards reach
uniform float fade_in = 5.0; // thinning in from inner
uniform float size = 1.0;    // scales every plant
uniform float density = 1.0; // scales how many grow
uniform float wind = 1.0;
// How much of the heather is in flower (Grass.cs, by the ride's date):
// each plant flowers or not, so in the weeks either side some do.
uniform float bloom = 0.0;

// A plant's own fade, in the share of the layer's (0 to 1) it spreads over.
const float SOFT = 0.25;

struct Spot {
	vec2 p;      // x, z
	float y;     // ground height
	int ground;
	float d;     // from the camera
	float clear; // metres to the nearest road edge, wall or car park
	float outer; // 1 to 0 towards reach
	float inward; // 0 to 1 from inner
	float keep;  // where in the fades this plant goes
	ivec2 cell;  // its fine cell
	uint h;
	uint h2;
	uint h3;
};

uint mix_hash(uint x) {
	x ^= x >> 16u;
	x *= 0x7feb352du;
	x ^= x >> 15u;
	x *= 0x846ca68bu;
	x ^= x >> 16u;
	return x;
}

float unit(uint h, uint shift) {
	return float((h >> shift) & 0xffffu) / 65535.0;
}

// pick is the fine cell square c of n × n cells takes, as an offset.
ivec2 pick(ivec2 c, uint tag, int n) {
	uint h = mix_hash(uint(c.x) * 73856093u ^ uint(c.y) * 19349663u ^ tag * 83492791u);
	return ivec2(int(h % uint(n)), int((h >> 16u) % uint(n)));
}

// fine_cell is the fine cell a layer of kk-cell squares takes in square c.
ivec2 fine_cell(ivec2 c, int kk) {
	if (kk == 6) { // within the 3-cell layer's
		c = c * 2 + pick(c, 6u, 2);
		kk = 3;
	}
	if (kk > 1) {
		c = c * kk + pick(c, uint(kk), kk);
	}
	return c;
}

// taken tells whether the layer of kk-cell squares takes fine cell f.
bool taken(ivec2 f, int kk) {
	ivec2 c = ivec2(floor(vec2(f) / float(kk)));
	return fine_cell(c, kk) == f;
}

// amount is how much of the plant shows (0 to 1) for the layer's fades.
float amount(float outer, float inward, float keep) {
	float q = 0.5 * SOFT + keep * (1.0 - SOFT);
	return min(clamp(0.5 + (outer - q) / SOFT, 0.0, 1.0), clamp(0.5 + (inward - (1.0 - q)) / SOFT, 0.0, 1.0));
}

// spot finds instance id's place; false if it is outside the window or the
// layer's ring.
bool spot(int id, out Spot s) {
	float spacing = base * float(k);
	ivec2 c = ivec2(id % side, id / side) - ivec2(side / 2);
	ivec2 sq = ivec2(floor(centre / spacing)) + c;
	s.cell = fine_cell(sq, k);
	s.h = mix_hash(uint(s.cell.x) * 73856093u ^ uint(s.cell.y) * 19349663u);
	s.h2 = mix_hash(s.h + 1u);
	s.h3 = mix_hash(s.h + 2u);
	s.keep = unit(mix_hash(s.h + 3u), 0u);
	s.p = (vec2(s.cell) + vec2(unit(s.h, 0u), unit(s.h, 16u))) * base;
	s.d = distance(s.p, centre);
	vec2 uv = (s.p - win_origin) / win_size;
	if (any(lessThan(uv, vec2(0.0))) || any(greaterThan(uv, vec2(1.0))) || s.d < inner || s.d > reach) {
		return false;
	}
	s.ground = int(round(texture(ground_map, uv).r * 255.0));
	s.clear = texture(clear_map, uv).r * 25.5;
	s.y = texture(height_map, uv).r;
	s.outer = smoothstep(reach, reach - fade, s.d);
	s.inward = inner <= 0.0 ? 1.0 : smoothstep(inner, inner + fade_in, s.d);
	return true;
}

// What grows on each ground: how much (the share of spots), how tall, which
// card (0 grass, 1 dense grass, 2 fern, 3 grass in flower, 4 heather in
// flower, 5 heather out of flower), its tint. r
// picks between kinds. Forest undergrowth is mostly ferns (owner,
// 2026-10-09: the Posbank's slopes above the Diepesteeg).
void look(int ground, float r, out float dense, out float tall, out int tile, out vec3 col) {
	dense = 0.0;
	tall = 0.35;
	tile = 0;
	col = vec3(1.0);
	if (ground == 1) {        // grass, verges
		dense = 0.9; tall = 0.35; tile = r < 0.5 ? 0 : 1;
	} else if (ground == 2) { // meadow
		dense = 0.95; tall = 0.45; tile = r < 0.15 ? 3 : 1;
	} else if (ground == 3) { // forest floor: ferns, some grass
		dense = 0.6; tile = r < 0.75 ? 2 : 0; tall = tile == 2 ? 0.7 : 0.3; col = vec3(0.9);
	} else if (ground == 4) { // built-up: lawns, mown
		dense = 0.6; tall = 0.16; tile = 1;
	} else if (ground == 5) { // farmland: its edges
		dense = 0.15; tall = 0.45; col = vec3(1.1, 1.05, 0.7);
	} else if (ground == 6) { // orchard
		dense = 0.8; tall = 0.3; tile = 1;
	} else if (ground == 7) { // heath: heather (Calluna), tufts of grass between
		dense = 0.9;
		if (r < 0.85) {
			tile = 4; tall = 0.32;
		} else {
			tall = 0.3; col = vec3(1.15, 1.0, 0.6);
		}
	}
}

// sway is how far the top of a plant at p leans in the wind now, in gusts
// that move across.
float sway(vec2 p, float time) {
	return wind * (sin(time * 1.6 + p.x * 0.23 + p.y * 0.19) + 0.5 * sin(time * 3.7 + p.x * 0.9));
}

// yawed turns v about the vertical by a.
vec3 yawed(vec3 v, float a) {
	return vec3(cos(a) * v.x - sin(a) * v.z, v.y, sin(a) * v.x + cos(a) * v.z);
}

// A modelled clump's room: it needs this much clearance (at size 1).
const float CLUMP_ROOM = 0.35;

// plant_kind is the clump layer that models card t's plants: 0 grass, 1
// ferns, 2 heather.
int plant_kind(int t) {
	return t == 2 ? 1 : t >= 4 ? 2 : 0;
}

// flowering tells whether the heather at s is in flower.
bool flowering(Spot s) {
	return unit(mix_hash(s.h + 4u), 0u) < bloom;
}

// clump_grows tells whether the clump layer of kind (plant_kind) and share
// dens grows a plant at s, of card t and share dense.
bool clump_grows(Spot s, int t, float dense, int kind, float dens) {
	return plant_kind(t) == kind && unit(s.h2, 16u) < dense * dens && s.clear >= CLUMP_ROOM;
}

// up_normal is up in view space: plants are lit as the ground they stand
// on, from either side (Godot turns a back face's normal over, which lit
// cards seen from behind from below, dark among their neighbours).
vec3 up_normal(mat4 view) {
	return normalize((view * vec4(0.0, 1.0, 0.0, 0.0)).xyz);
}

// tone is a plant's tint at s: its kind's colour, the ground patch's tone
// (as the grassy ground under it), a little of its own.
vec3 tone(Spot s, vec3 col) {
	return col * lawn_tone(s.p) * (0.92 + 0.16 * unit(s.h3, 16u));
}
