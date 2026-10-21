// The world builder's heather (a low mound per plant, vertex-coloured in
// flower), seen from beyond the heather grown near the rider
// (grass_clumps, grass_cards): each plant comes in as a screen door over
// fade metres from near, measured to the plant itself. Out of flower it
// is the twigs' olive green.
shader_type spatial;

#include "res://shaders/noise.gdshaderinc"

uniform float near = 45.0;
uniform float fade = 15.0;
uniform float bloom = 0.0;
// Out of flower the near heather (grass_clumps, grass_cards) is olive
// twigs under withered brown spikes; the mounds beyond were the twigs'
// dark olive alone, so the heath went from a warm brown carpet to a band
// of dark blobs at 45 m (owner, 2026-10-12: the distant heather at
// 10.0 km on the Velp loop). The near plants' mix.
const vec3 TWIGS = mix(vec3(0.075, 0.08, 0.035), vec3(0.16, 0.09, 0.06), 0.5);

varying float show;
varying float flower;

void vertex() {
	// MODEL_MATRIX includes the instance's transform: its origin is the plant's.
	vec3 o = (MODEL_MATRIX * vec4(0.0, 0.0, 0.0, 1.0)).xyz;
	show = smoothstep(near, near + fade, distance(o, CAMERA_POSITION_WORLD));
	flower = h21(floor(o.xz * 4.0)) < bloom ? 1.0 : 0.0;
}

void fragment() {
	if (show < dither(FRAGCOORD.xy)) {
		discard;
	}
	// The template's colour (0x7a5a72) shaded lighter on top: keep the shading.
	float shade = COLOR.r / 0.195;
	ALBEDO = mix(TWIGS * shade, COLOR.rgb, flower);
	ROUGHNESS = 1.0;
	SPECULAR = 0.1;
}
