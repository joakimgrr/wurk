package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/setup"
	"github.com/joakimgrr/wurk/internal/ui"
)

func newSetupCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "setup [name]",
		Short: "Run this repository's setup on an existing worktree",
		Long: `Run this repository's setup on an existing worktree.

The same steps that run when a worktree is created, over again: useful while
working out what they should be, or after one of them failed. With no name it
runs on the worktree you are in.

The steps live in wurk's configuration under the repository's own path. See
"wurk config" for where that file is, and what is configured for this one.`,
		Example: `  wurk setup
  wurk setup PROJ-2222-work-on-login-system`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := g.open()
			if err != nil {
				return err
			}
			if m.Setup().IsEmpty() {
				return fmt.Errorf("no setup is configured for %s", ui.Shorten(m.Repo().Root))
			}

			branch, path, err := m.Locate(args)
			if err != nil {
				return err
			}

			msg := cmd.ErrOrStderr()
			ui.Info(msg, "setting up %s at %s", ui.Branch(branch), ui.Path(path))
			if _, err := m.RunSetup(path, branch, msg, func(step setup.Step) {
				ui.SetupStep(msg, step)
			}); err != nil {
				return fmt.Errorf("setup stopped at %w", err)
			}
			ui.Success(msg, "setup finished")
			return nil
		},
	}
}
