// Package git wraps the handful of git plumbing calls wurk needs.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a git repository, identified by the top level of its main worktree.
type Repo struct {
	// Root is the main worktree's top level, even when wurk was invoked from
	// inside a linked worktree.
	Root string
}

// Open locates the repository containing dir.
func Open(dir string) (*Repo, error) {
	common, err := run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, errors.New("not inside a git repository")
	}
	if bare, _ := run(dir, "rev-parse", "--is-bare-repository"); bare == "true" {
		return nil, errors.New("bare repositories are not supported yet")
	}
	return &Repo{Root: filepath.Dir(common)}, nil
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

// WorktreeForBranch returns the path of the worktree that has branch checked
// out, if any.
func (r *Repo) WorktreeForBranch(branch string) (string, bool) {
	out, err := run(r.Root, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false
	}
	var path string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path, true
		}
	}
	return "", false
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
