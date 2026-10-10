using System.Collections.Generic;

namespace Osscycler.Plants;

// Bleed gives every transparent texel of a picture the colour of the
// nearest drawn one, its alpha left at 0 (edge padding). Mipmaps average
// neighbouring texels, colour and alpha alike: on transparent black a
// blade's edge turned dark at a distance, and the cards' shader, which
// raises alpha at coarser mips, drew it as a black outline (owner,
// 2026-10-10: grass with dark tops at 2.175 km of a preview). Plain C#
// apart from Godot, tested.
public static class Bleed
{
    // Pad fills rgba (size × size texels, 4 bytes each, row by row) in
    // place, breadth first from the drawn texels (alpha > 0), so each
    // transparent texel takes a colour from the nearest ring of them.
    public static void Pad(byte[] rgba, int size)
    {
        var done = new bool[size * size];
        var queue = new Queue<int>();
        for (int i = 0; i < size * size; i++)
            if (rgba[4 * i + 3] > 0)
            {
                done[i] = true;
                queue.Enqueue(i);
            }
        if (queue.Count == 0)
            return;
        while (queue.Count > 0)
        {
            int i = queue.Dequeue(), x = i % size, y = i / size;
            for (int k = 0; k < 4; k++)
            {
                int nx = x + (k == 0 ? 1 : k == 1 ? -1 : 0), ny = y + (k == 2 ? 1 : k == 3 ? -1 : 0);
                if (nx < 0 || ny < 0 || nx >= size || ny >= size)
                    continue;
                int j = ny * size + nx;
                if (done[j])
                    continue;
                done[j] = true;
                rgba[4 * j] = rgba[4 * i];
                rgba[4 * j + 1] = rgba[4 * i + 1];
                rgba[4 * j + 2] = rgba[4 * i + 2];
                queue.Enqueue(j);
            }
        }
    }
}
