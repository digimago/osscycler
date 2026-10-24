// Surfaces drawn rather than modelled (owner, 2026-10-15: the paved
// gutter, dressed stone, road surfaces, repair patches, drains are
// textures; colour and bump, not geometry, for detail passed in
// seconds): procedural, in world space, on the vertex colour the world
// builder gives (glTF's COLOR_0, linear). Each kind gives a colour and a
// height in metres; the normal is bumped from the height's screen-space
// slopes (surface-gradient bump mapping, Mikkelsen 2010: the meshes carry
// no tangents). By kind:
//   0 road: asphalt grain, worn wheel tracks, tar-sealed cracks (the
//     black lines on the D13B), repair patches with a raised seam;
//   1 stone wall: courses of dressed blocks, each its own tone, mortar
//     set back;
//   2 rock: fractured rock under diamond-mesh netting;
//   3 setts: a paved gutter's domed granite setts;
//   4 grate: a drain's cast-iron bars over a dark pit;
//   5 building: walls with windows as the builder laid them out (UV in
//     bays and storeys, UV2 their count, top and ground; see buildings.go's
//     winLayout), with
//     shutters in a French world (owner, 2026-10-16: La Bresse's houses
//     face the valley with rows of shuttered windows).
//   6 slope paving: grey concrete pavers under a deck (owner, 2026-10-23:
//     a brick texture for some character; concrete, not red clay; no
//     mortar, dry-laid, held by their weight, minimal seams), 20 × 10 cm
//     in stretcher bond, each a shade of its own, hairline seams.
// Detail fades out with distance before it would shimmer.
shader_type spatial;
render_mode cull_back;

#include "res://shaders/noise.gdshaderinc"

uniform int kind = 0;
uniform bool shutters = false; // windows with shutters (a French world)
uniform float bump = 1.0; // the relief's strength
// The material's own colour, for surfaces without vertex colours (the
// road's grey); COLOR is white there.
uniform vec4 albedo_tint : source_color = vec4(1.0);

varying vec3 wpos;
varying vec3 wnrm;

void vertex() {
	wpos = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
	wnrm = normalize((MODEL_MATRIX * vec4(NORMAL, 0.0)).xyz);
}

// inside is 1 within w/2 of a line's middle (d: the distance to it),
// edges smoothed over a pixel.
float inside(float d, float w) {
	float f = fwidth(d) + 1e-4;
	return 1.0 - smoothstep(w * 0.5 - f, w * 0.5 + f, abs(d));
}

// near fades detail of size s out as the pixel grows past it.
float near(vec2 p, float s) {
	vec2 f = fwidth(p);
	return 1.0 - smoothstep(s * 0.3, s * 1.2, max(f.x, f.y));
}

// bumped is the normal n (view space) at view position v tilted by the
// height h's slopes across the screen.
vec3 bumped(vec3 n, vec3 v, float h) {
	vec3 dx = dFdx(v), dy = dFdy(v);
	float hx = dFdx(h), hy = dFdy(h);
	vec3 r1 = cross(dy, n), r2 = cross(n, dx);
	float det = dot(dx, r1);
	vec3 grad = sign(det) * (hx * r1 + hy * r2);
	return normalize(abs(det) * n - grad);
}

void fragment() {
	vec3 base = COLOR.rgb * albedo_tint.rgb;
	vec3 c = base;
	float h = 0.0;
	float rough = 0.85;
	vec2 p = wpos.xz;
	if (kind == 0) {
		float fine = near(p, 0.06);
		float across = UV.x; // across the strip, 0 to 1 (0 where it has no UVs)
		// Aggregate: fine grain (stones standing a millimetre or two
		// proud), broad tone.
		float grain = 0.6 * vnoise(p * 9.0) + 0.4 * vnoise(p * 23.0);
		float broad = vnoise(p * 0.11);
		c *= 0.92 + 0.38 * (grain - 0.5) * fine + 0.22 * (broad - 0.5);
		h += 0.002 * grain * fine;
		// Wheel tracks a little lighter and smoother, a few mm worn down
		// (UV.x runs across the strip, 0 to 1; junctions have none).
		if (across > 0.001 && across < 0.999) {
			float tracks = exp(-pow((across - 0.27) / 0.07, 2.0)) + exp(-pow((across - 0.73) / 0.07, 2.0));
			c *= 1.0 + 0.07 * tracks;
			rough -= 0.08 * tracks;
			h -= 0.004 * tracks;
		}
		// Repair patches: rectangles of newer, darker asphalt a few mm
		// proud, their seam sealed, a few per 100 m; laid along the road
		// (its UVs: across 0-1 over the strip, along one per 5 m), else on
		// the world's grid (junctions).
		vec2 rp = p;
		if (across > 0.001 && across < 0.999) {
			rp = vec2(UV.y * 5.0, UV.x * 6.0);
		}
		vec2 cell = floor(rp / 7.0);
		// Rare and soft (owner's marks, 2026-10-16: patches in rows and
		// outlined crisp read as painted boxes).
		if (h21(cell + 17.3) > 0.94) {
			vec2 o = (vec2(h21(cell + 3.1), h21(cell + 5.7)) * 0.5 + 0.1) * 7.0;
			vec2 size = vec2(1.5 + 3.5 * h21(cell + 9.2), 1.0 + 2.0 * h21(cell + 11.4));
			vec2 q = rp - cell * 7.0 - o;
			float e = min(min(q.x, size.x - q.x), min(q.y, size.y - q.y));
			float in_patch = smoothstep(-0.01, 0.01, e);
			float seam = inside(e, 0.1);
			c = mix(c, base * (0.8 + 0.1 * grain), in_patch);
			c *= 1.0 - 0.25 * seam;
			h += 0.004 * in_patch + 0.002 * seam;
			rough -= 0.15 * seam;
		}
		// Tar-sealed cracks: thin black meandering lines, glossy and
		// raised, where a warped noise field crosses its middle.
		vec2 w = p + 1.8 * vec2(vnoise(p * 0.35), vnoise(p * 0.35 + 9.1));
		float n = vnoise(w * 0.22);
		float tar = inside(n - 0.5, 0.015) * smoothstep(0.62, 0.75, vnoise(p * 0.05 + 3.0)) * near(p, 0.15);
		c = mix(c, vec3(0.02), tar * 0.7);
		rough -= 0.35 * tar;
		h += 0.003 * tar;
	} else if (kind == 1 || kind == 2) {
		// The face's own coordinates: along it (the horizontal axis it runs
		// on most) and up.
		vec2 f = abs(wnrm.x) > abs(wnrm.z) ? vec2(wpos.z, wpos.y) : vec2(wpos.x, wpos.y);
		float fine = near(f, 0.04);
		if (kind == 1) {
			// Courses 0.45 m, blocks 0.6-1.1 m, joints staggered; each
			// block's face rough-dressed, the mortar 2 cm back.
			float row = floor(f.y / 0.45);
			float len = 0.6 + 0.5 * h21(vec2(row, 1.7));
			float x = f.x / len + 0.5 * mod(row, 2.0);
			float blk = floor(x);
			float jy = inside(fract(f.y / 0.45) - 0.5, 1.0 - 0.03 / 0.45);
			float jx = inside(fract(x) - 0.5, 1.0 - 0.03 / len);
			float block = jy * jx;
			float tone = 0.78 + 0.34 * h21(vec2(blk, row));
			float dress = vnoise(f * 7.0 + vec2(blk * 3.1, row * 1.7));
			c = base * tone * (0.9 + 0.2 * dress);
			c = mix(base * 0.45, c, mix(1.0, block, fine));
			h = (0.02 * block + 0.006 * dress * block) * fine;
			rough = 0.95;
		} else {
			// Rock: fractured, lichen-flecked, its relief a hand's depth;
			// the netting's diamonds 15 cm, its wire standing out.
			float rk = 0.55 * vnoise(f * 1.3) + 0.3 * vnoise(f * 4.1) + 0.15 * vnoise(f * 13.0) * fine;
			c = base * (0.7 + 0.6 * rk);
			c = mix(c, vec3(0.16, 0.19, 0.09), smoothstep(0.62, 0.75, vnoise(f * 2.2 + 4.0)) * 0.6);
			vec2 dm = vec2(f.x + f.y, f.x - f.y) / 0.15;
			float mesh = max(inside(fract(dm.x) - 0.5, 0.06), inside(fract(dm.y) - 0.5, 0.06)) * fine;
			c = mix(c, vec3(0.45, 0.47, 0.49), mesh * 0.85);
			rough = 0.9 - 0.2 * mesh;
			h = 0.12 * rk + 0.01 * mesh;
		}
	} else if (kind == 6) {
		// From above on the slopes (at 1:1.5 a paver looks a fifth longer
		// down them), on a vertical plane where steeper, as a wall's
		// courses: projected from above there they stretched into stripes.
		// (Laid on each slope's own plane they faded out up close: its
		// basis turned from pixel to pixel at world coordinates in the
		// thousands of metres.)
		if (abs(wnrm.y) < 0.65) {
			p = abs(wnrm.x) > abs(wnrm.z) ? vec2(wpos.z, wpos.y) : vec2(wpos.x, wpos.y);
		}
		vec2 q = vec2(p.x / 0.2, p.y / 0.1);
		float row = floor(q.y);
		q.x += 0.5 * mod(row, 2.0);
		vec2 s = floor(q);
		vec2 r = fract(q) - 0.5;
		float fine = near(p, 0.03);
		float joint = 1.0 - inside(r.x, 1.0 - 0.003 / 0.2) * inside(r.y, 1.0 - 0.003 / 0.1);
		c = base * (0.9 + 0.16 * h21(s)) * (0.95 + 0.1 * vnoise(p * 11.0));
		c = mix(c, base * 0.62, joint * fine);
		h = 0.002 * (1.0 - joint) * fine;
		rough = 0.95;
	} else if (kind == 3) {
		// Setts 10 cm in rows, grey granite, domed, dark sandy joints.
		vec2 q = p / 0.11;
		vec2 s = floor(q);
		vec2 r = fract(q) - 0.5;
		float fine = near(p, 0.03);
		float joint = 1.0 - inside(r.x, 0.86) * inside(r.y, 0.86);
		float dome = (1.0 - 4.0 * dot(r, r)) * (1.0 - joint);
		c = base * (0.75 + 0.4 * h21(s));
		c = mix(c, base * 0.4, joint * fine);
		h = 0.012 * dome * fine;
		rough = 0.9;
	} else if (kind == 5) {
		rough = 0.9;
		vec2 w = UV;
		// Cladding (the builder adds 10000 to u for timber, 20000 for zinc,
		// 30000 for slate): timber as boards 15 cm wide (horizontal on most
		// houses) with dark
		// gaps, each its own tone, grain along them; zinc and slate as
		// diamonds (owner, 2026-10-16), 30 cm across, each its own tone,
		// their joints dark and set back.
		float clad = floor((w.x + 5000.0) / 10000.0);
		if (w.x > 5000.0) {
			w.x -= clad * 10000.0;
			float fb = near(w, 0.03);
			if (clad == 1.0) {
				// Horizontal boards on 72 % of houses, vertical on the rest
				// (owner, 2026-10-16), by the house's own colour.
				bool across = h21(COLOR.rg * 53.0 + COLOR.b * 7.0) < 0.72;
				vec2 bp = across ? vec2(w.y, w.x) : w; // .x across the boards
				float bw = 0.15;
				float board = floor(bp.x / bw);
				float gap = 1.0 - inside(fract(bp.x / bw) - 0.5, 0.9);
				float tone = 0.8 + 0.35 * h21(vec2(board, 3.7));
				float grain = 0.9 + 0.2 * vnoise(vec2(bp.x * 40.0, bp.y * 1.5 + board * 7.0));
				c = base * mix(1.0, tone * grain, fb);
				c = mix(c, base * 0.35, gap * fb);
				h = (0.012 * (1.0 - gap) - 0.004 * grain) * fb;
				rough = 0.85;
			} else {
				vec2 d = vec2(w.x + w.y, w.x - w.y) / 0.21; // diamonds 30 cm across
				vec2 cellD = floor(d);
				vec2 f = fract(d) - 0.5;
				float joint = 1.0 - inside(f.x, 0.9) * inside(f.y, 0.9);
				float tone = 0.85 + 0.25 * h21(cellD + clad);
				c = base * mix(1.0, tone, fb);
				c = mix(c, base * 0.4, joint * fb);
				h = 0.006 * (1.0 - joint) * fb;
				rough = clad == 2.0 ? 0.45 : 0.6; // zinc a little sheen
			}
		}
		// Windows as the builder laid them out (buildings.go's winLayout):
		// u in bays, window i centred at i + 0.5 for 0 ≤ i < count; rows at
		// 0.25 + 2.8k over the face's reference, sill 0.9 over the floor,
		// 1.3 high, only up to UV2's top and with the sill over its ground.
		// UV2.x packs (door + 1)·100 + count + top / 100.
		float count = mod(floor(UV2.x), 100.0);
		float doorBay = floor(UV2.x / 100.0) - 1.0;
		float maxTop = fract(UV2.x) * 100.0;
		float bay = floor(w.x);
		float k = floor((w.y - 0.25) / 2.8);
		float sill = 0.25 + 2.8 * k + 0.9;
		bool row = w.y >= 0.0 && sill + 1.3 <= maxTop && sill >= UV2.y + 0.4;
		float seed = h21(COLOR.rg * 97.0 + COLOR.b * 13.0);
		// The door: the ground row of its bay, from the floor up 2.15 m,
		// where that row stands at the ground; no window there.
		bool doorHere = doorBay >= 0.0 && bay == doorBay && k == 0.0 && UV2.y < 0.6 && abs(wnrm.y) < 0.3;
		if (doorHere) {
			float cx = fract(w.x);
			vec2 q = vec2((cx - 0.32) / 0.36, (w.y - 0.25) / 2.15);
			float fine = near(w, 0.05);
			float inDoor = step(0.0, q.x) * step(q.x, 1.0) * step(0.0, q.y) * step(q.y, 1.0) * fine;
			if (inDoor > 0.0) {
				vec3 dc = h21(vec2(seed, 21.0)) < 0.5 ? vec3(0.2, 0.12, 0.07) : vec3(0.22, 0.27, 0.24);
				float frame = 1.0 - inside(q.x - 0.5, 0.88) * inside(q.y - 0.5, 0.95);
				// Two panels, the upper with glass.
				float glass = inside(q.x - 0.5, 0.6) * inside(q.y - 0.78, 0.25);
				c = mix(dc, vec3(0.85), frame);
				c = mix(c, vec3(0.05, 0.06, 0.07), glass);
				h = -0.08 * (1.0 - frame);
				rough = mix(0.7, 0.2, glass);
			}
		}
		if (!doorHere && count > 0.0 && bay >= 0.0 && bay < count && row && abs(wnrm.y) < 0.3) {
			float cx = fract(w.x);
			vec2 q = vec2((cx - 0.31) / 0.38, (w.y - sill) / 1.3); // 0..1 inside the window
			float fine = near(w, 0.05);
			float inWin = step(0.0, q.x) * step(q.x, 1.0) * step(0.0, q.y) * step(q.y, 1.0) * fine;
			if (inWin > 0.0) {
				// Glass, dark and a little reflective, in a white frame.
				float frame = 1.0 - inside(q.x - 0.5, 0.86) * inside(q.y - 0.5, 0.88);
				float bar = inside(q.x - 0.5, 0.04);
				c = mix(vec3(0.05, 0.06, 0.07), vec3(0.85), max(frame, bar));
				rough = mix(0.15, 0.6, max(frame, bar));
				h = -0.06 * (1.0 - frame);
			} else if (shutters) {
				// Shutters either side, the house's colour, slatted.
				vec3 sc = h21(vec2(seed, 5.0)) < 0.5 ? vec3(0.22, 0.13, 0.07) : (h21(vec2(seed, 9.0)) < 0.5 ? vec3(0.28, 0.33, 0.27) : vec3(0.55, 0.5, 0.42));
				float side = step(0.12, cx) * step(cx, 0.31) + step(0.69, cx) * step(cx, 0.88);
				float rows = step(0.0, q.y) * step(q.y, 1.0);
				float sh = side * rows * fine;
				float slat = 0.85 + 0.15 * step(0.5, fract(w.y * 12.0));
				c = mix(c, sc * slat, sh);
				h = 0.02 * sh;
			}
		}
	} else if (kind == 4) {
		// Cast iron: bars 3 cm apart (UV.x across), a dark pit between.
		float bars = inside(fract(UV.x * 12.0) - 0.5, 0.5);
		c = mix(vec3(0.01), base, bars);
		h = 0.02 * bars;
		rough = 0.6;
	}
	ALBEDO = c;
	ROUGHNESS = clamp(rough, 0.05, 1.0);
	NORMAL = bumped(NORMAL, VERTEX, h * bump);
}
