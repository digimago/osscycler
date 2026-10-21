using System;

namespace Osscycler.Hud;

// Solar is where the sun stands for a place and a day (owner, 2026-10-15:
// the sun stood high in the north-east over the Vosges, picked at random
// whatever the place and day): its height above the horizon and its
// compass direction, by the usual approximations (declination from the
// day of the year, the hour angle from local solar time; within a degree
// or two, plenty for light and shadows). Plain C#; Main lights the world
// with it.
public static class Solar
{
    // Position is the sun at latitude and longitude (degrees) on day of
    // year doy at local solar time hours (12: noon, the sun due south in
    // the northern hemisphere): height above the horizon and azimuth
    // clockwise from north, in degrees.
    public static (double Height, double Azimuth) Position(double lat, int doy, double hours)
    {
        double rad = Math.PI / 180;
        double decl = 23.44 * Math.Sin(2 * Math.PI / 365 * (doy - 81)) * rad;
        double ha = 15 * (hours - 12) * rad; // the hour angle, west positive
        double phi = lat * rad;
        double sinH = Math.Sin(phi) * Math.Sin(decl) + Math.Cos(phi) * Math.Cos(decl) * Math.Cos(ha);
        double height = Math.Asin(Math.Clamp(sinH, -1, 1));
        // Azimuth from north, clockwise: east in the morning, west after
        // noon.
        double az = Math.Atan2(Math.Sin(ha), Math.Cos(ha) * Math.Sin(phi) - Math.Tan(decl) * Math.Cos(phi)) + Math.PI;
        return (height / rad, (az / rad + 360) % 360);
    }

    // RideHours is the local solar time a ride's light is for at latitude
    // lat on day doy, picked from r and low in [0, 1): mostly between
    // mid-morning and mid-afternoon, but one ride in four (low < 0.25) in
    // the low light of early morning or late afternoon, the sun 6-14° up
    // (owner, 2026-10-15: a somewhat realistic sun with a chance of
    // dramatic lighting, rather than one overhead as in summer).
    public static double RideHours(double lat, int doy, double r, double low)
    {
        if (low >= 0.25)
            return 9.5 + 6.5 * r;
        double want = 6 + 8 * r;
        bool morning = low < 0.125;
        // Walk from noon towards the morning or the evening until the sun
        // is that low (it falls steadily that way).
        double step = morning ? -0.05 : 0.05;
        for (double h = 12; h > 3 && h < 21; h += step)
        {
            if (Position(lat, doy, h).Height <= want)
                return h;
        }
        return morning ? 9.5 : 16; // the sun never that low by day (far north in summer: none here)
    }

    // LightYaw is the yaw (degrees about the vertical) of a directional
    // light shining from azimuth az in a world whose x is east and z south
    // (glTF axes): Godot's light shines along its -Z, which yaw 0 points
    // north (the sun then in the south), turning towards -X (west) as yaw
    // grows.
    public static double LightYaw(double az) => ((180 - az) % 360 + 360) % 360;

    // DayOfYear is the day a ride's seed stands for (Main's default seed is
    // year × 1000 + day), 1 to 366; another seed takes its last three
    // digits, clamped into the year.
    public static int DayOfYear(int seed) => Math.Clamp(Math.Abs(seed) % 1000, 1, 366);
}
