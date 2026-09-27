// Package config reads wurk's optional configuration file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// OnFailure says what to become of a worktree whose setup did not finish.
type OnFailure string

const (
	// Keep leaves the worktree where it is, half prepared but standable in,
	// which is where fixing it from is easiest.
	Keep OnFailure = "keep"
	// Remove takes the worktree away again, and the branch with it when wurk
	// was what created it, leaving nothing behind to clean up by hand.
	Remove OnFailure = "remove"
)

// Config holds the settings that can be kept on disk. Every field is
// optional; a missing file is the same as an empty one.
type Config struct {
	// WorktreeDir is the directory new worktrees are created in. A relative
	// path is taken from the repository root, and a leading ~ is expanded.
	WorktreeDir string `toml:"worktree_dir"`
	// SetupOnFailure is what to do when a setup step fails, for repositories
	// that do not say for themselves. The default is Remove.
	SetupOnFailure OnFailure `toml:"setup_on_failure"`
	// Repos holds per-repository settings, keyed by the path of the
	// repository's main worktree. A leading ~ is expanded.
	Repos map[string]Repo `toml:"repos"`
}

// Repo is what is configured for one repository.
type Repo struct {
	Setup Setup `toml:"setup"`
}

// Setup is what to do with a worktree once git has created it. The phases run
// in the order they are declared here: the files a command might read are in
// place before any command runs.
type Setup struct {
	// Copy names files and directories to bring over from the main worktree,
	// relative to the repository root. A source that is not there is skipped.
	Copy []string `toml:"copy"`
	// Link names paths to symlink to the main worktree's copy instead of
	// duplicating, for directories too big to be worth copying.
	Link []string `toml:"link"`
	// Run holds shell commands, executed in the new worktree in order.
	Run []string `toml:"run"`
	// Script names a file to execute last. A relative path is taken from the
	// new worktree, so a script kept in the repository runs the branch's own
	// copy of itself.
	Script string `toml:"script"`
	// OnFailure overrides the file's setup_on_failure for this repository.
	OnFailure OnFailure `toml:"on_failure"`
}

// IsEmpty reports whether there is anything to do.
func (s Setup) IsEmpty() bool {
	return len(s.Copy) == 0 && len(s.Link) == 0 && len(s.Run) == 0 && s.Script == ""
}

// SetupFor returns the steps configured for the repository whose main worktree
// is at root, matching through ~ and symlinks so that the key can be written
// the way a person would type it.
func (c Config) SetupFor(root string) Setup {
	want := resolve(root)
	for key, repo := range c.Repos {
		if resolve(ExpandHome(key)) == want {
			return repo.Setup
		}
	}
	return Setup{}
}

// OnFailureFor says what to do with a worktree of this repository whose setup
// failed: what the repository asks for, else what the file asks for, else
// removing it, so that a worktree that exists is one that is ready to work in.
func (c Config) OnFailureFor(root string) OnFailure {
	if own := c.SetupFor(root).OnFailure; own != "" {
		return own
	}
	if c.SetupOnFailure != "" {
		return c.SetupOnFailure
	}
	return Remove
}

// valid reports whether the value is one wurk knows, an empty one meaning
// "not set here".
func (f OnFailure) valid() bool {
	return f == "" || f == Keep || f == Remove
}

// check refuses a setting that would otherwise be silently ignored.
func (c Config) check() error {
	if !c.SetupOnFailure.valid() {
		return fmt.Errorf("setup_on_failure is %q, want %q or %q", c.SetupOnFailure, Keep, Remove)
	}
	for key, repo := range c.Repos {
		if !repo.Setup.OnFailure.valid() {
			return fmt.Errorf("on_failure for %s is %q, want %q or %q", key, repo.Setup.OnFailure, Keep, Remove)
		}
	}
	return nil
}

// ExpandHome replaces a leading ~ with the home directory.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// resolve makes two paths comparable, following the symlinks that sit in front
// of directories such as /tmp on macOS.
func resolve(path string) string {
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// starter is written by Init, so the file explains itself.
const starter = `# wurk configuration.

# Where new worktrees are created. Absolute, ~-relative, or relative to the
# repository root. The default is a sibling of the repository named
# <repo>-worktrees, so ~/dev/myrepo gets ~/dev/myrepo-worktrees.
#
# worktree_dir = "~/dev/worktrees"

# What to do with a worktree when one of its setup steps fails.
#
#   remove  take it away again, and the branch with it, leaving nothing
#           behind and your shell where it was (the default)
#   keep    leave it, half prepared, and move into it to fix it
#
# setup_on_failure = "remove"

# What to do with a new worktree once git has created it, per repository. The
# key is the path of the repository itself, not of its worktrees. The phases
# run in the order below.
#
# [repos."~/dev/myrepo".setup]
# # Brought over from the main worktree; a file that is not there is skipped.
# copy   = [".env", ".env.local"]
# # Symlinked to the main worktree's copy instead of duplicated.
# link   = ["node_modules"]
# # Shell commands, run in the new worktree, in order.
# run    = ["npm ci"]
# # A file to execute last, relative to the new worktree.
# script = ".wurk/setup.sh"
# # Overrides setup_on_failure above, for this repository only.
# on_failure = "keep"
`

// Path is the configuration file's location, honouring XDG_CONFIG_HOME.
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "wurk", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "wurk", "config.toml"), nil
}

// Load reads the configuration file. A missing file is not an error.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.check(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Init writes a commented starter file and reports where it went. An existing
// file is left alone, and created is false.
func Init() (path string, created bool, err error) {
	path, err = Path()
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(starter), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}
