// Package config reads wurk's optional configuration file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds the settings that can be kept on disk. Every field is
// optional; a missing file is the same as an empty one.
type Config struct {
	// WorktreeDir is the directory new worktrees are created in. A relative
	// path is taken from the repository root, and a leading ~ is expanded.
	WorktreeDir string `toml:"worktree_dir"`
}

// starter is written by Init, so the file explains itself.
const starter = `# wurk configuration.

# Where new worktrees are created. Absolute, ~-relative, or relative to the
# repository root. The default is a sibling of the repository named
# <repo>-worktrees, so ~/dev/myrepo gets ~/dev/myrepo-worktrees.
#
# worktree_dir = "~/dev/worktrees"
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
