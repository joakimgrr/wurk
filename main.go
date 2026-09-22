// Command wurk creates a git worktree plus a branch of the same name and
// prints its path, so a shell function can cd into it.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"wurk/internal/worktree"
)

const version = "0.1.0"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "wurk: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var name, base, dir string

	cmd := &cobra.Command{
		Use:   "wurk <name>",
		Short: "Create a git worktree and a branch of the same name, and land in it",
		Long: `Create a git worktree and a branch of the same name, and land in it.

<name> is both the branch name and the directory name; a name with slashes
still gets one flat directory. The path is printed on stdout and everything
else on stderr, so the shell function from "wurk shell-init" can cd into it.`,
		Example: `  wurk PROJ-2222-work-on-login-system
  wurk -w PROJ-2222-work-on-login-system
  wurk hotfix-login --base v1.4.2`,
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

			res, err := worktree.Create(name, base, dir)
			if err != nil {
				return err
			}
			switch {
			case res.Existed:
				fmt.Fprintf(cmd.ErrOrStderr(), "wurk: %s is already checked out at %s\n", name, res.Path)
			case res.Base == "":
				fmt.Fprintf(cmd.ErrOrStderr(), "wurk: checked out %s at %s\n", name, res.Path)
			default:
				fmt.Fprintf(cmd.ErrOrStderr(), "wurk: created %s from %s at %s\n", name, res.Base, res.Path)
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Path)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&name, "worktree", "w", "", "name of the worktree and branch, same as passing it positionally")
	f.StringVar(&base, "base", "", "revision to branch from (default: the repo's default branch)")
	f.StringVar(&dir, "dir", "", "directory that holds worktrees (env "+worktree.EnvDir+")")

	cmd.AddCommand(newShellInitCmd())
	return cmd
}
