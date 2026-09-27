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

// TestCreateInARepoWithNothingCommitted covers a repository straight out of
// git init. There is nothing to branch from, but git starts an unborn branch
// here just as checkout -b would, so wurk does too rather than refusing.
func TestCreateInARepoWithNothingCommitted(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	m, trees := newManager(t, dir)

	res, err := m.Create("first-branch", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !res.Orphan {
		t.Errorf("Orphan = false, want the branch reported as having no history")
	}
	if want := filepath.Join(trees, "first-branch"); res.Path != want {
		t.Errorf("Path = %q, want %q", res.Path, want)
	}
	if branch := gitOutput(t, res.Path, "branch", "--show-current"); branch != "first-branch" {
		t.Errorf("checked out %q, want first-branch", branch)
	}

	// The point of it: the worktree is somewhere work can start.
	commit(t, res.Path, "app.js", "first commit")
	if branch := gitOutput(t, res.Path, "rev-parse", "--abbrev-ref", "HEAD"); branch != "first-branch" {
		t.Errorf("after committing, on %q", branch)
	}
}

// TestRemoveAnUnbornBranch covers finishing with such a worktree before
// anything was committed in it, when there is no branch ref to delete yet.
func TestRemoveAnUnbornBranch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	m, _ := newManager(t, dir)

	res, err := m.Create("never-used", "")
	if err != nil {
		t.Fatal(err)
	}
	// Nothing was committed, so there is nothing to lose and nothing to ask.
	if c := m.Inspect("never-used", res.Path); c.Any() {
		t.Errorf("an unborn branch raised %+v, want nothing", c)
	}
	if _, err := m.Remove("never-used", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Error("the worktree is still there")
	}
}

// TestEnsureRemovableGuards covers the refusals no flag gets past. "wurk done"
// settles these before asking anything, so that it never puts a question about
// a worktree the answer could not apply to.
func TestEnsureRemovableGuards(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)

	if err := m.EnsureRemovable("main"); err == nil {
		t.Error("the default branch should be refused")
	}
	if err := m.EnsureRemovable("never-existed"); err == nil {
		t.Error("an unknown name should be refused")
	}
	if _, err := m.Create("ordinary", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureRemovable("ordinary"); err != nil {
		t.Errorf("an ordinary worktree should be removable: %v", err)
	}
}

// TestEnsureRemovableRefusesTheMainWorktree covers the main worktree sitting
// on a branch that is not the default one, where the default-branch check
// would not catch it.
func TestEnsureRemovableRefusesTheMainWorktree(t *testing.T) {
	repo := newRepo(t)
	runGit(t, repo, "switch", "-c", "parked")
	m, _ := newManager(t, repo)

	err := m.EnsureRemovable("parked")
	if err == nil {
		t.Fatal("expected the main worktree to be refused")
	}
	if !strings.Contains(err.Error(), "main worktree") {
		t.Errorf("error = %q, want it to name the main worktree", err)
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

// TestUnstartedBranchIsNotCalledMerged covers a branch checked out a moment
// ago. It is an ancestor of the base, which is what "merged" is normally read
// off, but nothing has landed because nothing was done.
func TestUnstartedBranchIsNotCalledMerged(t *testing.T) {
	m, _ := newManager(t, newRepo(t))
	if _, err := m.Create("fresh", ""); err != nil {
		t.Fatal(err)
	}

	entries, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	fresh, found := entryFor(entries, "fresh")
	if !found {
		t.Fatal("the new worktree is missing from the listing")
	}
	if fresh.State != git.Unstarted {
		t.Errorf("State = %v, want new", fresh.State)
	}

	// Nothing to lose, so it still goes without --force.
	out, err := m.Remove("fresh", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if out.State != git.Unstarted {
		t.Errorf("State = %v, want new", out.State)
	}
}

// TestCommittingLeavesUnstarted is the other half: once there is a commit, the
// branch is unmerged work and gets the protection that comes with it.
func TestCommittingLeavesUnstarted(t *testing.T) {
	m, _ := newManager(t, newRepo(t))
	res, err := m.Create("started", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, res.Path, "a.txt", "some work")

	entries, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	started, _ := entryFor(entries, "started")
	if started.State != git.NotMerged {
		t.Errorf("State = %v, want not merged", started.State)
	}
	if _, err := m.Remove("started", false); err == nil {
		t.Error("expected Remove to protect a branch that has commits")
	}
}

// TestNewBranchDoesNotTrackTheBase guards against git's autoSetupMerge. A
// branch started from origin/main would otherwise adopt it as its upstream,
// and a later plain push can then land the branch's commits on main instead of
// creating a branch to open a pull request from.
func TestNewBranchDoesNotTrackTheBase(t *testing.T) {
	repo := newRepoWithRemote(t)
	m, _ := newManager(t, repo)

	res, err := m.Create("feature", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Base != "origin/main" {
		t.Fatalf("branched from %q, want origin/main, or this proves nothing", res.Base)
	}
	if upstream := gitConfig(t, repo, "branch.feature.merge"); upstream != "" {
		t.Errorf("the new branch tracks %q, want no upstream at all", upstream)
	}
	if remote := gitConfig(t, repo, "branch.feature.remote"); remote != "" {
		t.Errorf("the new branch has remote %q, want none", remote)
	}
}

// TestRollbackUndoesACreate covers the repositories configured to want nothing
// left behind when setup fails: the worktree goes, and the branch wurk made
// with it, even though setup has left untracked files in the way.
func TestRollbackUndoesACreate(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("rolled-back", "")
	if err != nil {
		t.Fatal(err)
	}
	// What a half-finished setup leaves behind, which git would refuse to
	// discard without being forced.
	if err := os.WriteFile(filepath.Join(res.Path, ".env"), []byte("copied\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.Rollback(res, "rolled-back"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Error("the worktree is still there")
	}
	if branchExists(t, repo, "rolled-back") {
		t.Error("the branch is still there")
	}
}

// TestRollbackKeepsABranchItDidNotCreate covers wurk having only added a
// worktree to a branch that was already in the repository.
func TestRollbackKeepsABranchItDidNotCreate(t *testing.T) {
	repo := newRepo(t)
	runGit(t, repo, "branch", "was-here-first")
	m, _ := newManager(t, repo)
	res, err := m.Create("was-here-first", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Base != "" {
		t.Fatalf("Base = %q, want empty for an existing branch", res.Base)
	}

	if err := m.Rollback(res, "was-here-first"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Error("the worktree is still there")
	}
	if !branchExists(t, repo, "was-here-first") {
		t.Error("a branch wurk did not create was deleted anyway")
	}
}

// TestInspectReportsWhatWouldBeLost is what "wurk done" puts to the person
// before it removes anything.
func TestInspectReportsWhatWouldBeLost(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("halfway", "")
	if err != nil {
		t.Fatal(err)
	}

	// Fresh: nothing to lose, nothing to ask about.
	if c := m.Inspect("halfway", res.Path); c.Any() {
		t.Errorf("a fresh worktree raised %+v", c)
	}

	commit(t, res.Path, "a.txt", "work")
	if err := os.WriteFile(filepath.Join(res.Path, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := m.Inspect("halfway", res.Path)
	if !c.Any() {
		t.Fatal("unmerged work with uncommitted changes should raise something")
	}
	got := c.Describe()
	if !strings.Contains(got, "not merged into main") || !strings.Contains(got, "1 uncommitted change") {
		t.Errorf("Describe() = %q, want both concerns named", got)
	}
	if strings.Contains(got, "1 uncommitted changes") {
		t.Errorf("Describe() = %q, want the singular", got)
	}
}

// TestInspectSaysNothingOnceMerged keeps "wurk done" quiet in the ordinary
// case, where the work has landed and there is nothing to confirm.
func TestInspectSaysNothingOnceMerged(t *testing.T) {
	repo := newRepo(t)
	m, _ := newManager(t, repo)
	res, err := m.Create("landed", "")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, res.Path, "a.txt", "work")
	runGit(t, repo, "merge", "--no-ff", "-m", "merge", "landed")

	if c := m.Inspect("landed", res.Path); c.Any() {
		t.Errorf("a merged, clean worktree raised %+v", c)
	}
}

// TestRemoveTakesTheGroundYouStandOn is what "wurk done" is for. The caller
// moves the shell afterwards, so the removal itself does not object.
func TestRemoveTakesTheGroundYouStandOn(t *testing.T) {
	repo := newRepo(t)
	m, trees := newManager(t, repo)
	res, err := m.Create("stand-here", "")
	if err != nil {
		t.Fatal(err)
	}
	chdir(t, res.Path)
	inside, err := Open(trees, config.Config{})
	if err != nil {
		t.Fatal(err)
	}

	if !inside.IsCurrent(res.Path) {
		t.Fatal("IsCurrent should recognise the worktree we chdir'd into")
	}
	if _, err := inside.Remove("stand-here", true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Error("the worktree is still there")
	}
}

// TestOpenFromTheWorktreeDirectory covers where "cd .." out of a worktree
// lands: beside the worktrees, which is not itself inside the repository.
func TestOpenFromTheWorktreeDirectory(t *testing.T) {
	repo := newRepo(t)
	m, trees := newManager(t, repo)
	if _, err := m.Create("beside", ""); err != nil {
		t.Fatal(err)
	}

	chdir(t, trees)
	outside, err := Open("", config.Config{})
	if err != nil {
		t.Fatalf("Open from the worktree directory: %v", err)
	}
	if !samePath(outside.Repo().Root, repo) {
		t.Errorf("found repository %q, want %q", outside.Repo().Root, repo)
	}
	// Standing in none of them, so none is refused for being the current one.
	if _, err := outside.Remove("beside", false); err != nil {
		t.Errorf("Remove from the worktree directory: %v", err)
	}
}

// TestOpenIgnoresADirectoryOfClones keeps the search above from guessing: a
// directory of checked-out projects is not a worktree directory.
func TestOpenIgnoresADirectoryOfClones(t *testing.T) {
	dir := t.TempDir()
	newRepoIn(t, filepath.Join(dir, "alpha"))
	newRepoIn(t, filepath.Join(dir, "beta"))
	chdir(t, dir)

	if _, err := Open("", config.Config{}); err == nil {
		t.Error("a directory of clones was taken for a worktree directory")
	}
}

// TestOpenRefusesAmbiguousWorktreeDirectory covers a shared worktree directory
// holding worktrees of more than one repository.
func TestOpenRefusesAmbiguousWorktreeDirectory(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	for _, name := range []string{"one", "two"} {
		repo := newRepo(t)
		chdir(t, repo)
		m, err := Open(shared, config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Create("wt-"+name, ""); err != nil {
			t.Fatal(err)
		}
	}
	chdir(t, shared)

	if _, err := Open("", config.Config{}); err == nil {
		t.Error("a worktree directory shared by two repositories should be ambiguous")
	}
}

func entryFor(entries []Entry, branch string) (Entry, bool) {
	for _, e := range entries {
		if e.Branch == branch {
			return e, true
		}
	}
	return Entry{}, false
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
	return newRepoIn(t, t.TempDir())
}

// newRepoWithRemote gives the repository an origin, so that the default base
// is a remote-tracking branch as it is in real use.
func newRepoWithRemote(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	origin := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, repo, "init", "--bare", origin)
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "push", "-u", "origin", "main")
	runGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return repo
}

func gitConfig(t *testing.T, dir, key string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func newRepoIn(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
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
