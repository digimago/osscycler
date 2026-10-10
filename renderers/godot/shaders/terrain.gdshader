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

varying vec3 wpos;

#include "res://shaders/noise.gdshaderinc"

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
	vec3 under = vec3(0.13, 0.29, 0.035); // the cards' average, linear
	if (win_size > 0.0) {
		vec2 uv = (p - win_origin) / win_size;
		vec2 edge = min(uv, 1.0 - uv);
		float inside = smoothstep(0.0, 0.12, min(edge.x, edge.y));
		if (inside > 0.0) {
			int g = int(round(texture(ground_map, uv).r * 255.0));
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
			grassy = share * room * inside;
		}
	}
	// Streaks along no particular way: blades in clumps, seen from above.
	float streak = 0.5 * vnoise(p * vec2(6.0, 2.5)) + 0.5 * vnoise(p * vec2(2.2, 7.0));
	vec3 lawn = under * (0.75 + 0.5 * mix(0.5, streak, near) + 0.3 * (broad - 0.5));
	c = mix(c, lawn, 0.75 * grassy);
	ALBEDO = c;
	ROUGHNESS = 1.0;
	SPECULAR = 0.2;
}
