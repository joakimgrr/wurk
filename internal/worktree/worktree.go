// Package worktree turns a name into a git worktree with a branch to match,
// and manages the worktrees once they exist.
package worktree

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joakimgrr/wurk/internal/config"
	"github.com/joakimgrr/wurk/internal/git"
	"github.com/joakimgrr/wurk/internal/setup"
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
	cfg    config.Config
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
	return &Manager{repo: repo, root: root, source: source, cfg: cfg}, nil
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

// Fetch brings the remote-tracking branches up to date, so that work merged
// elsewhere is known to have been merged here. A repository with no remote has
// nothing to do, and reports no error.
func (m *Manager) Fetch() error {
	if !m.repo.HasRemote() {
		return nil
	}
	return m.repo.Fetch()
}

// PruneRecords forgets worktrees whose directories are gone.
func (m *Manager) PruneRecords() error {
	return m.repo.PruneWorktreeRecords()
}

// KeepReason says why a worktree is not one to tidy away, and is empty for one
// that is. A worktree the caller is standing in is kept whatever its state:
// removing it would leave the shell nowhere, which is "wurk done"'s job to
// handle, not this one's.
func (m *Manager) KeepReason(e Entry) string {
	switch {
	case e.Main:
		return "the repository's main worktree"
	case e.Detached:
		return "no branch checked out"
	case e.Locked:
		return "locked"
	case e.Prunable:
		return "its directory is missing"
	case e.Changes == 1:
		return "1 uncommitted change"
	case e.Changes > 1:
		return fmt.Sprintf("%d uncommitted changes", e.Changes)
	case e.State == git.NotMerged:
		return "not merged into " + m.Base()
	case e.Current:
		return "you are standing in it"
	}
	return ""
}

// Locate resolves the worktree a command should act on: the one named, or
// else the one the caller is standing in.
func (m *Manager) Locate(args []string) (branch, path string, err error) {
	if len(args) == 1 {
		wt, found := m.repo.WorktreeForBranch(args[0])
		if !found {
			return "", "", fmt.Errorf("no worktree for %s", args[0])
		}
		return wt.Branch, wt.Path, nil
	}
	if m.repo.Current == "" {
		return "", "", errors.New("name a worktree, or run this from inside one")
	}
	worktrees, err := m.repo.Worktrees()
	if err != nil {
		return "", "", err
	}
	for _, wt := range worktrees {
		if samePath(wt.Path, m.repo.Current) {
			if wt.Branch == "" {
				return "", "", errors.New("this worktree has no branch checked out")
			}
			return wt.Branch, wt.Path, nil
		}
	}
	return "", "", errors.New("name a worktree, or run this from inside one")
}

// Setup is what this repository's configuration says to do with a new
// worktree. It is empty when nothing is configured.
func (m *Manager) Setup() config.Setup {
	return m.cfg.SetupFor(m.repo.Root)
}

// OnFailure says what to do with a worktree of this repository whose setup
// did not finish.
func (m *Manager) OnFailure() config.OnFailure {
	return m.cfg.OnFailureFor(m.repo.Root)
}

// Rollback undoes a Create whose setup failed: the worktree goes, forcibly,
// because setup will have left files behind that git would otherwise refuse to
// discard. The branch goes with it only when this call is what created it.
func (m *Manager) Rollback(res Result, branch string) error {
	if err := m.repo.RemoveWorktree(res.Path, true); err != nil {
		return err
	}
	if res.Base == "" {
		return nil // the branch was there before; it is not ours to delete
	}
	return m.repo.DeleteBranch(branch, true)
}

// RunSetup prepares a worktree that already exists, streaming what the
// commands print to out and announcing each step as it is reached.
func (m *Manager) RunSetup(path, branch string, out io.Writer, announce func(setup.Step)) ([]setup.Step, error) {
	steps := m.Setup()
	if steps.IsEmpty() {
		return nil, nil
	}
	return setup.Run(steps, setup.Env{
		Worktree: path,
		Repo:     m.repo.Root,
		Branch:   branch,
		Base:     m.Base(),
	}, out, announce)
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
	// Orphan reports that the branch was started with no history behind it,
	// there having been nothing in the repository to branch from.
	Orphan bool
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
		if !m.repo.HasCommits() {
			// Nothing to branch from yet, which is no reason to refuse: git
			// starts an unborn branch here, just as checkout -b would.
			if err := m.repo.AddOrphanWorktree(path, name); err != nil {
				return Result{}, err
			}
			return Result{Path: path, Orphan: true}, nil
		}
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

// Concerns is what stands in the way of being finished with a worktree.
type Concerns struct {
	State   git.MergeState
	Base    string
	Changes int
}

// Any reports whether there is anything here worth asking about.
func (c Concerns) Any() bool {
	return c.State == git.NotMerged || c.Changes > 0
}

// Describe phrases the concerns for a question put to the person.
func (c Concerns) Describe() string {
	var parts []string
	if c.State == git.NotMerged {
		parts = append(parts, "is not merged into "+c.Base)
	}
	switch {
	case c.Changes == 1:
		parts = append(parts, "has 1 uncommitted change")
	case c.Changes > 1:
		parts = append(parts, fmt.Sprintf("has %d uncommitted changes", c.Changes))
	}
	return strings.Join(parts, " and ")
}

// Inspect reports what would be lost by removing a worktree.
func (m *Manager) Inspect(branch, path string) Concerns {
	return Concerns{
		State:   m.repo.MergeStateOf(branch, m.Base()),
		Base:    m.Base(),
		Changes: m.repo.Changes(path),
	}
}

// EnsureRemovable reports the reason a worktree must not be removed, if there
// is one. These are the guards no amount of forcing gets past, so a caller
// that is about to ask the person a question should ask this first.
func (m *Manager) EnsureRemovable(name string) error {
	if err := m.repo.CheckBranchName(name); err != nil {
		return err
	}
	wt, hasWorktree := m.repo.WorktreeForBranch(name)
	if !hasWorktree && !m.repo.BranchExists(name) {
		return fmt.Errorf("no worktree or branch named %s", name)
	}
	if hasWorktree && wt.Main {
		return fmt.Errorf("%s is the repository's main worktree", wt.Path)
	}
	base := m.Base()
	if name == base || name == strings.TrimPrefix(base, "origin/") {
		return fmt.Errorf("%s is the repository's default branch", name)
	}
	return nil
}

// IsCurrent reports whether path is the worktree the caller is standing in.
func (m *Manager) IsCurrent(path string) bool {
	return samePath(path, m.repo.Current)
}

// Remove deletes the worktree for name and the branch with it. Unless force is
// set, a branch whose work has not landed in the base is refused, and so is a
// worktree with uncommitted changes.
//
// The worktree the caller is standing in is fair game: the caller is expected
// to move the shell somewhere that still exists afterwards.
func (m *Manager) Remove(name string, force bool) (RemoveResult, error) {
	if err := m.repo.CheckBranchName(name); err != nil {
		return RemoveResult{}, err
	}
	base := m.Base()
	res := RemoveResult{Branch: name, Base: base, Forced: force}

	if err := m.EnsureRemovable(name); err != nil {
		return res, err
	}
	wt, hasWorktree := m.repo.WorktreeForBranch(name)
	hasBranch := m.repo.BranchExists(name)
	if hasWorktree {
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
		// Getting here means the work has landed or force said to go anyway,
		// judged against the base branch. Git's own -d check judges against
		// HEAD instead, which lags the base whenever the base is a remote
		// branch — so it would refuse exactly the branches worth tidying, and
		// a squash-merged one is no ancestor of anything to begin with.
		if err := m.repo.DeleteBranch(name, true); err != nil {
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
