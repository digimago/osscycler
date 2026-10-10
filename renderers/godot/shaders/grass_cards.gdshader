// Grass, ferns and flowers as crossed cards with a picture of a clump
// (Grass.cs draws the four pictures, a texture array), placed on the GPU
// (ground.gdshaderinc). Used three times: dense near the rider, larger and
// sparser beyond, wide far out, each handing over to the next at the same
// spots. Edges are smoothed by alpha to coverage (the project has 4x MSAA),
// and thin blades keep their coverage in the smaller mipmaps rather than
// fading out.
shader_type spatial;
render_mode cull_disabled;

#include "res://shaders/ground.gdshaderinc"

uniform sampler2DArray atlas : source_color, filter_linear_mipmap, repeat_disable;
// Wider than tall: a far layer's card shows several clumps side by side.
uniform float stretch = 1.0;
// Near the rider modelled clumps (grass_clumps, cells of clump_k) stand in
// for the cards at their spots: there the card comes in as the clump goes.
uniform int clump_k = 0; // 0: no clumps before this layer
uniform vec3 clump_reach; // grass, ferns, heather (plant_kind)
uniform float clump_fade = 3.0;
uniform vec3 clump_density;

varying vec3 tint;
varying flat int tile;
varying float show;

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
	float w = tall * 1.6 * size * stretch;
	show = 0.0;
	if (here && unit(s.h2, 16u) < dense * density && s.clear >= w * 0.4 / stretch) {
		float inward = s.inward;
		if (clump_k > 0 && taken(s.cell, clump_k)) {
			int kind = plant_kind(t);
			if (clump_grows(s, t, dense, kind, clump_density[kind])) {
				float r = clump_reach[kind];
				inward = min(inward, smoothstep(r - clump_fade, r, s.d));
			}
		}
		show = amount(s.outer, inward, s.keep);
	}
	if (show <= 0.0) {
		VERTEX = vec3(0.0); // nothing here: every vertex in one point
	} else {
		float hgt = tall * size * (0.8 + 0.4 * unit(s.h, 8u)) * (0.75 + 0.5 * unit(s.h3, 0u));
		vec3 v = yawed(vec3(VERTEX.x * w, VERTEX.y * hgt, VERTEX.z * w), unit(s.h2, 8u) * 6.2831853);
		v.x += sway(s.p, TIME) * 0.05 * (1.0 - UV.y) * hgt;
		VERTEX = vec3(s.p.x, s.y - 0.02, s.p.y) + v;
	}
	NORMAL = vec3(0.0, 1.0, 0.0);
	tint = tone(s, col);
	tile = t == 4 && !flowering(s) ? 5 : t;
}

void fragment() {
	if (show < dither(FRAGCOORD.xy)) {
		discard;
	}
	vec2 texels = UV * vec2(textureSize(atlas, 0).xy);
	vec4 c = texture(atlas, vec3(UV, float(tile)));
	// Mipmaps average a blade's alpha away: raise it with the mip level.
	float mip = max(0.0, 0.5 * log2(max(dot(dFdx(texels), dFdx(texels)), dot(dFdy(texels), dFdy(texels)))));
	ALBEDO = c.rgb * tint;
	ALPHA = clamp(c.a * (1.0 + 0.25 * mip), 0.0, 1.0);
	ALPHA_SCISSOR_THRESHOLD = 0.5;
	ALPHA_ANTIALIASING_EDGE = 0.3;
	ALPHA_TEXTURE_COORDINATE = texels;
	NORMAL = up_normal(VIEW_MATRIX);
	ROUGHNESS = 1.0;
	SPECULAR = 0.1;
}
