package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// posixShellFunction wraps the binary for zsh and bash: stdout is the target
// directory, stderr is left alone so progress messages still reach the user.
// The status of the binary is kept in rc, because zsh reserves $status.
const posixShellFunction = `wurk() {
  local out rc
  out="$(command wurk "$@")"
  rc=$?
  if [ $rc -ne 0 ]; then
    return $rc
  fi
  if [ -d "$out" ]; then
    cd "$out" || return 1
  elif [ -n "$out" ]; then
    printf '%s\n' "$out"
  fi
}
`

const fishShellFunction = `function wurk
    set -l out (command wurk $argv)
    set -l code $status
    if test $code -ne 0
        return $code
    end
    if test -d "$out"
        cd "$out"
    else if test -n "$out"
        printf '%s\n' "$out"
    end
end
`

func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init [zsh|bash|fish]",
		Short: "Print the shell function that cds into new worktrees",
		Long: `Print the shell function that cds into new worktrees.

A process cannot change its parent shell's directory, so wurk prints the path
and this function does the cd. Install it once, defaulting to $SHELL:

    echo 'eval "$(wurk shell-init zsh)"' >> ~/.zshrc`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"zsh", "bash", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := filepath.Base(os.Getenv("SHELL"))
			if len(args) == 1 {
				shell = args[0]
			}
			switch strings.TrimPrefix(shell, "-") {
			case "zsh", "bash", "sh":
				_, err := io.WriteString(cmd.OutOrStdout(), posixShellFunction)
				return err
			case "fish":
				_, err := io.WriteString(cmd.OutOrStdout(), fishShellFunction)
				return err
			default:
				return fmt.Errorf("unsupported shell %q: expected zsh, bash or fish", shell)
			}
		},
	}
}
