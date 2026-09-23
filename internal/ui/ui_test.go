package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/joakimgrr/wurk/internal/git"
	"github.com/joakimgrr/wurk/internal/worktree"
)

// Tests render without a terminal, so terminalWidth is fallbackWidth and the
// layout is deterministic.

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func sample() []worktree.Entry {
	return []worktree.Entry{
		{Branch: "main", Path: "/Users/me/dev/myrepo", Main: true, Current: true, State: git.Merged},
		{Branch: "merged-work", Path: "/Users/me/dev/myrepo-worktrees/merged-work", Behind: 1, State: git.Merged},
		{Branch: "PROJ-2222-work-on-login-system", Path: "/Users/me/dev/myrepo-worktrees/PROJ-2222-work-on-login-system", Changes: 2, Ahead: 1},
	}
}

const root = "/Users/me/dev/myrepo-worktrees"

// isFootnote picks out the note under the table by the words it always keeps.
func isFootnote(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "paths relative to ") || strings.HasPrefix(trimmed, "worktrees in ")
}

// tableLines is the rendered table without its footnote, stripped of styling.
func tableLines(t *testing.T, entries []worktree.Entry, root string) []string {
	t.Helper()
	out := ansi.ReplaceAllString(Table(entries, "main", root), "")
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if !isFootnote(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestColumnsLineUp is the property the table is for: every row starts its
// cells at the same offsets, whatever is in them.
func TestColumnsLineUp(t *testing.T) {
	// The marker column is empty in the header and on every row but the
	// current one, so the columns are compared from the branch rightwards.
	const columns = 5
	var offsets [][]int
	for _, line := range tableLines(t, sample(), root) {
		if strings.Contains(line, "─") || strings.TrimSpace(line) == "" {
			continue
		}
		starts := cellStarts(line)
		if len(starts) < columns {
			t.Fatalf("line %q has %d cells, want at least %d", line, len(starts), columns)
		}
		offsets = append(offsets, starts[len(starts)-columns:])
	}
	if len(offsets) < 2 {
		t.Fatalf("expected a header and some rows, got %d", len(offsets))
	}
	for i, got := range offsets[1:] {
		for col := range got {
			if got[col] != offsets[0][col] {
				t.Errorf("row %d column %d starts at %d, header at %d", i+1, col, got[col], offsets[0][col])
			}
		}
	}
}

// TestRowsShareOneWidth keeps the header rule the same length as the rows.
func TestRowsShareOneWidth(t *testing.T) {
	lines := tableLines(t, sample(), root)
	for _, line := range lines {
		if got, want := len([]rune(line)), len([]rune(lines[0])); got != want {
			t.Errorf("line %q is %d wide, want %d", line, got, want)
		}
	}
}

// TestFitsTheTerminal covers the case that started this: a path long enough to
// stretch the table past the edge of the screen.
func TestFitsTheTerminal(t *testing.T) {
	entries := sample()
	entries[0].Path = "/Users/me/some/extremely/deeply/nested/directory/that/goes/on/and/on/and/on/myrepo"

	for _, line := range tableLines(t, entries, root) {
		if width := len([]rune(line)); width > fallbackWidth {
			t.Errorf("line is %d wide, want at most %d:\n%s", width, fallbackWidth, line)
		}
	}
}

// TestPathColumnDropsWhenCrowded checks the last resort: the path goes before
// the branch names are cut.
func TestPathColumnDropsWhenCrowded(t *testing.T) {
	entries := sample()
	for i := range entries {
		entries[i].Branch = strings.Repeat("x", 55) // no room left for paths
	}
	out := ansi.ReplaceAllString(Table(entries, "main", root), "")

	if strings.Contains(out, "PATH") {
		t.Error("the path column survived a table that had no room for it")
	}
	if !strings.Contains(out, "worktrees in") {
		t.Error("dropping the path column should leave a note saying where they are")
	}
	if !strings.Contains(out, strings.Repeat("x", 40)) {
		t.Error("branch names were cut before the path column was dropped")
	}
}

// TestFootnoteKeepsItsWords makes sure shortening the note cuts the path and
// not the explanation.
func TestFootnoteKeepsItsWords(t *testing.T) {
	deep := "/Users/me/" + strings.Repeat("a-long-directory-name/", 8) + "worktrees"
	entries := []worktree.Entry{
		{Branch: "main", Path: "/Users/me/dev/myrepo", Main: true},
		{Branch: "work", Path: deep + "/work"},
	}
	out := ansi.ReplaceAllString(Table(entries, "main", deep), "")

	var note string
	for _, line := range strings.Split(out, "\n") {
		if isFootnote(line) {
			note = line
		}
	}
	if note == "" {
		t.Fatalf("no note under the table:\n%s", out)
	}
	if width := len([]rune(note)); width > fallbackWidth {
		t.Errorf("the note is %d wide, want at most %d", width, fallbackWidth)
	}
	if !strings.Contains(note, "…") {
		t.Errorf("the note was not shortened: %q", note)
	}
}

// TestNoFootnoteWithoutShortenedPaths keeps the common single-worktree listing
// free of a note that explains nothing.
func TestNoFootnoteWithoutShortenedPaths(t *testing.T) {
	only := []worktree.Entry{{Branch: "main", Path: "/Users/me/dev/myrepo", Main: true, Current: true}}
	out := ansi.ReplaceAllString(Table(only, "main", root), "")
	if strings.Contains(out, "relative to") || strings.Contains(out, "worktrees in") {
		t.Errorf("unexpected note under a table that needs none:\n%s", out)
	}
}

func TestTruncate(t *testing.T) {
	for _, tc := range []struct {
		fn         func(string, int) string
		in         string
		width      int
		want, name string
	}{
		{truncateLeft, "/a/b/c", 10, "/a/b/c", "left: fits"},
		{truncateLeft, "/aaa/bbb/ccc", 6, "…b/ccc", "left: keeps the end"},
		{truncateLeft, "/aaa", 1, "…", "left: no room"},
		{truncateRight, "branch", 10, "branch", "right: fits"},
		{truncateRight, "PROJ-2222-login", 8, "PROJ-22…", "right: keeps the start"},
		{truncateRight, "PROJ", 0, "…", "right: no room"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.in, tc.width); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
			if width := len([]rune(tc.fn(tc.in, tc.width))); width > max(tc.width, 1) {
				t.Errorf("result is %d wide, want at most %d", width, tc.width)
			}
		})
	}
}

// cellStarts is the offset of each cell in a rendered line, cells being
// separated by the two spaces of padding between columns.
func cellStarts(line string) []int {
	var starts []int
	runes := []rune(line)
	inCell := false
	for i := 0; i < len(runes); i++ {
		if runes[i] != ' ' && !inCell {
			starts = append(starts, i)
			inCell = true
		}
		if inCell && runes[i] == ' ' && i+1 < len(runes) && runes[i+1] == ' ' {
			inCell = false
		}
	}
	return starts
}
