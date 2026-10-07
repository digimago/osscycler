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
