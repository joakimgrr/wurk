package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirName(t *testing.T) {
	for in, want := range map[string]string{
		"PROJ-2222-work-on-login-system": "PROJ-2222-work-on-login-system",
		"feature/login":                  "feature-login",
		"a/b/c":                          "a-b-c",
	} {
		if got := dirName(in); got != want {
			t.Errorf("dirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRootDir(t *testing.T) {
	t.Setenv(EnvDir, "")
	repo := "/Users/me/dev/myrepo"

	if got, want := rootDir(repo, ""), "/Users/me/dev/myrepo-worktrees"; got != want {
		t.Errorf("default = %q, want %q", got, want)
	}
	if got, want := rootDir(repo, ".worktrees"), "/Users/me/dev/myrepo/.worktrees"; got != want {
		t.Errorf("relative override = %q, want %q", got, want)
	}
	if got, want := rootDir(repo, "/tmp/trees"), "/tmp/trees"; got != want {
		t.Errorf("absolute override = %q, want %q", got, want)
	}

	t.Setenv(EnvDir, "/tmp/from-env")
	if got, want := rootDir(repo, ""), "/tmp/from-env"; got != want {
		t.Errorf("env override = %q, want %q", got, want)
	}
}

// TestCreate drives the real git commands against a throwaway repository.
func TestCreate(t *testing.T) {
	repo := newRepo(t)
	trees := filepath.Join(t.TempDir(), "trees")
	chdir(t, repo)

	res, err := Create("PROJ-2222-work-on-login-system", "", trees)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := filepath.Join(trees, "PROJ-2222-work-on-login-system")
	if res.Path != want {
		t.Fatalf("Path = %q, want %q", res.Path, want)
	}
	if res.Base != "main" {
		t.Errorf("Base = %q, want main", res.Base)
	}
	if res.Existed {
		t.Error("Existed = true for a fresh branch")
	}
	if branch := gitOutput(t, want, "rev-parse", "--abbrev-ref", "HEAD"); branch != "PROJ-2222-work-on-login-system" {
		t.Fatalf("checked out %q", branch)
	}

	// Asking again is not an error: it points at the existing worktree.
	again, err := Create("PROJ-2222-work-on-login-system", "", trees)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !again.Existed {
		t.Error("Existed = false for a branch that is already checked out")
	}
	if !samePath(t, again.Path, want) {
		t.Fatalf("second Create gave %q, want %q", again.Path, want)
	}
}

// TestCreateExistingBranch checks out a branch that is already in the repo
// rather than failing or branching anew.
func TestCreateExistingBranch(t *testing.T) {
	repo := newRepo(t)
	runGit(t, repo, "branch", "existing")
	chdir(t, repo)

	res, err := Create("existing", "", filepath.Join(t.TempDir(), "trees"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Base != "" {
		t.Errorf("Base = %q, want empty for an existing branch", res.Base)
	}
	if branch := gitOutput(t, res.Path, "rev-parse", "--abbrev-ref", "HEAD"); branch != "existing" {
		t.Fatalf("checked out %q", branch)
	}
}

func TestCreateErrors(t *testing.T) {
	repo := newRepo(t)
	chdir(t, repo)
	trees := filepath.Join(t.TempDir(), "trees")

	if _, err := Create("has spaces", "", trees); err == nil {
		t.Error("expected an error for an invalid branch name")
	}
	if _, err := Create("fine-name", "no-such-revision", trees); err == nil {
		t.Error("expected an error for a missing base revision")
	}
}

// samePath compares two paths through the symlinks macOS puts in front of
// temporary directories: git reports worktrees by their resolved path.
func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return resolve(a) == resolve(b)
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitOutput(t, dir, args...)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
}
