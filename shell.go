package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// The wrapper captures stdout only for the invocation that creates a worktree,
// because that one prints a path to cd into. Everything else is handed the
// terminal directly, so its output stays styled and unbuffered.
//
// A printed directory is followed whatever the exit code: a worktree whose
// setup failed still exists and is the place to fix it from. The code is
// passed on afterwards, so a caller can still tell that something went wrong.
// @@CASES@@ is filled in from the command tree.
const posixShellFunction = `wurk() {
  case "$1" in
    @@CASES@@)
      command wurk "$@"
      return $?
      ;;
  esac
  local out rc
  out="$(command wurk "$@")"
  rc=$?
  if [ -d "$out" ]; then
    cd "$out" || return 1
  elif [ -n "$out" ]; then
    printf '%s\n' "$out"
  fi
  return $rc
}
`

const fishShellFunction = `function wurk
    set -l sub ""
    if test (count $argv) -gt 0
        set sub $argv[1]
    end
    switch "$sub"
        case @@CASES@@
            command wurk $argv
            return $status
    end
    set -l out (command wurk $argv)
    set -l code $status
    if test -d "$out"
        cd "$out"
    else if test -n "$out"
        printf '%s\n' "$out"
    end
    return $code
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
		Args:          cobra.MaximumNArgs(1),
		ValidArgs:     []string{"zsh", "bash", "fish"},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := filepath.Base(os.Getenv("SHELL"))
			if len(args) == 1 {
				shell = args[0]
			}
			names := passthrough(cmd.Root())
			out := cmd.OutOrStdout()
			switch strings.TrimPrefix(shell, "-") {
			case "zsh", "bash", "sh":
				_, err := io.WriteString(out, strings.Replace(posixShellFunction, "@@CASES@@", strings.Join(quoteAll(names, `"`), "|"), 1))
				return err
			case "fish":
				_, err := io.WriteString(out, strings.Replace(fishShellFunction, "@@CASES@@", strings.Join(quoteAll(names, "'"), " "), 1))
				return err
			default:
				return fmt.Errorf("unsupported shell %q: expected zsh, bash or fish", shell)
			}
		},
	}
}

// annotationPrintsPath marks a command whose stdout is a path for the shell
// function to cd into. Those are the ones the wrapper has to capture; every
// other command wants the terminal to itself.
const annotationPrintsPath = "wurk.prints_path"

// passthrough lists the first arguments the wrapper must hand straight to the
// binary: every subcommand and alias except the ones that print a path, the
// flags that print something, and the empty argument, which is wurk called
// with nothing at all.
func passthrough(root *cobra.Command) []string {
	names := []string{"", "-h", "--help", "--version"}
	for _, sub := range root.Commands() {
		if sub.Annotations[annotationPrintsPath] == "true" {
			continue
		}
		names = append(names, sub.Name())
		names = append(names, sub.Aliases...)
	}
	return names
}

// quoteAll wraps each name so that an empty string and a leading dash survive
// the shell's own parsing.
func quoteAll(names []string, quote string) []string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quote + name + quote
	}
	return quoted
}
