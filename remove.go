package main

import (
	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/git"
	"github.com/joakimgrr/wurk/internal/ui"
)

func newRemoveCmd(g *globals) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a worktree and delete its branch",
		Long: `Remove a worktree and delete its branch.

By default wurk refuses a branch whose work has not landed in the base branch,
and git refuses a worktree with uncommitted changes, so nothing is lost by
accident. A branch that was squash-merged counts as merged. --force skips both
checks and throws the work away.`,
		Example: `  wurk rm PROJ-2222-work-on-login-system
  wurk rm spike-that-went-nowhere --force`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := g.open()
			if err != nil {
				return err
			}
			res, err := m.Remove(args[0], force)
			if err != nil {
				return err
			}

			msg := cmd.ErrOrStderr()
			if res.Path != "" {
				ui.Success(msg, "removed worktree %s", ui.Path(res.Path))
			}
			switch {
			case !res.BranchDeleted:
				ui.Info(msg, "there was no branch %s to delete", ui.Branch(res.Branch))
			case res.State == git.NotMerged:
				ui.Warn(msg, "deleted %s, which was not merged into %s", ui.Branch(res.Branch), res.Base)
			default:
				ui.Success(msg, "deleted %s (%s into %s)", ui.Branch(res.Branch), res.State, res.Base)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "delete even when the work has not landed, discarding uncommitted changes")
	return cmd
}
