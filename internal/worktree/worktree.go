// Package worktree turns a name into a git worktree with a branch to match.
package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wurk/internal/git"
)

// EnvDir overrides the directory new worktrees are created in.
const EnvDir = "WURK_WORKTREE_DIR"

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
// needed. base and dir are optional overrides for the revision to branch from
// and the directory that holds worktrees.
func Create(name, base, dir string) (Result, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{}, err
	}
	repo, err := git.Open(cwd)
	if err != nil {
		return Result{}, err
	}
	if err := repo.CheckBranchName(name); err != nil {
		return Result{}, err
	}

	if path, found := repo.WorktreeForBranch(name); found {
		return Result{Path: path, Existed: true}, nil
	}

	path := filepath.Join(rootDir(repo.Root, dir), dirName(name))
	if _, err := os.Lstat(path); err == nil {
		return Result{}, fmt.Errorf("%s already exists but is not a worktree for %s", path, name)
	}

	if repo.BranchExists(name) {
		base = "" // check the existing branch out as it is
	} else if base == "" {
		base = repo.DefaultBase()
	}
	if base != "" && !repo.RevExists(base) {
		return Result{}, fmt.Errorf("base revision %q not found", base)
	}
	if err := repo.AddWorktree(path, name, base); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Base: base}, nil
}

// rootDir is the directory new worktrees are created in: the override, then
// the environment, then a sibling of the repository named <repo>-worktrees.
// A relative override is resolved against the repository root.
func rootDir(repoRoot, override string) string {
	if override == "" {
		override = os.Getenv(EnvDir)
	}
	if override == "" {
		return filepath.Join(filepath.Dir(repoRoot), filepath.Base(repoRoot)+"-worktrees")
	}
	if strings.HasPrefix(override, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			override = filepath.Join(home, override[2:])
		}
	}
	if !filepath.IsAbs(override) {
		return filepath.Join(repoRoot, override)
	}
	return filepath.Clean(override)
}

// dirName turns a branch name into a single directory name, so that
// feature/login becomes feature-login rather than a nested directory.
func dirName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}
