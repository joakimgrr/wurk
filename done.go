package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/git"
	"github.com/joakimgrr/wurk/internal/ui"
	"github.com/joakimgrr/wurk/internal/worktree"
)

func newDoneCmd(g *globals) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:     "done [name]",
		Aliases: []string{"rm", "remove", "delete"},
		Short:   "Finish with a worktree: remove it, delete its branch, and go back",
		Long: `Finish with a worktree: remove it, delete its branch, and go back.

With no name it finishes the worktree you are in, and moves you back to the
repository afterwards, since where you were standing is about to be gone.
Naming another worktree removes that one instead and leaves you where you are.

A branch that has landed in the base, by merge or by squash, goes without
comment. Anything else — work that has not landed, changes never committed —
is put to you as a question first, unless --yes answers it in advance.

The repository is left on whatever branch it already had checked out.`,
		Example: `  wurk done
  wurk done PROJ-2222-work-on-login-system
  wurk done --yes`,
		Args:          cobra.MaximumNArgs(1),
		Annotations:   map[string]string{annotationPrintsPath: "true"},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := g.open()
			if err != nil {
				return err
			}
			branch, path, err := m.Locate(args)
			if err != nil {
				return err
			}
			// Whether the shell has to be moved afterwards depends on whether
			// the ground it is standing on is what goes.
			leaving := m.IsCurrent(path)

			// Whether this may be removed at all is settled before the
			// person is asked anything, so that the question is never about a
			// worktree the answer could not apply to.
			if err := m.EnsureRemovable(branch); err != nil {
				return err
			}

			msg := cmd.ErrOrStderr()
			if concerns := m.Inspect(branch, path); concerns.Any() && !yes {
				question := fmt.Sprintf("%s %s. Remove it anyway?", ui.Branch(branch), concerns.Describe())
				ahead, err := ui.Confirm(cmd.InOrStdin(), msg, question)
				if err != nil {
					return err
				}
				if !ahead {
					ui.Info(msg, "left %s alone", ui.Branch(branch))
					return nil
				}
			}

			res, err := m.Remove(branch, true)
			if err != nil {
				return err
			}
			reportRemoval(msg, res)

			if !leaving {
				return nil
			}
			// Back to the repository. Whatever branch it has checked out is
			// its own business, so it is reported rather than changed.
			root := m.Repo().Root
			if on := m.Repo().BranchAt(root); on != "" {
				ui.Success(msg, "back in %s on %s", ui.Path(root), ui.Branch(on))
			} else {
				ui.Success(msg, "back in %s", ui.Path(root))
			}
			fmt.Fprintln(cmd.OutOrStdout(), root)
			return nil
		},
	}
	// --force is the same answer under the name the old rm command used.
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask, whatever would be lost")
	cmd.Flags().BoolVarP(&yes, "force", "f", false, "same as --yes")
	return cmd
}

// reportRemoval says what a removal took away.
func reportRemoval(w io.Writer, res worktree.RemoveResult) {
	if res.Path != "" {
		ui.Success(w, "removed worktree %s", ui.Path(res.Path))
	}
	switch {
	case !res.BranchDeleted:
		ui.Info(w, "there was no branch %s to delete", ui.Branch(res.Branch))
	case res.State == git.NotMerged:
		ui.Warn(w, "deleted %s, which was not merged into %s", ui.Branch(res.Branch), res.Base)
	case res.State == git.Unstarted:
		ui.Success(w, "deleted %s, which had no commits of its own", ui.Branch(res.Branch))
	default:
		ui.Success(w, "deleted %s (%s into %s)", ui.Branch(res.Branch), res.State, res.Base)
	}
}
