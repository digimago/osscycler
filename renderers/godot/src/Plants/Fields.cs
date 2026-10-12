using System;

namespace Osscycler.Plants;

// Fields are the world's sown fields as the ground map names them
// ("maize 3": the crop, and the way its rows run in eighths of a half turn
// anticlockwise from east; world/ground.go), and their codes in the
// shaders that grow crops and draw furrows (ground.gdshaderinc, terrain).
public static class Fields
{
    // Crops is the sown fields' crops, in the order of their codes.
    public static readonly string[] Crops = ["ploughed", "maize", "wheat", "potatoes"];

    // Code is the shaders' code of a sown field's class: 16 + 8 × the crop
    // + the rows' direction; null for any other class.
    public static byte? Code(string cls)
    {
        int sp = cls.LastIndexOf(' ');
        if (sp < 0 || !int.TryParse(cls[(sp + 1)..], out int dir) || dir < 0 || dir > 7)
            return null;
        int crop = Array.IndexOf(Crops, cls[..sp]);
        return crop < 0 ? null : (byte)(16 + 8 * crop + dir);
    }
}
