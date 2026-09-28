package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/joakimgrr/wurk/internal/ui"
	"github.com/joakimgrr/wurk/internal/worktree"
)

func newTidyCmd(g *globals) *cobra.Command {
	var yes, noFetch bool

	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Remove the worktrees whose work has landed",
		Long: `Remove the worktrees whose work has landed.

For the branches finished with a while ago, whose pull requests merged while
you were somewhere else. Anything merged or squash-merged, with nothing
uncommitted in it, is listed and removed together once you say so; the branch
goes with each one.

It fetches first, because a branch merged on the forge is not merged here
until the remote-tracking branches know about it — which is exactly the branch
worth tidying away. Pass --no-fetch when offline or in a hurry.

Anything it will not touch is listed with the reason, including the worktree
you are standing in: "wurk done" is what takes that one, since it can move you
out first.`,
		Example: `  wurk tidy
  wurk tidy --yes
  wurk tidy --no-fetch`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := g.open()
			if err != nil {
				return err
			}
			// tidy keeps the terminal to itself, so everything goes to stdout.
			out := cmd.OutOrStdout()

			if !noFetch {
				if err := m.Fetch(); err != nil {
					ui.Warn(out, "could not fetch, so this is what was already known: %v", err)
				}
			}
			if err := m.PruneRecords(); err != nil {
				ui.Warn(out, "could not prune stale worktree records: %v", err)
			}

			entries, err := m.List()
			if err != nil {
				return err
			}
			var ready, kept []worktree.Entry
			reasons := map[string]string{}
			for _, e := range entries {
				if reason := m.KeepReason(e); reason != "" {
					if !e.Main {
						kept = append(kept, e)
						reasons[e.Branch] = reason
					}
					continue
				}
				ready = append(ready, e)
			}

			if len(ready) == 0 {
				ui.Info(out, "nothing to tidy")
				reportKept(out, kept, reasons)
				return nil
			}

			root, _ := m.Root()
			fmt.Fprintln(out, ui.Table(ready, m.Base(), root))
			fmt.Fprintln(out)
			reportKept(out, kept, reasons)

			if !yes {
				ahead, err := ui.Confirm(cmd.InOrStdin(), out, fmt.Sprintf("remove %s?", plural(len(ready), "worktree")))
				if err != nil {
					return err
				}
				if !ahead {
					ui.Info(out, "left them alone")
					return nil
				}
			}

			removed := 0
			for _, e := range ready {
				// Not forced: these were judged safe a moment ago, and if that
				// changed since, the refusal is the point.
				if _, err := m.Remove(e.Branch, false); err != nil {
					ui.Warn(out, "kept %s: %v", ui.Branch(e.Branch), err)
					continue
				}
				removed++
			}
			ui.Success(out, "removed %s and %s", plural(removed, "worktree"), plural(removed, "branch"))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before removing")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "judge by what is already known, without fetching first")
	return cmd
}

// reportKept says what survived and why, so that a worktree still standing is
// never a mystery.
func reportKept(w io.Writer, kept []worktree.Entry, reasons map[string]string) {
	for _, e := range kept {
		name := e.Branch
		if name == "" {
			name = ui.Shorten(e.Path)
		}
		ui.Info(w, "keeping %s — %s", ui.Branch(name), reasons[e.Branch])
	}
}
