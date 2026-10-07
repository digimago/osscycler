package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNetworkKeyNotInSource fails if the ANT+ network key appears in any
// file git would commit, in any formatting (0x prefixes, separators, case).
// It runs where the key is known: $ANT_PLUS_NETWORK_KEY (CI) or the local
// key file. It never prints the key.
func TestNetworkKeyNotInSource(t *testing.T) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("not in a git checkout")
	}
	root := strings.TrimSpace(string(out))
	raw := os.Getenv(keyEnv)
	if raw == "" {
		b, err := os.ReadFile(filepath.Join(root, "_secrets", "ant-network-key"))
		if err != nil {
			t.Skip("no ANT+ network key here to look for")
		}
		raw = string(b)
	}
	key := hexOnly(raw)
	if len(key) != 16 {
		t.Fatalf("the key to look for isn't 16 hex digits (%d)", len(key))
	}

	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	files, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range strings.Split(strings.TrimRight(string(files), "\x00"), "\x00") {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue // deleted in the work tree
		}
		if strings.Contains(hexOnly(string(b)), key) {
			t.Errorf("%s contains the ANT+ network key; it must never be committed", f)
		}
	}
}

// hexOnly lowercases s, drops 0x prefixes and keeps only hex digits.
func hexOnly(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "0x", "")
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			return r
		}
		return -1
	}, s)
}

func TestLoadNetworkKeyOrder(t *testing.T) {
	defer func(k string) { builtinNetworkKey = k }(builtinNetworkKey)
	const a, b = "0102030405060708", "1112131415161718"

	builtinNetworkKey = b
	t.Setenv(keyEnv, a)
	if k, src, err := loadNetworkKey(); err != nil || k[0] != 0x01 || src != "$"+keyEnv {
		t.Errorf("env should win: %v, %q", err, src)
	}
	t.Setenv(keyEnv, "")
	if k, src, err := loadNetworkKey(); err != nil || k[0] != 0x11 || src != "built in" {
		t.Errorf("built in: %v, %q", err, src)
	}
	builtinNetworkKey = ""
	if _, _, err := loadNetworkKey(); err == nil || !strings.Contains(err.Error(), "developer.garmin.com") {
		t.Errorf("no key: %v", err)
	}
	builtinNetworkKey = "zz"
	if _, _, err := loadNetworkKey(); err == nil || !strings.Contains(err.Error(), "built into this binary") {
		t.Errorf("bad built-in key: %v", err)
	}
}
