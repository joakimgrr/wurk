package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/ui"
)

func newListCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the worktrees and how they stand",
		Long: `List the worktrees and how they stand.

The columns say what is uncommitted, how far the branch has drifted from the
base branch, and whether its work has landed there yet, which is what "wurk rm"
checks before it deletes anything. A ● marks the worktree you are in.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := g.open()
			if err != nil {
				return err
			}
			entries, err := m.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				ui.Info(out, "no worktrees yet")
				return nil
			}
			root, _ := m.Root()
			fmt.Fprintln(out, ui.Table(entries, m.Base(), root))
			return nil
		},
	}
}
