// Clumps of grass, ferns or heather modelled in 3D (Grass.cs's meshes, coloured in
// their vertices), near the rider, where cards would look flat: placed on
// the GPU as the cards are (ground.gdshaderinc), where the ground's look
// says that kind grows; further out the near cards take over at the same
// spots.
shader_type spatial;
render_mode cull_disabled;

#include "res://shaders/ground.gdshaderinc"

uniform int kind = 0; // this layer's clumps: grass, ferns, heather (plant_kind)
// Heather's flower spikes (vertex alpha 1) out of flower: withered.
const vec3 WITHERED = vec3(0.16, 0.09, 0.06);

varying vec3 tint;
varying float show;
varying float wither;

void vertex() {
	Spot s;
	float dense = 0.0;
	float tall = 0.0;
	int t = 0;
	vec3 col = vec3(1.0);
	bool here = spot(INSTANCE_ID, s);
	if (here) {
		look(s.ground, unit(s.h2, 0u), dense, tall, t, col);
	}
	show = here && clump_grows(s, t, dense, kind, density) ? amount(s.outer, s.inward, s.keep) : 0.0;
	wither = kind == 2 && here && !flowering(s) ? 1.0 : 0.0;
	if (show <= 0.0) {
		VERTEX = vec3(0.0);
	} else {
		float kk = size * (0.8 + 0.4 * unit(s.h, 8u));
		vec3 v = yawed(VERTEX * kk, unit(s.h2, 8u) * 6.2831853);
		v.x += sway(s.p, TIME) * 0.04 * VERTEX.y * kk;
		VERTEX = vec3(s.p.x, s.y - 0.03, s.p.y) + v;
	}
	NORMAL = vec3(0.0, 1.0, 0.0);
	tint = tone(s, col);
}

void fragment() {
	if (show < dither(FRAGCOORD.xy)) {
		discard;
	}
	ALBEDO = mix(COLOR.rgb, WITHERED, wither * COLOR.a) * tint;
	NORMAL = up_normal(VIEW_MATRIX);
	ROUGHNESS = 1.0;
	SPECULAR = 0.1;
}
