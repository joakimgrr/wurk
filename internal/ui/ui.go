// Package ui renders wurk's output: styled status lines and the worktree
// table. Styling comes from lipgloss, which drops the colour by itself when
// the output is not a terminal.
package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/term"

	"github.com/joakimgrr/wurk/internal/git"
	"github.com/joakimgrr/wurk/internal/worktree"
)

var (
	bold   = lipgloss.NewStyle().Bold(true)
	dim    = lipgloss.NewStyle().Faint(true)
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	header = bold.Foreground(lipgloss.Color("6"))
	cell   = lipgloss.NewStyle().Padding(0, 1)
)

const (
	// markerWidth is what the ● column takes up, counting the padding after
	// it. Notes under the table are indented by it so that they line up with
	// the branch names rather than with the markers.
	markerWidth = 3
	// minPathWidth is the narrowest a path column is still worth showing; below
	// it the column is dropped and the root named under the table instead.
	minPathWidth = 12
	// minBranchWidth is the point past which branch names are not cut further,
	// and the table is allowed to overflow instead.
	minBranchWidth = 10
	// fallbackWidth stands in for the terminal width when there is no
	// terminal, as in a pipe or a test.
	fallbackWidth = 100
)

// Success reports something that worked.
func Success(w io.Writer, format string, a ...any) {
	fmt.Fprintln(w, green.Render("✓")+" "+fmt.Sprintf(format, a...))
}

// Info reports something that needed no work.
func Info(w io.Writer, format string, a ...any) {
	fmt.Fprintln(w, dim.Render("·")+" "+fmt.Sprintf(format, a...))
}

// Warn reports something that worked but is worth noticing.
func Warn(w io.Writer, format string, a ...any) {
	fmt.Fprintln(w, yellow.Render("!")+" "+fmt.Sprintf(format, a...))
}

// Dim styles secondary text.
func Dim(s string) string { return dim.Render(s) }

// Key styles the label of a setting.
func Key(s string) string { return header.Render(s) }

// Branch styles a branch name.
func Branch(name string) string { return bold.Render(name) }

// Path styles a path, shortened to ~ where it sits under the home directory.
func Path(p string) string { return dim.Render(Shorten(p)) }

// Shorten replaces the home directory with ~.
func Shorten(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return p
}

// Table renders one row per worktree, with the columns that say whether it is
// finished with: what is uncommitted, how it sits against base, and whether
// its work has landed. Paths are shown relative to root, the directory the
// worktrees live in, which is where all but the main one usually are.
func Table(entries []worktree.Entry, base, root string) string {
	rows := buildRows(entries, root)
	if len(rows) == 0 {
		return ""
	}
	branchWidth, pathWidth := columnWidths(base, rows)

	headers := []string{"", "BRANCH", "CHANGES", "VS " + strings.ToUpper(base), "STATE"}
	if pathWidth > 0 {
		headers = append(headers, "PATH")
	}

	t := table.New().
		Headers(headers...).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(true).
		BorderStyle(dim).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := cell
			if col == 0 {
				style = style.PaddingLeft(0)
			}
			if row == table.HeaderRow {
				return style.Inherit(header)
			}
			return style
		})

	for _, r := range rows {
		cells := []string{
			r.marker,
			r.branchStyle.Render(truncateRight(r.branch, branchWidth)),
			r.changes, r.drift, r.state,
		}
		if pathWidth > 0 {
			cells = append(cells, dim.Render(truncateLeft(r.path, pathWidth)))
		}
		t.Row(cells...)
	}
	return t.Render() + footnote(rows, root, pathWidth)
}

// tableRow is one line of the table before its columns have been sized. The
// branch and the path are kept unstyled until then: they are the columns that
// may have to be cut, and cutting a string with colour in it would sever the
// escape codes.
type tableRow struct {
	marker      string
	branch      string
	branchStyle lipgloss.Style
	changes     string
	drift       string
	state       string
	path        string
	relative    bool // the path is relative to the worktree root
}

func buildRows(entries []worktree.Entry, root string) []tableRow {
	rows := make([]tableRow, 0, len(entries))
	for _, e := range entries {
		path, relative := pathText(e, root)
		branch, style := branchText(e)
		rows = append(rows, tableRow{
			marker:      marker(e),
			branch:      branch,
			branchStyle: style,
			changes:     changesCell(e),
			drift:       driftCell(e),
			state:       stateCell(e),
			path:        path,
			relative:    relative,
		})
	}
	return rows
}

// columnWidths fits the two columns that can grow — the branch and the path —
// into what the terminal leaves once the fixed ones have had their share. The
// path column is dropped before the branch is squeezed, because the branch is
// what identifies a row; a pathWidth of zero means there was no room for it.
func columnWidths(base string, rows []tableRow) (branchWidth, pathWidth int) {
	fixed := markerWidth
	for _, column := range []struct {
		header string
		pick   func(tableRow) string
	}{
		{"CHANGES", func(r tableRow) string { return r.changes }},
		{"VS " + strings.ToUpper(base), func(r tableRow) string { return r.drift }},
		{"STATE", func(r tableRow) string { return r.state }},
	} {
		fixed += widest(column.header, rows, column.pick) + 2
	}

	branchWidth = widest("BRANCH", rows, func(r tableRow) string { return r.branch })
	pathWidth = widest("PATH", rows, func(r tableRow) string { return r.path })

	available := terminalWidth() - fixed
	if branchWidth+2+pathWidth+2 <= available {
		return branchWidth, pathWidth
	}
	if left := available - (branchWidth + 2) - 2; left >= minPathWidth {
		return branchWidth, left
	}
	return max(min(branchWidth, available-2), minBranchWidth), 0
}

// widest is the width the column needs to show every row in full.
func widest(header string, rows []tableRow, pick func(tableRow) string) int {
	width := lipgloss.Width(header)
	for _, r := range rows {
		width = max(width, lipgloss.Width(pick(r)))
	}
	return width
}

// footnote names the directory the worktrees live in: to explain the paths
// that were shortened against it, or to stand in for the column when there was
// no room to show it at all.
func footnote(rows []tableRow, root string, pathWidth int) string {
	var label string
	switch {
	case pathWidth == 0:
		label = "worktrees in "
	case anyRelative(rows):
		label = "paths relative to "
	default:
		return ""
	}
	// Only the path is cut, so that the words explaining it always survive.
	room := terminalWidth() - markerWidth - lipgloss.Width(label)
	return "\n" + strings.Repeat(" ", markerWidth) + dim.Render(label+truncateLeft(Shorten(root), room))
}

func anyRelative(rows []tableRow) bool {
	for _, r := range rows {
		if r.relative {
			return true
		}
	}
	return false
}

func marker(e worktree.Entry) string {
	if e.Current {
		return cyan.Render("●")
	}
	return " "
}

// branchText is the branch name and how to style it, kept apart so the name
// can be cut to fit first.
func branchText(e worktree.Entry) (string, lipgloss.Style) {
	if e.Detached {
		return "(detached)", dim
	}
	if e.Current {
		return e.Branch, cyan.Bold(true)
	}
	return e.Branch, bold
}

func changesCell(e worktree.Entry) string {
	switch {
	case e.Changes == 0:
		return dim.Render("clean")
	case e.Changes == 1:
		return yellow.Render("1 change")
	default:
		return yellow.Render(fmt.Sprintf("%d changes", e.Changes))
	}
}

// driftCell shows the commits the branch is ahead and behind the base by.
func driftCell(e worktree.Entry) string {
	if e.Detached {
		return dim.Render("—")
	}
	var parts []string
	if e.Ahead > 0 {
		parts = append(parts, green.Render(fmt.Sprintf("↑%d", e.Ahead)))
	}
	if e.Behind > 0 {
		parts = append(parts, red.Render(fmt.Sprintf("↓%d", e.Behind)))
	}
	if len(parts) == 0 {
		return dim.Render("up to date")
	}
	return strings.Join(parts, " ")
}

// stateCell answers whether wurk rm would take this worktree without --force.
func stateCell(e worktree.Entry) string {
	switch {
	case e.Prunable:
		return red.Render("prunable")
	case e.Locked:
		return yellow.Render("locked")
	case e.Main:
		return dim.Render("main worktree")
	case e.Detached:
		return dim.Render("—")
	case e.State == git.Unstarted:
		return dim.Render("new")
	case e.State == git.Merged:
		return green.Render("merged")
	case e.State == git.Squashed:
		return green.Render("squash-merged")
	default:
		return yellow.Render("not merged")
	}
}

// pathText is the path as the column should read it: a worktree under the root
// by the part that differs, anything else by its full path. The second result
// says which, so the caller knows whether to explain the shortening.
func pathText(e worktree.Entry, root string) (string, bool) {
	if rel, err := filepath.Rel(resolve(root), resolve(e.Path)); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel, true
	}
	return Shorten(e.Path), false
}

// truncateLeft shortens a path from the front, keeping the end that names it.
func truncateLeft(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-width+1:])
}

// truncateRight shortens a branch name from the back, keeping the start that
// usually carries the ticket it belongs to.
func truncateRight(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// resolve follows symlinks so that paths from git and from the configuration
// can be compared.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// terminalWidth is the width the table has to fit into.
func terminalWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return fallbackWidth
}
