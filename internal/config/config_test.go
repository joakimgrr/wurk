package config

import (
	"os"
	"path/filepath"
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
