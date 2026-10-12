namespace Osscycler.Head;

// WorldPick decides whether the world a ride needs has to be loaded: when
// it is in another directory than the one on screen, or in the same one
// but built again since (another stamp on disk). Comparing directories
// alone kept the old world: the world shown at rest at start-up is the
// figure 8's, and a rebuild writes it to the same directory, so a ride
// begun on it after a rebuild rode the old build (owner, 2026-10-12).
public static class WorldPick
{
    // NeedsLoad: dir and stamp are the ride's world (stamp "" when unknown:
    // not read again), shown and shownStamp the world on screen; loading
    // the directory being loaded, missing one that failed to load.
    public static bool NeedsLoad(string? dir, string stamp, string shown, string shownStamp, string loading, string missing, bool swapping)
    {
        if (dir == null || dir == loading || dir == missing || swapping)
            return false;
        return dir != shown || stamp != "" && shownStamp != "" && stamp != shownStamp;
    }
}
