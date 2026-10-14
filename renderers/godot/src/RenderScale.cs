using System;

namespace Osscycler;

// RenderScale is how much of the window's resolution the 3D view renders
// at: all of it up to 1440 pixels high (owner, 2026-10-14), and above that
// 1440 lines, upscaled with FSR (the HUD stays at the window's own). Full
// screen on a Retina Mac (3600×2260) rendered at 49-58 fps against the
// 120 Hz cap in a window; the GPU is the limit there, and its work grows
// with the pixels.
public static class RenderScale
{
    public const int MaxLines = 1440;

    // Of is the 3D scale for a window height pixels high (1: full).
    public static float Of(int height) =>
        height <= MaxLines ? 1 : MathF.Min(1, MaxLines / (float)height);
}
