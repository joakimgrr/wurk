// Command wurk creates a git worktree plus a branch of the same name and
// prints its path, so a shell function can cd into it.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/config"
	"github.com/joakimgrr/wurk/internal/setup"
	"github.com/joakimgrr/wurk/internal/ui"
	"github.com/joakimgrr/wurk/internal/worktree"
)

const version = "0.2.0"

// globals are the options every command shares.
type globals struct {
	dir string // --dir, overrides where worktrees live
}

// open builds the manager for the repository around the working directory.
func (g *globals) open() (*worktree.Manager, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return worktree.Open(g.dir, cfg)
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "wurk: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	g := &globals{}
	var name, base string
	var noSetup bool

	cmd := &cobra.Command{
		Use:   "wurk <name>",
		Short: "Create a git worktree and a branch of the same name, and land in it",
		Long: `Create a git worktree and a branch of the same name, and land in it.

<name> is both the branch name and the directory name; a name with slashes
still gets one flat directory. The path is printed on stdout and everything
else on stderr, so the shell function from "wurk shell-init" can cd into it.`,
		Example: `  wurk PROJ-2222-work-on-login-system
  wurk -w PROJ-2222-work-on-login-system
  wurk hotfix-login --base v1.4.2
  wurk list
  wurk done`,
		Args:          cobra.MaximumNArgs(1),
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if name != "" {
					return fmt.Errorf("name given twice: -w %s and %s", name, args[0])
				}
				name = args[0]
			}
			if name == "" {
				cmd.SilenceUsage = false
				return errors.New("give the worktree a name")
			}

			m, err := g.open()
			if err != nil {
				return err
			}
			res, err := m.Create(name, base)
			if err != nil {
				return err
			}

			msg := cmd.ErrOrStderr()
			switch {
			case res.Existed:
				ui.Info(msg, "%s is already checked out at %s", ui.Branch(name), ui.Path(res.Path))
			case res.Orphan:
				ui.Success(msg, "created %s at %s, with no history behind it yet", ui.Branch(name), ui.Path(res.Path))
			case res.Base == "":
				ui.Success(msg, "checked out %s at %s", ui.Branch(name), ui.Path(res.Path))
			default:
				ui.Success(msg, "created %s from %s at %s", ui.Branch(name), res.Base, ui.Path(res.Path))
			}
			// When a failed setup keeps its worktree, the path goes out
			// first, so that the shell lands there either way. When it does
			// not, there would be nothing to land in, so the path waits until
			// setup has finished.
			keep := m.OnFailure() == config.Keep
			printPath := func() { fmt.Fprintln(cmd.OutOrStdout(), res.Path) }
			if keep {
				printPath()
			}

			var setupErr error
			if !noSetup && !res.Existed {
				setupErr = runSetup(cmd, m, name, res.Path)
			}
			if setupErr != nil && !keep {
				rollback(cmd, m, res, name)
				return setupErr
			}
			if !keep {
				printPath()
			}
			return setupErr
		},
	}

	cmd.PersistentFlags().StringVar(&g.dir, "dir", "", "directory that holds worktrees (env "+worktree.EnvDir+")")
	cmd.Flags().StringVarP(&name, "worktree", "w", "", "name of the worktree and branch, same as passing it positionally")
	cmd.Flags().StringVar(&base, "base", "", "revision to branch from (default: the repo's default branch)")
	cmd.Flags().BoolVar(&noSetup, "no-setup", false, "skip the setup configured for this repository")

	cmd.AddCommand(newListCmd(g), newDoneCmd(g), newSetupCmd(g), newConfigCmd(g), newShellInitCmd())
	return cmd
}

// rollback takes back a worktree whose setup failed, for a repository
// configured to want nothing left behind.
func rollback(cmd *cobra.Command, m *worktree.Manager, res worktree.Result, branch string) {
	msg := cmd.ErrOrStderr()
	if err := m.Rollback(res, branch); err != nil {
		ui.Warn(msg, "could not remove %s again: %v", ui.Path(res.Path), err)
		return
	}
	what := "worktree"
	if res.Base != "" {
		what = "worktree and branch"
	}
	ui.Info(msg, "removed the %s again (%s = %q)", what, ui.Key("setup_on_failure"), config.Remove)
	ui.Info(msg, "to keep it and look: %s, then %s",
		ui.Dim("wurk "+branch+" --no-setup"), ui.Dim("wurk setup"))
}

// runSetup prepares a worktree and reports on it. A step that fails stops the
// rest but is not fatal to the worktree, which is there and worth standing in
// to fix, so the error says what happened and the caller still has its path.
func runSetup(cmd *cobra.Command, m *worktree.Manager, branch, path string) error {
	if m.Setup().IsEmpty() {
		return nil
	}
	msg := cmd.ErrOrStderr()
	_, err := m.RunSetup(path, branch, msg, func(step setup.Step) {
		ui.SetupStep(msg, step)
	})
	if err != nil {
		return fmt.Errorf("setup stopped at %w", err)
	}
	return nil
}
