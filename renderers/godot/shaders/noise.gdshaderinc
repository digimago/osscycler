// Value noise shared by the terrain and what grows on it, so a plant takes
// the tone of the ground it stands on.

float h21(vec2 p) {
	uvec2 q = uvec2(ivec2(p) + ivec2(65536));
	uint h = q.x * 1597334677u ^ q.y * 3812015801u;
	h = (h ^ (h >> 16u)) * 0x7feb352du;
	h ^= h >> 15u;
	return float(h & 0xffffu) / 65535.0;
}

float vnoise(vec2 p) {
	vec2 i = floor(p);
	vec2 f = fract(p);
	vec2 u = f * f * (3.0 - 2.0 * f);
	return mix(mix(h21(i), h21(i + vec2(1.0, 0.0)), u.x),
		mix(h21(i + vec2(0.0, 1.0)), h21(i + vec2(1.0, 1.0)), u.x), u.y);
}

// patches is the terrain's patches of lusher and drier ground (10-20 m).
float patches(vec2 p) {
	return 0.6 * vnoise(p * 0.06) + 0.4 * vnoise(p * 0.17);
}

// lawn_tone is how the grassy ground at p is lit and tinted by its patch
// (as terrain.gdshader's lawn): drier leans yellow, lusher blue-green.
vec3 lawn_tone(vec2 p) {
	float b = patches(p) - 0.5;
	return (1.0 + 0.3 * b) * vec3(1.0 + 0.25 * b, 1.0, 1.0 - 0.2 * b);
}

// dither is a 4x4 ordered threshold per pixel (0 to 1), for fading an
// opaque thing in and out as a screen door (as Godot's distance fade).
float dither(vec2 frag) {
	ivec2 q = ivec2(frag) & ivec2(3);
	int i = q.x + 4 * q.y;
	const float m[16] = float[](0.0, 8.0, 2.0, 10.0, 12.0, 4.0, 14.0, 6.0, 3.0, 11.0, 1.0, 9.0, 15.0, 7.0, 13.0, 5.0);
	return (m[i] + 0.5) / 16.0;
}
