package tui

import "strings"

// glyphs is a 5-row block font. '#' is a lit pixel; each pixel renders two
// cells wide so it looks square in a terminal.
var glyphs = map[rune][5]string{
	'0': {"###", "# #", "# #", "# #", "###"},
	'1': {"## ", " # ", " # ", " # ", "###"},
	'2': {"###", "  #", "###", "#  ", "###"},
	'3': {"###", "  #", "###", "  #", "###"},
	'4': {"# #", "# #", "###", "  #", "  #"},
	'5': {"###", "#  ", "###", "  #", "###"},
	'6': {"###", "#  ", "###", "# #", "###"},
	'7': {"###", "  #", "  #", "  #", "  #"},
	'8': {"###", "# #", "###", "# #", "###"},
	'9': {"###", "# #", "###", "  #", "###"},
	'-': {"   ", "   ", "###", "   ", "   "},
	'.': {" ", " ", " ", " ", "#"},
	':': {" ", "#", " ", "#", " "},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// BigHeight is the number of lines Big returns.
const BigHeight = 5

// Big renders s in the block font. Characters without a glyph are skipped.
func Big(s string) string {
	var rows [BigHeight]strings.Builder
	first := true
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		for i := range rows {
			if !first {
				rows[i].WriteString("  ")
			}
			for _, px := range g[i] {
				if px == '#' {
					rows[i].WriteString("██")
				} else {
					rows[i].WriteString("  ")
				}
			}
		}
		first = false
	}
	lines := make([]string, BigHeight)
	for i := range rows {
		lines[i] = rows[i].String()
	}
	return strings.Join(lines, "\n")
}

// MediumHeight is the number of lines Medium returns.
const MediumHeight = 3

// Medium renders s in the same font at about half the size: a pixel per
// cell, two pixel rows per line in half blocks.
func Medium(s string) string {
	var rows [MediumHeight]strings.Builder
	first := true
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		for i := range rows {
			if !first {
				rows[i].WriteString(" ")
			}
			top := g[2*i]
			bottom := strings.Repeat(" ", len(top))
			if 2*i+1 < len(g) {
				bottom = g[2*i+1]
			}
			for x := range len(top) {
				switch t, b := top[x] == '#', bottom[x] == '#'; {
				case t && b:
					rows[i].WriteString("█")
				case t:
					rows[i].WriteString("▀")
				case b:
					rows[i].WriteString("▄")
				default:
					rows[i].WriteString(" ")
				}
			}
		}
		first = false
	}
	lines := make([]string, MediumHeight)
	for i := range rows {
		lines[i] = rows[i].String()
	}
	return strings.Join(lines, "\n")
}

// digitSize is how big tiles draw their numbers.
type digitSize int

const (
	sizeLarge  digitSize = iota // Big
	sizeMedium                  // Medium
)

func (s digitSize) render(v string) string {
	if s == sizeMedium {
		return Medium(v)
	}
	return Big(v)
}

func (s digitSize) height() int {
	if s == sizeMedium {
		return MediumHeight
	}
	return BigHeight
}
