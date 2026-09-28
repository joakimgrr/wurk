package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/config"
	"github.com/joakimgrr/wurk/internal/setup"
	"github.com/joakimgrr/wurk/internal/ui"
	"github.com/joakimgrr/wurk/internal/worktree"
)

func newConfigCmd(g *globals) *cobra.Command {
	var write bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the configuration, or write a starter file",
		Long: `Show the configuration, or write a starter file.

The file is optional and lives at $XDG_CONFIG_HOME/wurk/config.toml, or
~/.config/wurk/config.toml. It currently holds one setting:

    worktree_dir = "~/dev/worktrees"

The --dir flag beats the ` + worktree.EnvDir + ` environment variable, which beats
the file, which beats the built-in default.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if write {
				path, created, err := config.Init()
				if err != nil {
					return err
				}
				if created {
					ui.Success(out, "wrote %s", ui.Path(path))
				} else {
					ui.Info(out, "%s already exists, leaving it alone", ui.Path(path))
				}
				return nil
			}

			path, err := config.Path()
			if err != nil {
				return err
			}
			note := ""
			if _, err := os.Stat(path); err != nil {
				note = ui.Dim(" (none yet, write one with --init)")
			}
			fmt.Fprintf(out, "%s  %s%s\n", ui.Key("config file "), ui.Shorten(path), note)

			// Where worktrees go depends on the repository, so only report it
			// when wurk was run inside one.
			m, err := g.open()
			if err != nil {
				fmt.Fprintf(out, "%s  %s\n", ui.Key("worktree dir"), ui.Dim("("+err.Error()+")"))
				return nil
			}
			root, source := m.Root()
			fmt.Fprintf(out, "%s  %s %s\n", ui.Key("worktree dir"), ui.Shorten(root), ui.Dim("("+string(source)+")"))
			fmt.Fprintf(out, "%s  %s\n", ui.Key("base branch "), m.Base())

			steps := setup.Steps(m.Setup())
			if len(steps) == 0 {
				fmt.Fprintf(out, "%s  %s\n", ui.Key("setup       "), ui.Dim("nothing configured for this repository"))
				return nil
			}
			fmt.Fprintf(out, "%s  %s\n", ui.Key("setup       "), ui.Dim(plural(len(steps), "step")))
			for _, step := range steps {
				ui.SetupStep(out, step)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&write, "init", false, "write a commented starter file if there is none")
	return cmd
}

// plural renders a count with its noun, so that one step is not "1 steps".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	if strings.HasSuffix(noun, "ch") || strings.HasSuffix(noun, "s") {
		return fmt.Sprintf("%d %ses", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
