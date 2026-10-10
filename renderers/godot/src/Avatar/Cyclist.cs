using System;
using System.Collections.Generic;
using Godot;
using N = System.Numerics;

namespace Osscycler.Avatar;

// Cyclist is a rider on a road bike, made in code (plan step 9; the
// world-assets skill: low-poly, no asset files): tubes, wheels with spokes,
// cranks and a rider of capsules, posed by Pose. Advance turns the cranks
// at the cadence and the wheels at the speed; the legs follow the pedals.
// The node's origin is on the ground under the bottom bracket, facing -z.
// As a ghost every part is see-through in the ghost's magenta.
public sealed class Cyclist
{
    public Node3D Node { get; } = new() { Name = "cyclist" };

    readonly Pose _pose;
    readonly Node3D _rear = new(), _front = new(), _cranks = new();
    readonly List<MeshInstance3D> _parts = new();
    readonly MeshInstance3D _thighL, _thighR, _shankL, _shankR, _footL, _footR;
    float _crank, _wheel;
    StandardMaterial3D? _ghost;

    // The look: kit colours, as sRGB.
    public sealed record Look(Color Jersey, Color Shorts, Color Skin, Color Helmet, Color Frame);

    public static readonly Look Plain = new(new Color("2f6db5"), new Color("1b1b1d"), new Color("d9a27a"), new Color("f2f2f2"), new Color("c23a2b"));

    public Cyclist(float height, Look look)
    {
        _pose = new Pose(height);
        var p = _pose;
        var frame = Mat(look.Frame, 0.4f, 0.3f);
        var black = Mat(new Color("161616"), 0.8f);
        var steel = Mat(new Color("9aa0a6"), 0.35f, 0.7f);
        var jersey = Mat(look.Jersey, 0.7f);
        var shorts = Mat(look.Shorts, 0.7f);
        var skin = Mat(look.Skin, 0.75f);
        var helmet = Mat(look.Helmet, 0.4f);

        // The frame: tubes between its points (chainstays and seatstays
        // either side of the wheel, the fork's two legs).
        Tube(p.BottomBracket, p.SeatTop, 0.017f, frame);
        Tube(p.SeatCluster, p.HeadTop, 0.016f, frame);
        Tube(p.BottomBracket, p.HeadBottom, 0.022f, frame);
        Tube(p.HeadBottom, p.HeadTop, 0.02f, frame);
        foreach (float x in new[] { -0.065f, 0.065f })
        {
            var off = new N.Vector3(x, 0, 0);
            Tube(p.BottomBracket + off * 0.5f, p.RearAxle + off, 0.011f, frame);
            Tube(p.SeatCluster, p.RearAxle + off, 0.009f, frame);
            Tube(p.HeadBottom + off * 0.3f, p.FrontAxle + off * 0.75f, 0.012f, frame);
        }
        Tube(p.SeatTop, p.Saddle - new N.Vector3(0, 0.03f, 0), 0.0135f, black);
        Box(p.Saddle - new N.Vector3(0, 0.015f, -0.02f), new Vector3(0.13f, 0.035f, 0.27f), black);
        Tube(p.HeadTop, p.Bars, 0.016f, black);
        Tube(p.Bars + new N.Vector3(-0.21f, 0, 0), p.Bars + new N.Vector3(0.21f, 0, 0), 0.013f, black);
        foreach (var hood in new[] { p.HoodL, p.HoodR })
        {
            // The hood, and the drop curling down and back under it.
            Tube(hood + new N.Vector3(0, -0.02f, 0.08f), hood, 0.017f, black);
            Tube(hood, hood + new N.Vector3(0, -0.10f, -0.02f), 0.013f, black);
            Tube(hood + new N.Vector3(0, -0.10f, -0.02f), hood + new N.Vector3(0, -0.14f, 0.07f), 0.013f, black);
        }

        // Wheels: tyre, rim, hub, and spokes that show them turn.
        foreach (var (node, axle) in new[] { (_rear, p.RearAxle), (_front, p.FrontAxle) })
        {
            node.Position = V(axle);
            Node.AddChild(node);
            Part(node, new TorusMesh { InnerRadius = Pose.WheelR - 0.028f, OuterRadius = Pose.WheelR, Rings = 28, RingSegments = 6 }, black,
                new Transform3D(new Basis(Vector3.Forward, Mathf.Pi / 2), Vector3.Zero));
            Part(node, new TorusMesh { InnerRadius = Pose.WheelR - 0.05f, OuterRadius = Pose.WheelR - 0.026f, Rings = 28, RingSegments = 4 }, steel,
                new Transform3D(new Basis(Vector3.Forward, Mathf.Pi / 2), Vector3.Zero));
            Part(node, new CylinderMesh { TopRadius = 0.02f, BottomRadius = 0.02f, Height = 0.1f, RadialSegments = 8 }, steel,
                new Transform3D(new Basis(Vector3.Forward, Mathf.Pi / 2), Vector3.Zero));
            for (int i = 0; i < 6; i++)
                Part(node, new CylinderMesh { TopRadius = 0.0025f, BottomRadius = 0.0025f, Height = 2 * (Pose.WheelR - 0.04f), RadialSegments = 3 }, steel,
                    new Transform3D(new Basis(Vector3.Right, i * Mathf.Pi / 6), Vector3.Zero));
        }

        // The cranks turn as one: the chainring, both arms, the pedals.
        _cranks.Position = V(p.BottomBracket);
        Node.AddChild(_cranks);
        Part(_cranks, new CylinderMesh { TopRadius = 0.105f, BottomRadius = 0.105f, Height = 0.008f, RadialSegments = 16 }, steel,
            new Transform3D(new Basis(Vector3.Forward, Mathf.Pi / 2), new Vector3(0.06f, 0, 0)));
        foreach (float side in new[] { 1f, -1f })
        {
            // The right arm points up at angle 0, the left down.
            var tip = new Vector3(side * 0.15f * p.Height / Pose.RefHeight, side * p.CrankM, 0);
            Part(_cranks, new BoxMesh { Size = new Vector3(0.018f, p.CrankM, 0.03f) }, black,
                new Transform3D(Basis.Identity, new Vector3(side * 0.075f, side * p.CrankM / 2, 0)));
            Part(_cranks, new BoxMesh { Size = new Vector3(0.09f, 0.02f, 0.07f) }, black, new Transform3D(Basis.Identity, tip));
        }

        // The rider.
        Tube(p.Pelvis + new N.Vector3(-0.1f, 0, 0), p.Pelvis + new N.Vector3(0.1f, 0, 0), 0.11f, shorts);
        // The torso: the bib shorts' black over its lower third, so the
        // back runs into the shorts over the saddle; the jersey above.
        var waist = N.Vector3.Lerp(p.Pelvis, p.Chest, 0.35f);
        var lower = Tube(p.Pelvis, waist, 0.14f, shorts);
        lower.Scale = new Vector3(1.25f, 1, 0.8f);
        var torso = Tube(waist, p.Chest, 0.14f, jersey);
        torso.Scale = new Vector3(1.25f, 1, 0.8f);
        Tube(p.ShoulderL, p.ShoulderR, 0.075f, jersey);
        Tube(p.Chest, p.Head - new N.Vector3(0, 0.08f, 0), 0.05f, skin);
        Sphere(p.Head, 0.1f, skin);
        var lid = Sphere(p.Head + new N.Vector3(0, 0.035f, 0.01f), 0.125f, helmet);
        lid.Scale = new Vector3(1, 0.75f, 1.2f);
        foreach (var (sh, el, hand) in new[] { (p.ShoulderL, p.ElbowL, p.HandL), (p.ShoulderR, p.ElbowR, p.HandR) })
        {
            Tube(sh, el, 0.048f, jersey);
            Tube(el, hand, 0.038f, skin);
            Sphere(hand, 0.045f, black); // gloves
        }
        _thighL = Tube(p.HipL, p.KneeL, 0.075f, shorts);
        _thighR = Tube(p.HipR, p.KneeR, 0.075f, shorts);
        _shankL = Tube(p.KneeL, p.AnkleL, 0.052f, skin);
        _shankR = Tube(p.KneeR, p.AnkleR, 0.052f, skin);
        _footL = Part(Node, new BoxMesh { Size = new Vector3(0.09f, 0.08f, 0.26f) }, black, null);
        _footR = Part(Node, new BoxMesh { Size = new Vector3(0.09f, 0.08f, 0.26f) }, black, null);
        Legs();
    }

    // Advance turns the cranks at cadence (rpm) and the wheels at speed
    // (m/s) over dt seconds, and poses the legs.
    public void Advance(double dt, float cadence, float speed)
    {
        _crank = (_crank + cadence / 60f * Mathf.Tau * (float)dt) % Mathf.Tau;
        _wheel = (_wheel + speed / Pose.WheelR * (float)dt) % Mathf.Tau;
        // Forward turns the top of the wheel towards -z: about +x, negative.
        _rear.Basis = new Basis(Vector3.Right, -_wheel);
        _front.Basis = new Basis(Vector3.Right, -_wheel);
        // Pose's angle turns the right crank from up to forward (-z).
        _cranks.Basis = new Basis(Vector3.Right, -_crank);
        Legs();
    }

    void Legs()
    {
        _pose.Crank(_crank);
        var p = _pose;
        Place(_thighL, p.HipL, p.KneeL);
        Place(_thighR, p.HipR, p.KneeR);
        Place(_shankL, p.KneeL, p.AnkleL);
        Place(_shankR, p.KneeR, p.AnkleR);
        Foot(_footL, p.AnkleL, p.ToeL);
        Foot(_footR, p.AnkleR, p.ToeR);
    }

    // Ghostly makes every part see-through in the ghost's colour at alpha
    // (0 hides it).
    public void Ghostly(Color c, float alpha)
    {
        if (_ghost == null)
        {
            _ghost = new StandardMaterial3D
            {
                Transparency = BaseMaterial3D.TransparencyEnum.Alpha,
                ShadingMode = BaseMaterial3D.ShadingModeEnum.PerPixel,
                Roughness = 0.6f,
                EmissionEnabled = true,
                Emission = c * 0.35f,
            };
            foreach (var m in _parts)
            {
                m.MaterialOverride = _ghost;
                m.CastShadow = GeometryInstance3D.ShadowCastingSetting.Off;
            }
        }
        _ghost.AlbedoColor = c with { A = alpha };
        Node.Visible = alpha > 0.01f;
    }

    static Vector3 V(N.Vector3 v) => new(v.X, v.Y, v.Z);

    static StandardMaterial3D Mat(Color c, float rough, float metal = 0) =>
        new() { AlbedoColor = c, Roughness = rough, Metallic = metal };

    MeshInstance3D Part(Node3D parent, Mesh mesh, Material mat, Transform3D? t)
    {
        var mi = new MeshInstance3D { Mesh = mesh, MaterialOverride = mat };
        if (t is { } tt)
            mi.Transform = tt;
        parent.AddChild(mi);
        _parts.Add(mi);
        return mi;
    }

    // Tube is a capsule from a to b, radius r (its length fixed now: a limb
    // keeps it as it moves).
    MeshInstance3D Tube(N.Vector3 a, N.Vector3 b, float r, Material mat)
    {
        float len = N.Vector3.Distance(a, b);
        var mesh = new CapsuleMesh { Radius = r, Height = len + 2 * r, RadialSegments = 8, Rings = 1 };
        var mi = Part(Node, mesh, mat, null);
        Place(mi, a, b);
        return mi;
    }

    MeshInstance3D Sphere(N.Vector3 at, float r, Material mat) =>
        Part(Node, new SphereMesh { Radius = r, Height = 2 * r, RadialSegments = 10, Rings = 5 }, mat, new Transform3D(Basis.Identity, V(at)));

    void Box(N.Vector3 at, Vector3 size, Material mat) => Part(Node, new BoxMesh { Size = size }, mat, new Transform3D(Basis.Identity, V(at)));

    // Place puts a capsule's axis (its y) from a to b, keeping its scale.
    static void Place(MeshInstance3D mi, N.Vector3 a, N.Vector3 b)
    {
        var from = V(a);
        var d = V(b) - from;
        var y = d.Normalized();
        var x = Mathf.Abs(y.Dot(Vector3.Forward)) > 0.95f ? Vector3.Right : y.Cross(Vector3.Forward).Normalized();
        var z = x.Cross(y);
        var scale = mi.Scale;
        mi.Transform = new Transform3D(new Basis(x * scale.X, y * scale.Y, z * scale.Z), from + d / 2);
    }

    // Foot: the shoe from behind the ankle to the toe.
    static void Foot(MeshInstance3D mi, N.Vector3 ankle, N.Vector3 toe)
    {
        var a = V(ankle);
        var t = V(toe);
        var fwd = (t - a) with { X = 0 };
        var z = -fwd.Normalized();
        var x = Vector3.Right;
        var y = z.Cross(x);
        mi.Transform = new Transform3D(new Basis(x, y, z), a.Lerp(t, 0.5f) + new Vector3(0, -0.02f, 0));
    }
}
