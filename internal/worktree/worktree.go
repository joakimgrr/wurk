// Package worktree turns a name into a git worktree with a branch to match,
// and manages the worktrees once they exist.
package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joakimgrr/wurk/internal/config"
	"github.com/joakimgrr/wurk/internal/git"
)

// EnvDir overrides the directory new worktrees are created in.
const EnvDir = "WURK_WORKTREE_DIR"

// Source says which setting decided where worktrees go.
type Source string

const (
	SourceFlag    Source = "--dir"
	SourceEnv     Source = "$" + EnvDir
	SourceConfig  Source = "config file"
	SourceDefault Source = "default"
)

// Manager operates on one repository's worktrees.
type Manager struct {
	repo   *git.Repo
	root   string
	source Source
	base   string // cached, the revision branches are compared and created from
}

// Open finds the repository around the working directory and works out where
// its worktrees belong: the --dir override, then the environment, then the
// config file, then a sibling of the repository.
func Open(dirOverride string, cfg config.Config) (*Manager, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	repo, err := git.Open(cwd)
	if err != nil {
		return nil, err
	}
	root, source := resolveRoot(repo.Root, dirOverride, cfg)
	return &Manager{repo: repo, root: root, source: source}, nil
}

// Root is the directory new worktrees are created in, and Source says which
// setting put it there.
func (m *Manager) Root() (string, Source) { return m.root, m.source }

// Repo is the repository being managed.
func (m *Manager) Repo() *git.Repo { return m.repo }

// Base is the revision new branches start from and merges are judged against.
func (m *Manager) Base() string {
	if m.base == "" {
		m.base = m.repo.DefaultBase()
	}
	return m.base
}

// Result describes what Create did, so the caller can report it.
type Result struct {
	// Path is the worktree's directory.
	Path string
	// Base is the revision the branch was created from, empty when the branch
	// already existed.
	Base string
	// Existed reports that the branch was already checked out somewhere and
	// nothing new was created.
	Existed bool
}

// Create checks name out as a branch in its own worktree, creating both if
// needed. base overrides the revision to branch from.
func (m *Manager) Create(name, base string) (Result, error) {
	if err := m.repo.CheckBranchName(name); err != nil {
		return Result{}, err
	}
	if wt, found := m.repo.WorktreeForBranch(name); found {
		return Result{Path: wt.Path, Existed: true}, nil
	}

	path := filepath.Join(m.root, dirName(name))
	if _, err := os.Lstat(path); err == nil {
		return Result{}, fmt.Errorf("%s already exists but is not a worktree for %s", path, name)
	}

	if m.repo.BranchExists(name) {
		base = "" // check the existing branch out as it is
	} else if base == "" {
		base = m.Base()
	}
	if base != "" && !m.repo.RevExists(base) {
		return Result{}, fmt.Errorf("base revision %q not found", base)
	}
	if err := m.repo.AddWorktree(path, name, base); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Base: base}, nil
}

// RemoveResult describes what Remove did.
type RemoveResult struct {
	Branch string
	// Path is the worktree that was removed, empty when there was none.
	Path string
	// State is how the branch stood against Base when it was removed.
	State git.MergeState
	Base  string
	// Forced reports that the merge check was overridden.
	Forced bool
	// BranchDeleted reports that a branch was deleted, not only a worktree.
	BranchDeleted bool
}

// Remove deletes the worktree for name and the branch with it. Unless force is
// set, a branch whose work has not landed in the base is refused, and git
// refuses a worktree with uncommitted changes.
func (m *Manager) Remove(name string, force bool) (RemoveResult, error) {
	if err := m.repo.CheckBranchName(name); err != nil {
		return RemoveResult{}, err
	}
	base := m.Base()
	res := RemoveResult{Branch: name, Base: base, Forced: force}

	wt, hasWorktree := m.repo.WorktreeForBranch(name)
	hasBranch := m.repo.BranchExists(name)
	if !hasWorktree && !hasBranch {
		return res, fmt.Errorf("no worktree or branch named %s", name)
	}
	if name == base || name == strings.TrimPrefix(base, "origin/") {
		return res, fmt.Errorf("%s is the repository's default branch", name)
	}
	if hasWorktree {
		if wt.Main {
			return res, fmt.Errorf("%s is the repository's main worktree", wt.Path)
		}
		if samePath(wt.Path, m.repo.Current) {
			return res, fmt.Errorf("you are inside %s; cd out of it first", wt.Path)
		}
		res.Path = wt.Path
	}

	if hasBranch {
		res.State = m.repo.MergeStateOf(name, base)
		if !force && res.State == git.NotMerged {
			return res, fmt.Errorf("%s is not merged into %s; use --force to delete it anyway", name, base)
		}
	}

	if hasWorktree {
		// git refuses a dirty worktree too, but says it at much greater length.
		if n := m.repo.Changes(wt.Path); n > 0 && !force {
			noun := "changes"
			if n == 1 {
				noun = "change"
			}
			return res, fmt.Errorf("%s has %d uncommitted %s; commit them or use --force", name, n, noun)
		}
		if err := m.repo.RemoveWorktree(wt.Path, force); err != nil {
			return res, err
		}
	}
	if hasBranch {
		// A squash-merged branch is not an ancestor, so git's own check would
		// refuse it even though the work has landed.
		if err := m.repo.DeleteBranch(name, force || res.State == git.Squashed); err != nil {
			return res, err
		}
		res.BranchDeleted = true
	}
	return res, nil
}

// Entry is one worktree as List reports it.
type Entry struct {
	Branch   string
	Path     string
	Current  bool // the worktree wurk was invoked from
	Main     bool // the repository's main worktree
	Detached bool
	Locked   bool
	Prunable bool
	Changes  int // uncommitted changes, untracked files included
	Ahead    int // commits the branch has that the base does not
	Behind   int
	State    git.MergeState
}

// List reports every worktree in the repository with its status.
func (m *Manager) List() ([]Entry, error) {
	worktrees, err := m.repo.Worktrees()
	if err != nil {
		return nil, err
	}
	base := m.Base()
	entries := make([]Entry, 0, len(worktrees))
	for _, wt := range worktrees {
		e := Entry{
			Branch:   wt.Branch,
			Path:     wt.Path,
			Current:  samePath(wt.Path, m.repo.Current),
			Main:     wt.Main,
			Detached: wt.Detached,
			Locked:   wt.Locked,
			Prunable: wt.Prunable,
			Changes:  m.repo.Changes(wt.Path),
		}
		if wt.Branch != "" {
			e.Ahead, e.Behind = m.repo.AheadBehind(wt.Branch, base)
			e.State = m.repo.MergeStateOf(wt.Branch, base)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// resolveRoot works out the directory new worktrees are created in, and which
// setting decided it. A relative path is taken from the repository root.
func resolveRoot(repoRoot, override string, cfg config.Config) (string, Source) {
	dir, source := override, SourceFlag
	if dir == "" {
		dir, source = os.Getenv(EnvDir), SourceEnv
	}
	if dir == "" {
		dir, source = cfg.WorktreeDir, SourceConfig
	}
	if dir == "" {
		return filepath.Join(filepath.Dir(repoRoot), filepath.Base(repoRoot)+"-worktrees"), SourceDefault
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(repoRoot, dir), source
	}
	return filepath.Clean(dir), source
}

// dirName turns a branch name into a single directory name, so that
// feature/login becomes feature-login rather than a nested directory.
func dirName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// samePath compares paths through symlinks, because git reports worktrees by
// their resolved path and the shell may not.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return resolve(a) == resolve(b)
}
