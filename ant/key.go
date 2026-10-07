package ant

import (
	"encoding/hex"
	"errors"
	"strings"
)

// NetworkKey is an 8-byte ANT network key. It never prints its value, so it
// can't leak into logs by accident.
type NetworkKey [8]byte

func (NetworkKey) String() string     { return "NetworkKey(redacted)" }
func (k NetworkKey) GoString() string { return k.String() }

// ParseNetworkKey parses 16 hex digits. It accepts "0x" prefixes and space,
// comma, colon or dash separators, so the key can be pasted from the
// adopter documentation in whatever form it appears there.
func ParseNetworkKey(s string) (NetworkKey, error) {
	var k NetworkKey
	s = strings.ReplaceAll(strings.ToLower(s), "0x", "")
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', ',', ':', '-', '\t', '\n':
			return -1
		}
		return r
	}, s)
	b, err := hex.DecodeString(s)
	if err != nil {
		// Don't wrap err: it would echo part of the key.
		return k, errors.New("ant: network key is not valid hex")
	}
	if len(b) != len(k) {
		return k, errors.New("ant: network key must be 8 bytes")
	}
	copy(k[:], b)
	return k, nil
}
