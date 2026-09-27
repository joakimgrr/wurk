package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joakimgrr/wurk/internal/config"
)

// env builds a repository and a worktree to set up, and returns both.
func env(t *testing.T) Env {
	t.Helper()
	base := t.TempDir()
	e := Env{
		Worktree: filepath.Join(base, "worktree"),
		Repo:     filepath.Join(base, "repo"),
		Branch:   "PROJ-1-a-branch",
		Base:     "origin/main",
	}
	for _, dir := range []string{e.Worktree, e.Repo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, s config.Setup, e Env) ([]Step, string, error) {
	t.Helper()
	var out bytes.Buffer
	steps, err := Run(s, e, &out, nil)
	return steps, out.String(), err
}

func TestCopyBringsFilesOver(t *testing.T) {
	e := env(t)
	write(t, filepath.Join(e.Repo, ".env"), "SECRET=1\n")
	write(t, filepath.Join(e.Repo, "certs/dev/key.pem"), "key\n")

	steps, _, err := run(t, config.Setup{Copy: []string{".env", "certs"}}, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if got := read(t, filepath.Join(e.Worktree, ".env")); got != "SECRET=1\n" {
		t.Errorf(".env = %q", got)
	}
	// Directories come over whole, nested files and all.
	if got := read(t, filepath.Join(e.Worktree, "certs/dev/key.pem")); got != "key\n" {
		t.Errorf("certs/dev/key.pem = %q", got)
	}
}

// TestCopySkipsWhatIsNotThere covers .env.local and friends, which exist on
// some machines and not others.
func TestCopySkipsWhatIsNotThere(t *testing.T) {
	e := env(t)
	steps, _, err := run(t, config.Setup{Copy: []string{".env.local"}}, e)
	if err != nil {
		t.Fatalf("a missing source should not be an error: %v", err)
	}
	if steps[0].Skipped == "" {
		t.Error("the step should say it was skipped")
	}
}

func TestCopyPreservesTheExecutableBit(t *testing.T) {
	e := env(t)
	script := filepath.Join(e.Repo, "tool.sh")
	write(t, script, "#!/bin/sh\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, err := run(t, config.Setup{Copy: []string{"tool.sh"}}, e); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(e.Worktree, "tool.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("mode = %v, want the executable bit kept", info.Mode())
	}
}

func TestLinkPointsAtTheRepository(t *testing.T) {
	e := env(t)
	if err := os.MkdirAll(filepath.Join(e.Repo, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, err := run(t, config.Setup{Link: []string{"node_modules"}}, e); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(e.Worktree, "node_modules"))
	if err != nil {
		t.Fatalf("not a symlink: %v", err)
	}
	if target != filepath.Join(e.Repo, "node_modules") {
		t.Errorf("links to %q, want the repository's copy", target)
	}
}

// TestLinkLeavesWhatIsAlreadyThere protects checked-in content from being
// replaced by a link to the main worktree.
func TestLinkLeavesWhatIsAlreadyThere(t *testing.T) {
	e := env(t)
	write(t, filepath.Join(e.Repo, "config"), "from the repo\n")
	write(t, filepath.Join(e.Worktree, "config"), "checked in\n")

	steps, _, err := run(t, config.Setup{Link: []string{"config"}}, e)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Skipped == "" {
		t.Error("the step should say it left the file alone")
	}
	if got := read(t, filepath.Join(e.Worktree, "config")); got != "checked in\n" {
		t.Errorf("the worktree's own file was replaced: %q", got)
	}
}

func TestRunExecutesInTheWorktreeWithTheEnvironment(t *testing.T) {
	e := env(t)
	_, out, err := run(t, config.Setup{Run: []string{
		`printf '%s %s %s\n' "$(basename "$PWD")" "$WURK_BRANCH" "$WURK_BASE"`,
		`printf '%s\n' "$(basename "$WURK_REPO")"`,
	}}, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "worktree PROJ-1-a-branch origin/main\nrepo\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestRunStopsAtTheFirstFailure is the contract the caller reports on: what
// came before stands, what came after did not happen.
func TestRunStopsAtTheFirstFailure(t *testing.T) {
	e := env(t)
	steps, out, err := run(t, config.Setup{Run: []string{
		"echo first",
		"exit 3",
		"echo never",
	}}, e)
	if err == nil {
		t.Fatal("expected the failing command to be reported")
	}
	if !strings.Contains(err.Error(), "exit 3") {
		t.Errorf("error = %q, want it to name the command", err)
	}
	if len(steps) != 2 {
		t.Errorf("got %d steps, want the two that were reached", len(steps))
	}
	if strings.Contains(out, "never") {
		t.Error("a command after the failure ran anyway")
	}
}

func TestScriptRunsLast(t *testing.T) {
	e := env(t)
	script := filepath.Join(e.Worktree, ".wurk/setup.sh")
	write(t, script, "#!/bin/sh\necho from the script\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	_, out, err := run(t, config.Setup{
		Run:    []string{"echo from run"},
		Script: ".wurk/setup.sh",
	}, e)
	if err != nil {
		t.Fatal(err)
	}
	if want := "from run\nfrom the script\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestScriptWithoutTheExecutableBit covers a script that was never chmodded,
// which git will happily check out.
func TestScriptWithoutTheExecutableBit(t *testing.T) {
	e := env(t)
	write(t, filepath.Join(e.Worktree, "setup.sh"), "echo ran anyway\n")

	_, out, err := run(t, config.Setup{Script: "setup.sh"}, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "ran anyway") {
		t.Errorf("output = %q", out)
	}
}

// TestScriptMissingIsSkipped keeps a branch that predates the script from
// being refused a worktree.
func TestScriptMissingIsSkipped(t *testing.T) {
	e := env(t)
	steps, _, err := run(t, config.Setup{Script: ".wurk/setup.sh"}, e)
	if err != nil {
		t.Fatalf("a missing script should not be an error: %v", err)
	}
	if steps[0].Skipped == "" {
		t.Error("the step should say it was skipped")
	}
}

// TestPathsMustStayInsideTheRepository refuses configuration that would reach
// out of the worktree.
func TestPathsMustStayInsideTheRepository(t *testing.T) {
	e := env(t)
	for _, path := range []string{"../outside", "/etc/passwd"} {
		if _, _, err := run(t, config.Setup{Copy: []string{path}}, e); err == nil {
			t.Errorf("copy %q was allowed", path)
		}
		if _, _, err := run(t, config.Setup{Link: []string{path}}, e); err == nil {
			t.Errorf("link %q was allowed", path)
		}
	}
}

func TestAnnounceReportsEveryStepInOrder(t *testing.T) {
	e := env(t)
	write(t, filepath.Join(e.Repo, ".env"), "x\n")

	var seen []string
	if _, err := Run(config.Setup{
		Copy: []string{".env"},
		Run:  []string{"true"},
	}, e, &bytes.Buffer{}, func(s Step) {
		seen = append(seen, s.Kind+" "+s.What)
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(seen, ", "); got != "copy .env, run true" {
		t.Errorf("announced %q", got)
	}
}

func TestStepsListsWithoutRunning(t *testing.T) {
	steps := Steps(config.Setup{
		Copy:   []string{".env"},
		Link:   []string{"node_modules"},
		Run:    []string{"npm ci"},
		Script: "s.sh",
	})
	var kinds []string
	for _, s := range steps {
		kinds = append(kinds, s.Kind)
	}
	if got := strings.Join(kinds, " "); got != "copy link run script" {
		t.Errorf("kinds = %q, want them in execution order", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
