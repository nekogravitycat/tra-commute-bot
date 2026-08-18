package render

import "strings"

// Alignment inside a <pre> block relies on one invariant: its content is
// printable ASCII only.
//
// The obvious alternative — measuring Unicode East Asian Width and treating
// CJK as two cells — does not work here. Telegram's monospace font does not
// render a Chinese glyph at exactly twice the width of a Latin one, so a table
// that is perfectly rectangular by that model still comes out crooked on the
// handset. Column headings are therefore English abbreviations (NO., DLY,
// DEP, ARR, LATE) and every value in the grid is ASCII. Chinese belongs in the
// prose around the table, where it is not being aligned against anything.
//
// TestPreBlocksAreASCII enforces the invariant.

// cellWidth returns how many monospace cells s occupies, which for the ASCII
// content of a table is simply its length.
func cellWidth(s string) int { return len(s) }

// padLeft right-aligns s in the given width.
func padLeft(s string, width int) string {
	if pad := width - cellWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// padRight left-aligns s in the given width.
func padRight(s string, width int) string {
	if pad := width - cellWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// isASCII reports whether s is printable ASCII, the precondition for anything
// placed inside a <pre> block.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7E {
			return false
		}
	}
	return true
}
