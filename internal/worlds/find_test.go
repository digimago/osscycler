package worlds

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/digimago/osscycler/internal/world"
)

func TestFindTheCurrentWorld(t *testing.T) {
	own, pre := t.TempDir(), t.TempDir()
	put := func(dir, stamp string) {
		os.MkdirAll(filepath.Join(dir, "posbank"), 0o755)
		os.WriteFile(filepath.Join(dir, "posbank", world.ManifestFile), []byte(`{"format":"osscycler-world","stamp":"`+stamp+`","path":{}}`), 0o644)
	}
	if got := Find([]string{own, pre}, "posbank", "1-a"); got != "" {
		t.Errorf("found %q where there is none", got)
	}
	put(pre, "1-a")
	if got := Find([]string{own, pre}, "posbank", "1-a"); got != filepath.Join(pre, "posbank") {
		t.Errorf("ready-built: %q", got)
	}
	put(own, "1-old")
	if got := Find([]string{own, pre}, "posbank", "1-a"); got != filepath.Join(pre, "posbank") {
		t.Errorf("an out-of-date own world beat a current ready-built one: %q", got)
	}
	put(own, "1-a")
	if got := Find([]string{own, pre}, "posbank", "1-a"); got != filepath.Join(own, "posbank") {
		t.Errorf("own: %q", got)
	}
}
