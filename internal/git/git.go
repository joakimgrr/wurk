// Package git wraps the handful of git plumbing calls wurk needs.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Repo is a git repository, identified by the top level of its main worktree.
type Repo struct {
	// Root is the main worktree's top level, even when wurk was invoked from
	// inside a linked worktree.
	Root string
	// Current is the top level of the worktree wurk was invoked from.
	Current string
}

// Worktree is one entry of `git worktree list`.
type Worktree struct {
	Path     string
	Branch   string // empty when the worktree is detached
	Head     string
	Detached bool
	Main     bool // the repository's main worktree
	Locked   bool
	Prunable bool
}

// MergeState says how a branch relates to a base revision.
type MergeState int

const (
	// NotMerged means the branch holds work the base does not have.
	NotMerged MergeState = iota
	// Merged means the branch is an ancestor of the base.
	Merged
	// Squashed means the branch's tree is already in the base, as a squash
	// merge leaves it: the commits differ but the content landed.
	Squashed
	// Unstarted means the branch still sits on the base with no commits of
	// its own, which is not the same as having had work that landed.
	Unstarted
)

func (s MergeState) String() string {
	switch s {
	case Merged:
		return "merged"
	case Squashed:
		return "squash-merged"
	case Unstarted:
		return "new"
	default:
		return "not merged"
	}
}

// Open locates the repository containing dir. Standing in the directory that
// holds the worktrees counts too: it is where cd .. out of one lands, and it
// is not itself inside the repository.
func Open(dir string) (*Repo, error) {
	repo, err := openAt(dir)
	if err == nil {
		return repo, nil
	}
	if beside, found := worktreeBelow(dir); found {
		if repo, inner := openAt(beside); inner == nil {
			repo.Current = "" // beside the worktrees, standing in none of them
			return repo, nil
		}
	}
	return nil, err
}

func openAt(dir string) (*Repo, error) {
	common, err := run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, errors.New("not inside a git repository")
	}
	if bare, _ := run(dir, "rev-parse", "--is-bare-repository"); bare == "true" {
		return nil, errors.New("bare repositories are not supported yet")
	}
	repo := &Repo{Root: filepath.Dir(common)}
	repo.Current, _ = run(dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	return repo, nil
}

// worktreeBelow finds the linked worktrees sitting directly under dir and
// returns one of them. It reports false unless they all belong to the same
// repository, since a shared worktree directory may hold several.
func worktreeBelow(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var found, common string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		// A linked worktree keeps a .git file pointing at the repository. A
		// clone keeps a .git directory, and a directory of those is just a
		// directory of projects, not something to guess a repository from.
		if info, err := os.Stat(filepath.Join(path, ".git")); err != nil || info.IsDir() {
			continue
		}
		gitDir, err := run(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			continue
		}
		if common == "" {
			common, found = gitDir, path
			continue
		}
		if gitDir != common {
			return "", false
		}
	}
	return found, found != ""
}

// sameCommit reports whether two revisions point at the same commit.
func (r *Repo) sameCommit(a, b string) bool {
	left, err := run(r.Root, "rev-parse", a+"^{commit}")
	if err != nil {
		return false
	}
	right, err := run(r.Root, "rev-parse", b+"^{commit}")
	return err == nil && left == right
}

// CheckBranchName reports whether name is usable as a branch name.
func (r *Repo) CheckBranchName(name string) error {
	if name == "" {
		return errors.New("empty name")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid branch name %q: must not start with '-'", name)
	}
	if !ok(r.Root, "check-ref-format", "refs/heads/"+name) {
		return fmt.Errorf("invalid branch name %q", name)
	}
	return nil
}

// BranchExists reports whether a local branch of that name is already there.
func (r *Repo) BranchExists(branch string) bool {
	return ok(r.Root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
}

// RevExists reports whether rev resolves to a commit.
func (r *Repo) RevExists(rev string) bool {
	return ok(r.Root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
}

// Worktrees lists the repository's worktrees, the main one first.
func (r *Repo) Worktrees() ([]Worktree, error) {
	out, err := run(r.Root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var (
		list    []Worktree
		current *Worktree
	)
	flush := func() {
		if current != nil {
			current.Main = len(list) == 0
			list = append(list, *current)
			current = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			flush()
			current = &Worktree{Path: value}
		case "HEAD":
			if current != nil {
				current.Head = value
			}
		case "branch":
			if current != nil {
				current.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		case "detached":
			if current != nil {
				current.Detached = true
			}
		case "locked":
			if current != nil {
				current.Locked = true
			}
		case "prunable":
			if current != nil {
				current.Prunable = true
			}
		}
	}
	flush()
	return list, nil
}

// WorktreeForBranch returns the worktree that has branch checked out, if any.
func (r *Repo) WorktreeForBranch(branch string) (Worktree, bool) {
	list, err := r.Worktrees()
	if err != nil {
		return Worktree{}, false
	}
	for _, wt := range list {
		if wt.Branch == branch {
			return wt, true
		}
	}
	return Worktree{}, false
}

// AddWorktree checks branch out at path. When base is non-empty the branch is
// created at that revision; otherwise the existing branch is used.
func (r *Repo) AddWorktree(path, branch, base string) error {
	args := []string{"worktree", "add"}
	if base != "" {
		args = append(args, "-b", branch, path, base)
	} else {
		args = append(args, path, branch)
	}
	_, err := run(r.Root, args...)
	return err
}

// RemoveWorktree deletes a worktree. Without force, git refuses to remove one
// that has uncommitted changes.
func (r *Repo) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := run(r.Root, append(args, path)...)
	return err
}

// DeleteBranch deletes a local branch. Without force, git refuses to delete
// one whose commits are not merged.
func (r *Repo) DeleteBranch(branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := run(r.Root, "branch", flag, branch)
	return err
}

// DefaultBase picks the revision new branches start from: the remote's default
// branch when it is known, then a local main or master, then HEAD.
func (r *Repo) DefaultBase() string {
	if ref, err := run(r.Root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref
	}
	for _, candidate := range []string{"main", "master"} {
		if r.BranchExists(candidate) {
			return candidate
		}
	}
	return "HEAD"
}

// MergeStateOf reports whether branch has already landed in base, treating a
// squash merge as merged: the commits differ, but the content is there.
func (r *Repo) MergeStateOf(branch, base string) MergeState {
	if !r.RevExists(base) || !r.RevExists(branch) {
		return NotMerged
	}
	// Checked out a moment ago and not committed to yet: an ancestor of the
	// base, but calling that merged would claim work that never happened.
	if r.sameCommit(branch, base) {
		return Unstarted
	}
	if ok(r.Root, "merge-base", "--is-ancestor", branch, base) {
		return Merged
	}
	// A squash merge replays the branch's tree as one new commit, so the
	// branch is no ancestor. Build the commit that squashing would have
	// produced and ask git whether an equivalent is already in base.
	mergeBase, err := run(r.Root, "merge-base", base, branch)
	if err != nil {
		return NotMerged
	}
	tree, err := run(r.Root, "rev-parse", branch+"^{tree}")
	if err != nil {
		return NotMerged
	}
	squashed, err := run(r.Root, "commit-tree", tree, "-p", mergeBase, "-m", "squash probe")
	if err != nil {
		return NotMerged
	}
	// git cherry prefixes commits that base already contains with "-".
	cherry, err := run(r.Root, "cherry", base, squashed)
	if err == nil && strings.HasPrefix(cherry, "-") {
		return Squashed
	}
	return NotMerged
}

// Changes counts the uncommitted changes in a worktree, untracked files
// included.
func (r *Repo) Changes(worktreePath string) int {
	out, err := run(worktreePath, "status", "--porcelain")
	if err != nil || out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

// AheadBehind counts the commits branch has that base does not, and the other
// way round.
func (r *Repo) AheadBehind(branch, base string) (ahead, behind int) {
	out, err := run(r.Root, "rev-list", "--left-right", "--count", base+"..."+branch)
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return ahead, behind
}

// run executes git in dir and returns its trimmed stdout.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ok reports whether git exited cleanly, for the commands used as predicates.
func ok(dir string, args ...string) bool {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run() == nil
}
