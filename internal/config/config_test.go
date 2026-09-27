package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/tmp/xdg/wurk/config.toml"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WorktreeDir != "" {
		t.Errorf("WorktreeDir = %q, want empty", cfg.WorktreeDir)
	}
}

func TestLoadReadsWorktreeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write(t, dir, "worktree_dir = \"~/dev/trees\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := "~/dev/trees"; cfg.WorktreeDir != want {
		t.Errorf("WorktreeDir = %q, want %q", cfg.WorktreeDir, want)
	}
}

func TestLoadReportsBadSyntax(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write(t, dir, "worktree_dir = not quoted\n")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for invalid TOML")
	}
}

// TestSetupForMatchesTheRepository covers the key people actually write: a
// path with a ~ in it, matched against the absolute path git reports.
func TestSetupForMatchesTheRepository(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := filepath.Join(home, "dev", "myrepo")

	cfg := Config{Repos: map[string]Repo{
		"~/dev/myrepo":  {Setup: Setup{Run: []string{"npm ci"}}},
		"~/dev/another": {Setup: Setup{Run: []string{"wrong one"}}},
	}}

	got := cfg.SetupFor(repo)
	if len(got.Run) != 1 || got.Run[0] != "npm ci" {
		t.Errorf("SetupFor(%q) = %+v, want the ~/dev/myrepo entry", repo, got)
	}
	if steps := cfg.SetupFor(filepath.Join(home, "dev", "unconfigured")); !steps.IsEmpty() {
		t.Errorf("an unconfigured repository got %+v, want nothing", steps)
	}
}

func TestSetupForAcceptsAnAbsoluteKey(t *testing.T) {
	repo := t.TempDir()
	cfg := Config{Repos: map[string]Repo{
		repo + "/": {Setup: Setup{Run: []string{"yes"}}},
	}}
	if cfg.SetupFor(repo).IsEmpty() {
		t.Error("a key with a trailing slash should still match")
	}
}

// TestOnFailureFor pins the order: what the repository says, then what the
// file says, then keeping the worktree.
func TestOnFailureFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := filepath.Join(home, "dev", "myrepo")
	other := filepath.Join(home, "dev", "other")

	for _, tc := range []struct {
		name string
		cfg  Config
		path string
		want OnFailure
	}{
		{"nothing set", Config{}, repo, Remove},
		{"the file decides", Config{SetupOnFailure: Keep}, repo, Keep},
		{
			"the repository overrides the file",
			Config{
				SetupOnFailure: Keep,
				Repos:          map[string]Repo{"~/dev/myrepo": {Setup: Setup{OnFailure: Remove}}},
			},
			repo, Remove,
		},
		{
			"another repository still follows the file",
			Config{
				SetupOnFailure: Keep,
				Repos:          map[string]Repo{"~/dev/myrepo": {Setup: Setup{OnFailure: Remove}}},
			},
			other, Keep,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.OnFailureFor(tc.path); got != tc.want {
				t.Errorf("OnFailureFor = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLoadRefusesAnUnknownOnFailure keeps a typo from quietly meaning "keep".
func TestLoadRefusesAnUnknownOnFailure(t *testing.T) {
	for _, contents := range []string{
		"setup_on_failure = \"explode\"\n",
		"[repos.\"~/dev/myrepo\".setup]\non_failure = \"explode\"\n",
	} {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		write(t, dir, contents)

		_, err := Load()
		if err == nil {
			t.Fatalf("expected %q to be refused", contents)
		}
		if !strings.Contains(err.Error(), "explode") {
			t.Errorf("error = %q, want it to quote the bad value", err)
		}
	}
}

func TestSetupIsEmpty(t *testing.T) {
	if !(Setup{}).IsEmpty() {
		t.Error("the zero Setup should be empty")
	}
	if (Setup{Script: "s.sh"}).IsEmpty() {
		t.Error("a Setup with only a script is not empty")
	}
}

// TestLoadReadsAPerRepositorySetup parses the shape from the starter file.
func TestLoadReadsAPerRepositorySetup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	write(t, dir, `worktree_dir = "~/dev/trees"

[repos."~/dev/myrepo".setup]
copy   = [".env", ".env.local"]
link   = ["node_modules"]
run    = ["npm ci"]
script = ".wurk/setup.sh"
`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	setup := cfg.Repos["~/dev/myrepo"].Setup
	switch {
	case len(setup.Copy) != 2:
		t.Errorf("copy = %v", setup.Copy)
	case len(setup.Link) != 1:
		t.Errorf("link = %v", setup.Link)
	case len(setup.Run) != 1:
		t.Errorf("run = %v", setup.Run)
	case setup.Script != ".wurk/setup.sh":
		t.Errorf("script = %q", setup.Script)
	}
}

func TestInitWritesOnceThenLeavesItAlone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, created, err := Init()
	if err != nil || !created {
		t.Fatalf("Init() = %v, %v, %v; want a created file", path, created, err)
	}
	// The starter file must parse, or the first thing a user edits is broken.
	if _, err := Load(); err != nil {
		t.Fatalf("starter file does not parse: %v", err)
	}

	if _, created, err := Init(); err != nil || created {
		t.Fatalf("second Init() = %v, %v; want created=false", created, err)
	}
}

func write(t *testing.T, xdg, contents string) {
	t.Helper()
	path := filepath.Join(xdg, "wurk", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
