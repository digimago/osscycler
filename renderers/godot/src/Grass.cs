using System;
using Godot;

namespace Osscycler;

// Grass grows grass, ferns and flowers near the rider, placed on the GPU
// (shaders/ground.gdshaderinc) in layers that cross-fade: modelled clumps
// of grass and of ferns close by, where cards would look flat; dense cards
// to 30 m; larger, sparser cards to 70 m; wide cards of several clumps to
// 120 m; and under them all, grassy ground (the terrain shader reads the
// same maps). Each layer is one MultiMesh
// whose instances find their own spots in a world-fixed grid around the
// camera. What grows where and how high the ground is reach the shaders
// as textures of a window around the rider (WindowM square: the land, its
// clearance, the height; texel centres on the ground map's cell middles,
// so the clearance interpolates as the world builder measured it), filled
// from the ground map and refreshed when the rider has gone RefreshM from
// its middle. The cards' pictures are drawn here, at load: grass, dense
// grass, a fern, grass in flower, heather in and out of flower. Heather
// flowers by the ride's date (Bloom).
public sealed class Grass
{
    const float WindowM = 384;
    const float RefreshM = 32;
    const float HeightCellM = 2;

    // The fine grid every layer places its plants on (ground.gdshaderinc).
    const float BaseM = 0.4f;

    // A layer: its shader, the mesh each instance draws, its cells (in fine
    // cells: 1, 2, 3 or 6, nested so layers hand plants over at the same
    // spots), its ring and the fades at either side of it.
    sealed record Layer(string Shader, Mesh Mesh, int K, float Inner, float Reach, float Fade,
        float Size, float Density, int Kind = 0, float Stretch = 1, float FadeIn = 0);

    readonly GroundMap _map;
    readonly float _relief, _groundCell;
    readonly ShaderMaterial[] _materials;
    readonly ShaderMaterial _terrain;
    readonly Image _groundImg, _clearImg, _heightImg;
    readonly ImageTexture _groundTex, _clearTex, _heightTex;
    readonly byte[] _codes; // the ground map's values as the shaders' codes
    readonly byte[] _groundBuf, _clearBuf, _heightBuf;
    Vector2? _window;       // the window's middle, x and z

    public Node3D Node { get; } = new() { Name = "grass" };

    // Bloom is how much of the heather (Calluna) is in flower on day of the
    // year doy: in the Netherlands from early August to late September,
    // coming on over the last week of July, the last going by mid-October.
    public static float Bloom(int doy)
    {
        const int start = 205, full = 220, fading = 265, over = 290; // 24 Jul, 8 Aug, 22 Sep, 17 Oct
        if (doy <= start || doy >= over)
            return 0;
        if (doy < full)
            return (doy - start) / (float)(full - start);
        if (doy <= fading)
            return 1;
        return (over - doy) / (float)(over - fading);
    }

    public Grass(GroundMap map, float relief, ShaderMaterial terrain, float bloom = 0)
    {
        _terrain = terrain;
        _terrain.SetShaderParameter("bloom", bloom);
        _map = map;
        _relief = relief;
        _groundCell = map.CellM;
        // The shaders' codes: 1 grass, 2 meadow, 3 forest, 4 built-up,
        // 5 farmland, 6 orchard, 7 heath; anything else grows nothing.
        _codes = new byte[256];
        for (int i = 0; i < map.Classes.Length && i < 256; i++)
            _codes[i] = map.Classes[i] switch
            {
                "grass" => 1,
                "meadow" => 2,
                "forest" => 3,
                "built" => 4,
                "farmland" => 5,
                "orchard" => 6,
                "heath" => 7,
                _ => 0,
            };
        int gn = (int)(WindowM / _groundCell), hn = (int)(WindowM / HeightCellM);
        _groundBuf = new byte[gn * gn];
        _clearBuf = new byte[gn * gn];
        _heightBuf = new byte[4 * hn * hn];
        _groundImg = Image.CreateFromData(gn, gn, false, Image.Format.R8, _groundBuf);
        _clearImg = Image.CreateFromData(gn, gn, false, Image.Format.R8, _clearBuf);
        _heightImg = Image.CreateFromData(hn, hn, false, Image.Format.Rf, _heightBuf);
        _groundTex = ImageTexture.CreateFromImage(_groundImg);
        _clearTex = ImageTexture.CreateFromImage(_clearImg);
        _heightTex = ImageTexture.CreateFromImage(_heightImg);
        _terrain.SetShaderParameter("ground_map", _groundTex);
        _terrain.SetShaderParameter("clear_map", _clearTex);

        var atlas = Atlas();
        var cards = Cards();
        // Each ring's inner fade is the one before's outer fade, over the
        // same metres: a plant goes from one layer to the next at its spot.
        const float clumpFade = 3;
        var clumpReach = new Vector3(12, 14, 12);      // grass, ferns, heather
        var clumpDensity = new Vector3(0.28f, 0.43f, 0.7f);
        Layer[] layers =
        [
            new("grass_clumps", Clump(blades: 9, height: 0.3f, width: 0.04f, spread: 0.18f, lean: 0.3f,
                new Color(0.07f, 0.17f, 0.02f), new Color(0.30f, 0.55f, 0.10f)), 2, 0, clumpReach.X, clumpFade, 1, clumpDensity.X),
            new("grass_clumps", Clump(blades: 7, height: 0.6f, width: 0.13f, spread: 0.05f, lean: 1.0f,
                new Color(0.05f, 0.14f, 0.02f), new Color(0.16f, 0.40f, 0.06f)), 2, 0, clumpReach.Y, clumpFade, 1, clumpDensity.Y, Kind: 1),
            new("grass_clumps", Heather(), 2, 0, clumpReach.Z, clumpFade, 1, clumpDensity.Z, Kind: 2),
            new("grass_cards", cards, 1, 0, 30, 6, 1, 1),
            new("grass_cards", cards, 3, 24, 70, 8, 1.8f, 0.8f, FadeIn: 6),
            // Far: wide, low cards of several clumps each, thinning out
            // onto the grassy ground.
            new("grass_cards", cards, 6, 62, 120, 15, 1.6f, 0.7f, Stretch: 3, FadeIn: 8),
        ];
        _materials = new ShaderMaterial[layers.Length];
        for (int k = 0; k < layers.Length; k++)
        {
            var l = layers[k];
            int side = (int)Math.Ceiling(2 * l.Reach / (BaseM * l.K));
            var mat = new ShaderMaterial { Shader = GD.Load<Shader>($"res://shaders/{l.Shader}.gdshader") };
            mat.SetShaderParameter("ground_map", _groundTex);
            mat.SetShaderParameter("clear_map", _clearTex);
            mat.SetShaderParameter("height_map", _heightTex);
            mat.SetShaderParameter("win_size", WindowM);
            mat.SetShaderParameter("base", BaseM);
            mat.SetShaderParameter("k", l.K);
            mat.SetShaderParameter("side", side);
            mat.SetShaderParameter("inner", l.Inner);
            mat.SetShaderParameter("reach", l.Reach);
            mat.SetShaderParameter("fade", l.Fade);
            mat.SetShaderParameter("fade_in", l.FadeIn);
            mat.SetShaderParameter("size", l.Size);
            mat.SetShaderParameter("density", l.Density);
            mat.SetShaderParameter("stretch", l.Stretch);
            if (l.Shader == "grass_cards")
            {
                mat.SetShaderParameter("atlas", atlas);
                if (l.K == 1)
                {
                    // The clumps stand in for these cards near the rider.
                    mat.SetShaderParameter("clump_k", 2);
                    mat.SetShaderParameter("clump_reach", clumpReach);
                    mat.SetShaderParameter("clump_fade", clumpFade);
                    mat.SetShaderParameter("clump_density", clumpDensity);
                }
            }
            else
                mat.SetShaderParameter("kind", l.Kind);
            mat.SetShaderParameter("bloom", bloom);
            _materials[k] = mat;

            var mm = new MultiMesh
            {
                TransformFormat = MultiMesh.TransformFormatEnum.Transform3D,
                Mesh = l.Mesh,
                InstanceCount = side * side,
            };
            for (int i = 0; i < mm.InstanceCount; i++)
                mm.SetInstanceTransform(i, Transform3D.Identity);
            Node.AddChild(new MultiMeshInstance3D
            {
                Name = $"{l.Shader} {k}",
                Multimesh = mm,
                MaterialOverride = mat,
                CastShadow = GeometryInstance3D.ShadowCastingSetting.Off,
                // The shader moves every plant: the mesh's own bounds mean nothing.
                CustomAabb = new Aabb(new Vector3(-1e5f, -1e4f, -1e5f), new Vector3(2e5f, 2e4f, 2e5f)),
            });
        }
    }

    // Update follows the camera at pos (glTF axes).
    public void Update(Vector3 pos)
    {
        var c = new Vector2(pos.X, pos.Z);
        foreach (var m in _materials)
            m.SetShaderParameter("centre", c);
        if (_window is Vector2 w && Math.Abs(c.X - w.X) < RefreshM && Math.Abs(c.Y - w.Y) < RefreshM)
            return;
        // A new window round the camera, its corner on whole metres (the
        // ground map's cell edges).
        var mid = new Vector2(MathF.Round(c.X), MathF.Round(c.Y));
        _window = mid;
        var origin = mid - new Vector2(WindowM / 2, WindowM / 2);
        int gn = _groundImg.GetWidth();
        for (int j = 0; j < gn; j++)
            for (int i = 0; i < gn; i++)
            {
                float x = origin.X + (i + 0.5f) * _groundCell, z = origin.Y + (j + 0.5f) * _groundCell;
                var (land, clear) = _map.Ground(x, -z);
                _groundBuf[j * gn + i] = _codes[land];
                _clearBuf[j * gn + i] = clear;
            }
        int hn = _heightImg.GetWidth();
        var heights = new float[hn * hn];
        for (int j = 0; j < hn; j++)
            for (int i = 0; i < hn; i++)
            {
                // Texel centres, as linear filtering reads them.
                float x = origin.X + (i + 0.5f) * HeightCellM, z = origin.Y + (j + 0.5f) * HeightCellM;
                float h = _map.Height(x, -z);
                heights[j * hn + i] = float.IsNaN(h) ? 0 : h * _relief;
            }
        Buffer.BlockCopy(heights, 0, _heightBuf, 0, _heightBuf.Length);
        _groundImg.SetData(gn, gn, false, Image.Format.R8, _groundBuf);
        _clearImg.SetData(gn, gn, false, Image.Format.R8, _clearBuf);
        _heightImg.SetData(hn, hn, false, Image.Format.Rf, _heightBuf);
        _groundTex.Update(_groundImg);
        _clearTex.Update(_clearImg);
        _heightTex.Update(_heightImg);
        foreach (var m in _materials)
            m.SetShaderParameter("win_origin", origin);
        _terrain.SetShaderParameter("win_origin", origin);
        _terrain.SetShaderParameter("win_size", WindowM);
    }

    // Clump is a plant of blades round its foot, modelled: each a tapering
    // triangle pair bending outwards, coloured (linear) from root to tip.
    static ArrayMesh Clump(int blades, float height, float width, float spread, float lean, Color root, Color tip)
    {
        var st = new SurfaceTool();
        st.Begin(Mesh.PrimitiveType.Triangles);
        var rng = new RandomNumberGenerator { Seed = (ulong)(blades * 7919 + (int)(height * 1000)) };
        for (int b = 0; b < blades; b++)
        {
            float a = Mathf.Tau * b / blades + rng.RandfRange(-0.3f, 0.3f);
            var out_ = new Vector3(Mathf.Cos(a), 0, Mathf.Sin(a));
            var side = new Vector3(-out_.Z, 0, out_.X) * width;
            float hgt = height * rng.RandfRange(0.7f, 1.2f);
            var foot = out_ * spread * rng.RandfRange(0, 1);
            var mid = foot + out_ * lean * hgt * 0.35f + Vector3.Up * hgt * 0.55f;
            var top = foot + out_ * lean * hgt + Vector3.Up * hgt;
            var mc = root.Lerp(tip, 0.55f);
            void V(Vector3 p, Color c) { st.SetColor(c); st.SetNormal(Vector3.Up); st.AddVertex(p); }
            V(foot - side, root); V(foot + side, root); V(mid + side * 0.6f, mc);
            V(foot - side, root); V(mid + side * 0.6f, mc); V(mid - side * 0.6f, mc);
            V(mid - side * 0.6f, mc); V(mid + side * 0.6f, mc); V(top, tip);
        }
        return st.Commit();
    }

    // Heather is a low shrub of heather (Calluna vulgaris, 20-50 cm):
    // twigs from a small foot, upright and leaning out, olive green, each
    // with a spike of flowers along its upper part (vertex alpha 1: the
    // shader withers them out of season).
    static ArrayMesh Heather()
    {
        var st = new SurfaceTool();
        st.Begin(Mesh.PrimitiveType.Triangles);
        var rng = new RandomNumberGenerator { Seed = 1771 };
        var twig = new Color(0.05f, 0.06f, 0.025f, 0);
        var leaf = new Color(0.09f, 0.12f, 0.04f, 0);
        var flower = new Color(0.44f, 0.13f, 0.27f, 1);
        void V(Vector3 p, Color c) { st.SetColor(c); st.SetNormal(Vector3.Up); st.AddVertex(p); }
        for (int b = 0; b < 16; b++)
        {
            float a = Mathf.Tau * b / 16 + rng.RandfRange(-0.3f, 0.3f);
            var out_ = new Vector3(Mathf.Cos(a), 0, Mathf.Sin(a));
            var side = new Vector3(-out_.Z, 0, out_.X);
            float hgt = rng.RandfRange(0.22f, 0.36f);
            var foot = out_ * rng.RandfRange(0, 0.12f);
            var top = foot + out_ * hgt * rng.RandfRange(0.3f, 0.7f) + Vector3.Up * hgt;
            var mid = foot.Lerp(top, 0.5f);
            // The twig, leafy: a narrow blade to half way.
            V(foot - side * 0.02f, twig); V(foot + side * 0.02f, twig); V(mid + side * 0.03f, leaf);
            V(foot - side * 0.02f, twig); V(mid + side * 0.03f, leaf); V(mid - side * 0.03f, leaf);
            // The flower spike: wider, tapering to the tip.
            var fc = flower.Lightened(rng.RandfRange(0, 0.15f));
            V(mid - side * 0.03f, fc); V(mid + side * 0.03f, fc); V(top + side * 0.008f, fc);
            V(mid - side * 0.03f, fc); V(top + side * 0.008f, fc); V(top - side * 0.008f, fc);
        }
        return st.Commit();
    }

    // Cards is two crossed upright quads, 1 m wide and 1 m tall, standing
    // on their middle; UV y 0 at the top (the shader sways the tops).
    static ArrayMesh Cards()
    {
        var st = new SurfaceTool();
        st.Begin(Mesh.PrimitiveType.Triangles);
        foreach (var dir in new[] { new Vector3(1, 0, 0), new Vector3(0, 0, 1) })
        {
            var a = -dir * 0.5f;
            var b = dir * 0.5f;
            void V(Vector3 p, float u, float v) { st.SetUV(new Vector2(u, v)); st.SetNormal(Vector3.Up); st.AddVertex(p); }
            V(a, 0, 1); V(b, 1, 1); V(b + Vector3.Up, 1, 0);
            V(a, 0, 1); V(b + Vector3.Up, 1, 0); V(a + Vector3.Up, 0, 0);
        }
        return st.Commit();
    }

    // Atlas draws the four cards' pictures (128 px each, as the layers of a
    // texture array: in an atlas, a card's edge sampled its neighbour's and
    // showed a line of it): blades as tapering, slightly bending strokes, dark at the
    // root and lighter towards the tip, on transparent.
    static Texture2DArray Atlas()
    {
        const int T = 128;
        var imgs = new Image[6];
        for (int i = 0; i < imgs.Length; i++)
        {
            imgs[i] = Image.CreateEmpty(T, T, false, Image.Format.Rgba8);
            imgs[i].Fill(new Color(0, 0, 0, 0));
        }
        var rng = new RandomNumberGenerator { Seed = 20261009 };
        var root = new Color(0.30f, 0.45f, 0.14f);
        var tip = new Color(0.62f, 0.80f, 0.36f);

        void Blade(int tile, float x0, float height, float bend, float width, Color r, Color t)
        {
            var img = imgs[tile];
            int steps = (int)(height * T);
            for (int s = 0; s < steps; s++)
            {
                float f = (float)s / steps;              // 0 at the root
                float x = x0 * T + bend * T * f * f;     // bending as it rises
                float y = T - 1 - s;
                float half = Math.Max(0.5f, width * T * (1 - f) * 0.5f);
                var c = r.Lerp(t, f);
                for (int dx = (int)(x - half); dx <= (int)(x + half); dx++)
                    if (dx >= 0 && dx < T && y >= 0)
                        img.SetPixel(dx, (int)y, c);
            }
        }
        // 0: grass, open.
        for (int k = 0; k < 24; k++)
            Blade(0, rng.RandfRange(0.1f, 0.9f), rng.RandfRange(0.45f, 0.95f), rng.RandfRange(-0.25f, 0.25f), 0.06f, root, tip);
        // 1: grass, dense.
        for (int k = 0; k < 45; k++)
            Blade(1, rng.RandfRange(0.05f, 0.95f), rng.RandfRange(0.35f, 0.95f), rng.RandfRange(-0.3f, 0.3f), 0.055f, root, tip.Darkened(0.05f));
        // 2: a fern: fronds of leaflets along arching stems.
        var fern = new Color(0.36f, 0.58f, 0.18f);
        for (int k = 0; k < 7; k++)
        {
            float x0 = 0.5f + rng.RandfRange(-0.08f, 0.08f), lean = rng.RandfRange(-0.4f, 0.4f), len = rng.RandfRange(0.6f, 0.95f);
            Blade(2, x0, len, lean, 0.03f, root, fern);
            for (float f = 0.2f; f < 0.95f; f += 0.06f)
                foreach (var side in new[] { -1f, 1f })
                    Blade(2, x0 + lean * f * f + side * 0.02f, 0.07f * (1 - f) + 0.025f, side * 0.14f * (1.2f - f), 0.045f, fern.Darkened(0.05f), fern);
        }
        // 3: grass in flower: open grass, and heads of yellow, white and purple.
        for (int k = 0; k < 22; k++)
            Blade(3, rng.RandfRange(0.1f, 0.9f), rng.RandfRange(0.45f, 0.9f), rng.RandfRange(-0.2f, 0.2f), 0.06f, root, tip);
        var blooms = new[] { new Color(1.0f, 0.85f, 0.25f), new Color(0.95f, 0.95f, 0.9f), new Color(0.65f, 0.45f, 0.9f) };
        for (int k = 0; k < 6; k++)
        {
            float x = rng.RandfRange(0.2f, 0.8f), height = rng.RandfRange(0.6f, 0.95f);
            Blade(3, x, height, 0, 0.02f, root, tip);
            var c = blooms[k % blooms.Length];
            int cx = (int)(x * T), cy = (int)(T * (1 - height));
            for (int dy = -4; dy <= 4; dy++)
                for (int dx = -4; dx <= 4; dx++)
                    if (dx * dx + dy * dy <= 14 && cx + dx >= 0 && cx + dx < T && cy + dy >= 0 && cy + dy < T)
                        imgs[3].SetPixel(cx + dx, cy + dy, c);
        }
        // 4 and 5: heather, a carpet of short upright twigs; in flower
        // spikes of purple-pink along their upper part, out of flower
        // withered brown.
        var twigRoot = new Color(0.22f, 0.24f, 0.12f);
        var twigTip = new Color(0.36f, 0.40f, 0.20f);
        foreach (var (tile, spike) in new[] { (4, new Color(0.66f, 0.38f, 0.58f)), (5, new Color(0.45f, 0.32f, 0.24f)) })
        {
            var hr = new RandomNumberGenerator { Seed = 1771 }; // the same twigs in both
            for (int k = 0; k < 34; k++)
            {
                float x = hr.RandfRange(0.06f, 0.94f), height = hr.RandfRange(0.55f, 0.95f), bend = hr.RandfRange(-0.12f, 0.12f);
                Blade(tile, x, height, bend, 0.03f, twigRoot, twigTip);
                var c = spike.Lightened(hr.RandfRange(0, 0.15f));
                for (float f = 0.55f; f < 1f; f += 0.06f)
                {
                    int cx = (int)((x + bend * f * f) * T), cy = (int)(T - 1 - f * height * T);
                    for (int dy = -2; dy <= 2; dy++)
                        for (int dx = -2; dx <= 2; dx++)
                            if (dx * dx + dy * dy <= 4 && cx + dx >= 0 && cx + dx < T && cy + dy >= 0 && cy + dy < T)
                                imgs[tile].SetPixel(cx + dx, cy + dy, c);
                }
            }
        }
        var layers = new Godot.Collections.Array<Image>();
        foreach (var img in imgs)
        {
            // Transparent texels in the blades' colours, not black: the
            // mipmaps blend them in (Plants/Bleed.cs).
            var data = img.GetData();
            Osscycler.Plants.Bleed.Pad(data, T);
            img.SetData(T, T, false, Image.Format.Rgba8, data);
            img.GenerateMipmaps();
            layers.Add(img);
        }
        var arr = new Texture2DArray();
        arr.CreateFromImages(layers);
        return arr;
    }
}
