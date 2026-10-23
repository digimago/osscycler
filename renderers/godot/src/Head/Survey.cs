using System;

namespace Osscycler.Head;

// Survey places the camera to look at a spot of the course from outside, as
// from a helicopter: straight down or at a slant from any side (the control
// socket's camera command; the MCP tools view_camera and view_survey). Plain
// numbers in the world's frame (x east, y up, z south), angles in degrees;
// yaw and pitch as --camera takes them (yaw clockwise from north, pitch up).
public static class Survey
{
    // FovDeg is the camera's vertical field of view (Godot's default).
    public const double FovDeg = 75;

    // Orbit is the camera dist metres from the spot at x, y, z, looking at
    // it from the side fromDeg right of the way ahead (headingDeg, clockwise
    // from north: 180 from behind, 90 from the right, 0 from ahead, looking
    // back), elevDeg above the horizon. Straight down (90) the way ahead is
    // at the top of the picture, from whichever side.
    public static (double X, double Y, double Z, double Yaw, double Pitch) Orbit(
        double x, double y, double z, double headingDeg, double fromDeg, double elevDeg, double dist)
    {
        elevDeg = Math.Clamp(elevDeg, -89, 90);
        double bearing = Rad(headingDeg + fromDeg); // from the spot to the camera
        double h = dist * Math.Cos(Rad(elevDeg));
        double yaw = elevDeg >= 89.9 ? headingDeg : headingDeg + fromDeg + 180;
        return (x + h * Math.Sin(bearing), y + dist * Math.Sin(Rad(elevDeg)), z - h * Math.Cos(bearing), Norm(yaw), -elevDeg);
    }

    // Heading is the bearing of a step dx east, dz south, clockwise from
    // north.
    public static double Heading(double dx, double dz) => Norm(Math.Atan2(dx, -dz) * 180 / Math.PI);

    // DistanceFor is how far away the camera sees radius metres either side
    // of the spot, top to bottom of the picture.
    public static double DistanceFor(double radius) => radius / Math.Tan(Rad(FovDeg / 2));

    static double Rad(double deg) => deg * Math.PI / 180;

    static double Norm(double deg) => ((deg % 360) + 360) % 360;
}
