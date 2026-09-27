// Package setup prepares a freshly created worktree: the files git does not
// carry over, and the commands that make the checkout usable.
package setup

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joakimgrr/wurk/internal/config"
)

// Env is what a worktree's setup is told about itself. Each field is exported
// to the commands as WURK_WORKTREE, WURK_REPO, WURK_BRANCH and WURK_BASE.
type Env struct {
	Worktree string // the new worktree
	Repo     string // the main worktree, which the files are taken from
	Branch   string
	Base     string
}

// Step is one thing setup did, or declined to do.
type Step struct {
	// Kind is copy, link, run or script.
	Kind string
	// What is the path or command the step names.
	What string
	// Skipped says why nothing happened, and is empty when the step ran.
	Skipped string
}

// Steps lists what Run would do, without doing any of it.
func Steps(s config.Setup) []Step {
	var steps []Step
	for _, path := range s.Copy {
		steps = append(steps, Step{Kind: "copy", What: path})
	}
	for _, path := range s.Link {
		steps = append(steps, Step{Kind: "link", What: path})
	}
	for _, command := range s.Run {
		steps = append(steps, Step{Kind: "run", What: command})
	}
	if s.Script != "" {
		steps = append(steps, Step{Kind: "script", What: s.Script})
	}
	return steps
}

// Run works through the setup in order: copy, link, run, script. It stops at
// the first step that fails and returns the steps it got through, so the
// caller can report how far it came.
//
// announce is called as each step is reached — before a command runs, so that
// the output streaming into out has a heading above it.
func Run(s config.Setup, env Env, out io.Writer, announce func(Step)) ([]Step, error) {
	var done []Step
	record := func(step Step) {
		done = append(done, step)
		if announce != nil {
			announce(step)
		}
	}

	for _, path := range s.Copy {
		step, err := copyStep(path, env)
		record(step)
		if err != nil {
			return done, wrap(path, err)
		}
	}
	for _, path := range s.Link {
		step, err := linkStep(path, env)
		record(step)
		if err != nil {
			return done, wrap(path, err)
		}
	}
	for _, command := range s.Run {
		record(Step{Kind: "run", What: command})
		if err := execute(env, out, "sh", "-c", command); err != nil {
			return done, wrap(command, err)
		}
	}
	if s.Script != "" {
		path, missing, err := scriptPath(s.Script, env)
		step := Step{Kind: "script", What: s.Script, Skipped: missing}
		record(step)
		switch {
		case err != nil:
			return done, wrap(s.Script, err)
		case missing != "":
			return done, nil
		}
		if err := runScript(path, env, out); err != nil {
			return done, wrap(s.Script, err)
		}
	}
	return done, nil
}

// copyStep brings one path over from the main worktree. A source that is not
// there is not an error: a file such as .env.local exists on some machines
// and not others.
func copyStep(path string, env Env) (Step, error) {
	step := Step{Kind: "copy", What: path}
	src, dst, err := endpoints(path, env)
	if err != nil {
		return step, err
	}
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		step.Skipped = "not in " + filepath.Base(env.Repo)
		return step, nil
	}
	if err != nil {
		return step, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return step, err
	}
	if info.IsDir() {
		return step, copyTree(src, dst)
	}
	return step, copyFile(src, dst, info.Mode())
}

// linkStep points one path at the main worktree's copy. Something already
// there is left alone: it is more likely to be checked-in content than
// something to replace.
func linkStep(path string, env Env) (Step, error) {
	step := Step{Kind: "link", What: path}
	src, dst, err := endpoints(path, env)
	if err != nil {
		return step, err
	}
	if _, err := os.Lstat(src); os.IsNotExist(err) {
		step.Skipped = "not in " + filepath.Base(env.Repo)
		return step, nil
	}
	if _, err := os.Lstat(dst); err == nil {
		step.Skipped = "already in the worktree"
		return step, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return step, err
	}
	return step, os.Symlink(src, dst)
}

// scriptPath locates the configured script. A relative path is taken from the
// new worktree, so a script kept in the repository runs the branch's own copy.
func scriptPath(script string, env Env) (path, missing string, err error) {
	path = config.ExpandHome(script)
	if !filepath.IsAbs(path) {
		path = filepath.Join(env.Worktree, path)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// The branch may predate the script, which is no reason to refuse it
		// a worktree.
		return path, "no such file", nil
	} else if err != nil {
		return path, "", err
	}
	return path, "", nil
}

// runScript executes the file directly when it is executable, so that its
// shebang decides the interpreter, and through sh when it is not.
func runScript(path string, env Env, out io.Writer) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode()&0o111 != 0 {
		return execute(env, out, path)
	}
	return execute(env, out, "sh", path)
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

// endpoints turns a configured path into where it comes from and where it
// goes, refusing anything that would reach outside the worktree.
func endpoints(path string, env Env) (src, dst string, err error) {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%q must be a path inside the repository", path)
	}
	return filepath.Join(env.Repo, clean), filepath.Join(env.Worktree, clean), nil
}

// execute runs one command in the worktree, with its output going straight to
// out so that a long install is not a silent wait.
func execute(env Env, out io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = env.Worktree
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = append(os.Environ(),
		"WURK_WORKTREE="+env.Worktree,
		"WURK_REPO="+env.Repo,
		"WURK_BRANCH="+env.Branch,
		"WURK_BASE="+env.Base,
	)
	return cmd.Run()
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyTree copies a directory, keeping symlinks as symlinks so that a linked
// cache is not duplicated by accident.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.Remove(target)
			return os.Symlink(link, target)
		default:
			return copyFile(path, target, info.Mode())
		}
	})
}
