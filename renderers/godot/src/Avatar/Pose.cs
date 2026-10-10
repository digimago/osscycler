using System;
using System.Numerics;

namespace Osscycler.Avatar;

// Pose is where a cyclist's joints and a road bike's parts are, for a rider
// of a given height with the cranks at a given angle (plan step 9: made in
// code, every course shares it). Plain math apart from Godot, tested.
//
// Bike frame: x right, y up, z backward (Godot's forward is -z), the
// ground at y = 0 under the bottom bracket. Measures: a road bike (700c
// wheels 0.68 m across, bottom bracket 7 cm below the axles, 41 cm
// chainstays, seat tube at 73.5°, cranks by height) whose frame scales with
// the rider; body segments as fractions of height (Drillis and Contini:
// thigh 0.245, shank 0.246, upper arm 0.186, forearm 0.146, hip to
// shoulder 0.288); the saddle at 0.883 of the inseam (0.45 of height)
// above the bottom bracket (LeMond). Legs by two-bone IK from the pedals
// to the hips, knees forward; arms from the shoulders to the hoods, elbows
// out and down.
public sealed class Pose
{
    public const float WheelR = 0.34f, SeatAngle = 73.5f * MathF.PI / 180;
    public const float RefHeight = 1.80f;

    public float Height { get; }
    float S => Height / RefHeight; // the frame and body's scale

    // CrankM goes with the rider: 165 mm at 1.60 m, 172.5 at 1.80, 175 at 1.90.
    public float CrankM => Math.Clamp(0.1725f + (Height - RefHeight) * 0.035f, 0.160f, 0.180f);

    // The bike.
    public Vector3 BottomBracket, RearAxle, FrontAxle, SeatTop, SeatCluster, HeadTop, HeadBottom, Saddle, Bars, HoodL, HoodR;
    public Vector3 PedalL, PedalR;

    // The rider: left and right as the rider sees them.
    public Vector3 HipL, HipR, KneeL, KneeR, AnkleL, AnkleR, ToeL, ToeR;
    public Vector3 Pelvis, Chest, ShoulderL, ShoulderR, ElbowL, ElbowR, HandL, HandR, Head;

    // Lengths of the body's segments (for the renderer's capsules).
    public float Thigh => 0.245f * Height;
    public float Shank => 0.246f * Height;
    public float UpperArm => 0.186f * Height;
    public float Forearm => 0.146f * Height + 0.05f * S; // to the grip

    public Pose(float height)
    {
        Height = Math.Clamp(height, 1.2f, 2.2f);
        float s = S;
        BottomBracket = new(0, WheelR - 0.07f, 0);
        RearAxle = new(0, WheelR, 0.41f * s);
        FrontAxle = new(0, WheelR, -0.58f * s);
        float saddleH = 0.883f * 0.45f * Height;
        var seatDir = new Vector3(0, MathF.Sin(SeatAngle), MathF.Cos(SeatAngle)); // up and back
        SeatTop = BottomBracket + seatDir * (saddleH - 0.12f * s);
        SeatCluster = BottomBracket + seatDir * (saddleH * 0.72f);
        Saddle = BottomBracket + seatDir * saddleH;
        HeadTop = BottomBracket + new Vector3(0, 0.57f * s, -0.39f * s);
        float headAngle = 73f * MathF.PI / 180;
        HeadBottom = HeadTop + new Vector3(0, -MathF.Sin(headAngle), -MathF.Cos(headAngle)) * 0.16f * s;
        Bars = HeadTop + new Vector3(0, 0.03f * s, -0.10f * s);
        HoodL = Bars + new Vector3(-0.20f * s, 0.02f * s, -0.08f * s);
        HoodR = Bars + new Vector3(0.20f * s, 0.02f * s, -0.08f * s);

        Pelvis = Saddle + new Vector3(0, 0.04f * s, -0.02f * s);
        HipL = Pelvis + new Vector3(-0.09f * s, 0, 0);
        HipR = Pelvis + new Vector3(0.09f * s, 0, 0);
        // The torso leans forward 40° to the hoods.
        float torso = 0.288f * Height, lean = 40f * MathF.PI / 180;
        Chest = Pelvis + new Vector3(0, MathF.Sin(lean), -MathF.Cos(lean)) * torso;
        ShoulderL = Chest + new Vector3(-0.19f * s, 0, 0);
        ShoulderR = Chest + new Vector3(0.19f * s, 0, 0);
        Head = Chest + new Vector3(0, 0.20f * s, -0.10f * s);
        HandL = HoodL;
        HandR = HoodR;
        ElbowL = Joint(ShoulderL, HandL, UpperArm, Forearm, new Vector3(-0.5f, -1, 0.2f));
        ElbowR = Joint(ShoulderR, HandR, UpperArm, Forearm, new Vector3(0.5f, -1, 0.2f));
        Crank(0);
    }

    // Crank sets the pedals and legs for crank angle a (radians from the
    // right crank pointing up, turning forward).
    public void Crank(float a)
    {
        Vector3 Pedal(float ang, float side) =>
            BottomBracket + new Vector3(side * 0.15f * S, CrankM * MathF.Cos(ang), -CrankM * MathF.Sin(ang));
        PedalR = Pedal(a, 1);
        PedalL = Pedal(a + MathF.PI, -1);
        // The ankle above and behind the pedal (the ball of the foot on
        // it), the toe in front.
        var toAnkle = new Vector3(0, 0.06f, 0.07f) * S;
        AnkleR = PedalR + toAnkle - new Vector3(0.02f * S, 0, 0);
        AnkleL = PedalL + toAnkle + new Vector3(0.02f * S, 0, 0);
        ToeR = PedalR + new Vector3(0, 0.01f, -0.10f) * S;
        ToeL = PedalL + new Vector3(0, 0.01f, -0.10f) * S;
        KneeR = Joint(HipR, AnkleR, Thigh, Shank, new Vector3(0.05f, 0.3f, -1));
        KneeL = Joint(HipL, AnkleL, Thigh, Shank, new Vector3(-0.05f, 0.3f, -1));
    }

    // Joint is the middle joint of a two-bone limb from root to tip with
    // bones a and b, bending towards pole: by the law of cosines; a limb
    // too short to reach goes straight towards the tip.
    public static Vector3 Joint(Vector3 root, Vector3 tip, float a, float b, Vector3 pole)
    {
        var d = tip - root;
        float len = d.Length();
        if (len < 1e-5f)
            return root + Vector3.Normalize(pole) * a;
        var dir = d / len;
        len = Math.Clamp(len, MathF.Abs(a - b) + 1e-4f, a + b - 1e-4f);
        float along = (a * a - b * b + len * len) / (2 * len);
        float across = MathF.Sqrt(MathF.Max(0, a * a - along * along));
        var side = pole - dir * Vector3.Dot(pole, dir);
        side = side.LengthSquared() < 1e-8f ? Vector3.UnitY : Vector3.Normalize(side);
        return root + dir * along + side * across;
    }

    // CadenceFromSpeed is a cadence for a rider without one reported (the
    // ghost): about 90 rpm at 36 km/h on a 50 × 17 (6.2 m a turn of the
    // cranks), within 60-100; none below 1 m/s (coasting).
    public static float CadenceFromSpeed(float mps) => mps < 1 ? 0 : Math.Clamp(mps * 60 / 6.2f, 60, 100);
}
