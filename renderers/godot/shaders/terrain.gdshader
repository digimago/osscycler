// The ground: its land cover from the vertex colours (as the world builder
// made them), with detail on top so a field or a verge doesn't look like
// one flat colour: broad patches (10-20 m) of lusher and drier ground
// everywhere, and fine mottling near the rider, fading out with distance
// (beyond, it would shimmer). Where grass grows (the ground map's window
// round the rider, as the grass shaders read it) the ground itself turns
// grassy, the colour the grass cards average to, streaked: behind and
// beyond the last cards there is grass-coloured ground, not a flat green,
// so the grass fades into the land rather than ending.
shader_type spatial;

uniform sampler2D ground_map : filter_nearest, repeat_disable;
uniform sampler2D clear_map : filter_linear, repeat_disable;
uniform vec2 win_origin;
uniform float win_size = 0.0; // 0: no window yet
uniform float bloom = 0.0;    // the heather in flower (Grass.cs)
uniform float dry_above = 1e9; // grassy ground dries above this height, as the grass on it (ground.gdshaderinc)

varying vec3 wpos;

// field_colour is a sown field's ground (linear): ridge 1 on a ridge or
// row, 0 in the furrow between, 0.5 the average. Ploughed: furrows of
// turned soil, the ridges catching the light; maize: soil between rows of
// green; wheat: straw-coloured, close drilled; potatoes: ridges under
// dark haulm.
vec3 field_colour(int crop, float ridge, vec3 soil) {
	if (crop == 0) {
		return soil * (0.55 + 0.9 * ridge);
	} else if (crop == 1) {
		return mix(soil * 0.9, vec3(0.05, 0.10, 0.02), 0.65 * ridge);
	} else if (crop == 2) {
		return vec3(0.34, 0.27, 0.11) * (0.85 + 0.3 * ridge);
	}
	return mix(soil * (0.6 + 0.4 * ridge), vec3(0.045, 0.09, 0.02), 0.7 * ridge);
}

#include "res://shaders/noise.gdshaderinc"

// bumped is the normal n (view space) at view position v tilted by the
// height h's slopes across the screen (as shaders/surfaces.gdshader).
vec3 bumped(vec3 n, vec3 v, float h) {
	vec3 dx = dFdx(v), dy = dFdy(v);
	float hx = dFdx(h), hy = dFdy(h);
	vec3 r1 = cross(dy, n), r2 = cross(n, dx);
	float det = dot(dx, r1);
	vec3 grad = sign(det) * (hx * r1 + hy * r2);
	return normalize(abs(det) * n - grad);
}

void vertex() {
	wpos = (MODEL_MATRIX * vec4(VERTEX, 1.0)).xyz;
}

void fragment() {
	vec3 base = COLOR.rgb; // linear, as glTF has them
	vec2 p = wpos.xz;
	float d = length(wpos - CAMERA_POSITION_WORLD);
	float broad = patches(p);
	float fine = 0.5 * vnoise(p * 1.4) + 0.3 * vnoise(p * 3.7) + 0.2 * vnoise(p * 9.0);
	float near = 1.0 - smoothstep(12.0, 70.0, d);
	float k = 1.0 + 0.45 * (broad - 0.5) + 0.5 * (fine - 0.5) * near;
	vec3 c = base * k;
	// Drier patches lean yellow, lusher ones blue-green.
	c *= vec3(1.0 + 0.25 * (broad - 0.5), 1.0, 1.0 - 0.2 * (broad - 0.5));

	// Grassy ground: how much grows here (as ground.gdshaderinc's look),
	// fading out towards the window's edge.
	float grassy = 0.0;
	bool dries = false; // grass, meadow, lawns, orchards: the kinds that dry
	vec4 field = vec4(0.0); // a sown field: how much (the window's fade), its crop, across its rows
	vec3 under = vec3(0.13, 0.29, 0.035); // the cards' average, linear
	if (win_size > 0.0) {
		vec2 uv = (p - win_origin) / win_size;
		vec2 edge = min(uv, 1.0 - uv);
		float inside = smoothstep(0.0, 0.12, min(edge.x, edge.y));
		if (inside > 0.0) {
			int g = int(round(texture(ground_map, uv).r * 255.0));
			// A sown field away from a road's edge (the world builder makes
			// its 8 m headland grass; this as ground.gdshaderinc's HEADLAND).
			float head = 0.0;
			if (g >= 16 && g < 48) {
				float a = float((g - 16) % 8) * 3.14159265 / 8.0;
				head = smoothstep(2.0, 2.5, texture(clear_map, uv).r * 25.5);
				field = vec4(inside * head, float((g - 16) / 8), sin(a), cos(a));
			}
			float room = smoothstep(0.05, 0.5, texture(clear_map, uv).r * 25.5);
			float share = g == 1 ? 0.9 : g == 2 ? 0.95 : g == 4 ? 0.5 : g == 6 ? 0.8
				: g == 3 ? 0.6 : g == 7 ? 0.9 : g == 5 ? 0.15 : g == 8 ? 0.75 : g == 9 ? 0.45 : 0.0;
			if (g == 3) {
				under = vec3(0.06, 0.15, 0.02); // ferns
			} else if (g == 8 || g == 9) {
				under = vec3(0.2, 0.23, 0.1); // grey dune turf and moss
			} else if (g == 7) {
				// heather: dark green twigs, purple in flower
				under = mix(vec3(0.07, 0.075, 0.035), vec3(0.24, 0.09, 0.2), 0.7 * bloom);
			}
			grassy = (g >= 16 && g < 48 ? 0.9 * (1.0 - head) : share) * room * inside;
			dries = g == 1 || g == 2 || g == 4 || g == 6;
		}
	}
	// Streaks along no particular way: blades in clumps, seen from above.
	float streak = 0.5 * vnoise(p * vec2(6.0, 2.5)) + 0.5 * vnoise(p * vec2(2.2, 7.0));
	vec3 lawn = under * (0.75 + 0.5 * mix(0.5, streak, near) + 0.3 * (broad - 0.5));
	if (dries) {
		lawn = mix(lawn, lawn * vec3(2.2, 1.2, 2.0), 0.75 * smoothstep(dry_above - 40.0, dry_above + 40.0, wpos.y));
	}
	c = mix(c, lawn, 0.75 * grassy);
	// A sown field near the rider: its furrows or rows on the soil, the
	// ridges lit as ridges (the normal tilted across them), fading to the
	// field's average colour further out (the stripes would shimmer).
	if (field.x > 0.0) {
		int crop = int(field.y);
		vec2 across = field.zw;
		float sp = crop == 2 ? 0.25 : 0.75;
		float q = dot(p, across) / sp;
		float ridge = 0.5 + 0.5 * cos(6.2831853 * q);
		float rows = (1.0 - smoothstep(20.0, 55.0, d)) * field.x;
		vec3 soil = vec3(0.085, 0.062, 0.038) * (0.85 + 0.3 * fine);
		vec3 fc = field_colour(crop, mix(0.5, ridge, rows), soil);
		c = mix(c, fc, field.x);
		if (crop != 2) {
			float slope = -sin(6.2831853 * q) * 0.45 * rows;
			vec3 tilt = (VIEW_MATRIX * vec4(across.x, 0.0, across.y, 0.0)).xyz;
			NORMAL = normalize(NORMAL + tilt * slope);
		}
	}
	// Micro relief the 5 m ground model hasn't (owner, 2026-10-15: a
	// detail surface over the coarse terrain): undulations, tussocks and
	// the odd stone, as bump (lit as relief, no geometry), fading out
	// beyond 60 m; less on fields and lawns, kept off sown rows.
	float relief = 1.0 - smoothstep(25.0, 60.0, d);
	float lumps = 0.6 * vnoise(p * 0.9) + 0.4 * vnoise(p * 2.3);
	float tuft = vnoise(p * 6.0);
	float rough_ground = mix(1.0, 0.5, grassy) * (1.0 - field.x);
	float hgt = (0.10 * lumps + 0.03 * tuft * near) * relief * (0.4 + 0.6 * rough_ground);
	c *= 0.94 + 0.12 * lumps * relief;
	NORMAL = bumped(NORMAL, VERTEX, hgt);
	ALBEDO = c;
	ROUGHNESS = 1.0;
	SPECULAR = 0.2;
}
