package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joakimgrr/wurk/internal/config"
	"github.com/joakimgrr/wurk/internal/git"
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

// TestResolveRoot pins the precedence: flag, environment, config file,
// built-in default.
func TestResolveRoot(t *testing.T) {
	const repo = "/Users/me/dev/myrepo"
	cfg := config.Config{WorktreeDir: "/from/config"}

	for _, tc := range []struct {
		name       string
		override   string
		env        string
		cfg        config.Config
		wantDir    string
		wantSource Source
	}{
		{"flag wins", "/from/flag", "/from/env", cfg, "/from/flag", SourceFlag},
		{"env beats config", "", "/from/env", cfg, "/from/env", SourceEnv},
		{"config beats default", "", "", cfg, "/from/config", SourceConfig},
		{"default", "", "", config.Config{}, "/Users/me/dev/myrepo-worktrees", SourceDefault},
		{"relative to the repo", "", "", config.Config{WorktreeDir: ".worktrees"}, repo + "/.worktrees", SourceConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvDir, tc.env)
			dir, source := resolveRoot(repo, tc.override, tc.cfg)
			if dir != tc.wantDir || source != tc.wantSource {
				t.Errorf("resolveRoot = %q from %q, want %q from %q", dir, source, tc.wantDir, tc.wantSource)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	m, trees := newManager(t, newRepo(t))

	res, err := m.Create("PROJ-2222-work-on-login-system", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := filepath.Join(trees, "PROJ-2222-work-on-login-system")
	if res.Path != want {
		t.Fatalf("Path = %q, want %q", res.Path, want)
	}
	if res.Base != "main" || res.Existed {
		t.Errorf("Base = %q, Existed = %v; want main, false", res.Base, res.Existed)
	}
	if branch := gitOutput(t, want, "rev-parse", "--abbrev-ref", "HEAD"); branch != "PROJ-2222-work-on-login-system" {
		t.Fatalf("checked out %q", branch)
	}

	// Asking again is not an error: it points at the existing worktree.
	again, err := m.Create("PROJ-2222-work-on-login-system", "")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !again.Existed || !samePath(again.Path, want) {
		t.Fatalf("second Create = %+v, want the existing worktree at %s", again, want)
	}
}

func TestCreateExistingBranch(t *testing.T) {
	repo := newRepo(t)
	runGit(t, repo, "branch", "existing")
	m, _ := newManager(t, repo)

	res, err := m.Create("existing", "")
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
	m, _ := newManager(t, newRepo(t))

	if _, err := m.Create("has spaces", ""); err == nil {
		t.Error("expected an error for an invalid branch name")
	}
	if _, err := m.Create("fine-name", "no-such-revision"); err == nil {
		t.Error("expected an error for a missing base revision")
	}
}

// TestRemoveMerged deletes a branch that was merged the ordinary way.
func TestRemoveMerged(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("merged-work", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, res.Path, "a.txt", "add a")
	runGit(t, repo, "merge", "--no-ff", "-m", "merge", "merged-work")

	out, err := m.Remove("merged-work", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if out.State != git.Merged || !out.BranchDeleted {
		t.Errorf("Remove = %+v, want a merged branch deleted", out)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Error("the worktree directory is still there")
	}
	if branchExists(t, repo, "merged-work") {
		t.Error("the branch is still there")
	}
}

// TestRemoveSquashMerged covers the GitHub-style merge, where the branch is no
// ancestor of the base but its content has landed all the same.
func TestRemoveSquashMerged(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("squashed-work", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, res.Path, "b.txt", "add b")
	commit(t, res.Path, "b2.txt", "add more b")
	runGit(t, repo, "merge", "--squash", "squashed-work")
	runGit(t, repo, "commit", "-m", "squash squashed-work")

	out, err := m.Remove("squashed-work", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if out.State != git.Squashed {
		t.Errorf("State = %v, want squash-merged", out.State)
	}
	if branchExists(t, repo, "squashed-work") {
		t.Error("the branch is still there")
	}
}

// TestRemoveUnmerged is the safety net: work that has not landed is refused
// until --force says otherwise.
func TestRemoveUnmerged(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("unmerged-work", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, res.Path, "c.txt", "work nobody merged")

	_, err = m.Remove("unmerged-work", false)
	if err == nil {
		t.Fatal("expected Remove to refuse an unmerged branch")
	}
	if !strings.Contains(err.Error(), "not merged") {
		t.Errorf("error = %q, want it to say the branch is not merged", err)
	}
	if !branchExists(t, repo, "unmerged-work") {
		t.Fatal("the refused branch was deleted anyway")
	}

	out, err := m.Remove("unmerged-work", true)
	if err != nil {
		t.Fatalf("forced Remove: %v", err)
	}
	if out.State != git.NotMerged || !out.Forced {
		t.Errorf("Remove = %+v, want an unmerged branch force-deleted", out)
	}
	if branchExists(t, repo, "unmerged-work") {
		t.Error("the branch survived --force")
	}
}

// TestRemoveUncommittedChanges leans on git's own refusal to throw away work
// that was never committed.
func TestRemoveUncommittedChanges(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("dirty-work", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res.Path, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, res.Path, "add", "scratch.txt")

	_, err = m.Remove("dirty-work", false)
	if err == nil {
		t.Fatal("expected Remove to refuse a worktree with uncommitted changes")
	}
	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error = %q, want it to name the uncommitted changes", err)
	}
	if _, err := m.Remove("dirty-work", true); err != nil {
		t.Fatalf("forced Remove: %v", err)
	}
}

func TestRemoveGuards(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)

	if _, err := m.Remove("main", false); err == nil {
		t.Error("expected Remove to refuse the default branch")
	}
	if _, err := m.Remove("never-existed", false); err == nil {
		t.Error("expected Remove to refuse an unknown name")
	}

	res, err := m.Create("stand-here", "")
	if err != nil {
		t.Fatal(err)
	}
	chdir(t, res.Path)
	inside, err := Open("", config.Config{WorktreeDir: filepath.Dir(res.Path)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inside.Remove("stand-here", true); err == nil || !strings.Contains(err.Error(), "inside") {
		t.Errorf("error = %v, want a refusal to delete the worktree we are standing in", err)
	}
}

// TestRemoveBranchWithoutWorktree covers a branch left behind after its
// worktree is gone.
func TestRemoveBranchWithoutWorktree(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	runGit(t, repo, "branch", "orphan")

	out, err := m.Remove("orphan", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if out.Path != "" {
		t.Errorf("Path = %q, want empty when there is no worktree", out.Path)
	}
	if !out.BranchDeleted || branchExists(t, repo, "orphan") {
		t.Error("the branch was not deleted")
	}
}

func TestList(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("listed-work", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, res.Path, "d.txt", "ahead by one")
	if err := os.WriteFile(filepath.Join(res.Path, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want the main worktree and one more", len(entries))
	}
	if !entries[0].Main || entries[0].Branch != "main" {
		t.Errorf("first entry = %+v, want the main worktree", entries[0])
	}

	got := entries[1]
	switch {
	case got.Branch != "listed-work":
		t.Errorf("Branch = %q", got.Branch)
	case got.Ahead != 1 || got.Behind != 0:
		t.Errorf("Ahead/Behind = %d/%d, want 1/0", got.Ahead, got.Behind)
	case got.Changes != 1:
		t.Errorf("Changes = %d, want 1 untracked file", got.Changes)
	case got.State != git.NotMerged:
		t.Errorf("State = %v, want not merged", got.State)
	case got.Main:
		t.Error("Main = true for a linked worktree")
	}
}

// newManager points a manager at a fresh worktree directory and moves the test
// into the repository, which is where wurk is normally run from.
func newManager(t *testing.T, repo string) (*Manager, string) {
	t.Helper()
	t.Setenv(EnvDir, "")
	trees := filepath.Join(t.TempDir(), "trees")
	chdir(t, repo)
	m, err := Open(trees, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return m, trees
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

func commit(t *testing.T, dir, file, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(message+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-m", message)
}

func branchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repo
	return cmd.Run() == nil
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
