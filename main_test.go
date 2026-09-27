package main

import (
	"bytes"
	"strings"
	"testing"
)

// execute runs the root command with args and captures what it writes.
func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestNameIsRequired(t *testing.T) {
	_, _, err := execute(t)
	if err == nil {
		t.Fatal("expected an error when no name is given")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error = %q, want it to mention the name", err)
	}
}

func TestNameGivenTwice(t *testing.T) {
	_, _, err := execute(t, "-w", "one", "two")
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("error = %v, want a complaint about two names", err)
	}
}

func TestShellInit(t *testing.T) {
	for _, tc := range []struct{ shell, want string }{
		{"zsh", "wurk() {"},
		{"bash", "wurk() {"},
		{"fish", "function wurk"},
	} {
		stdout, _, err := execute(t, "shell-init", tc.shell)
		if err != nil {
			t.Fatalf("shell-init %s: %v", tc.shell, err)
		}
		if !strings.Contains(stdout, tc.want) {
			t.Errorf("shell-init %s printed %q, want it to contain %q", tc.shell, stdout, tc.want)
		}
		if !strings.Contains(stdout, "command wurk") {
			t.Errorf("shell-init %s does not call the binary", tc.shell)
		}
	}
}

// TestDonePrintsAPath is what keeps done out of the passthrough list: its
// stdout is for cd, so the wrapper has to capture it.
func TestDoneIsMarkedAsPrintingAPath(t *testing.T) {
	for _, sub := range newRootCmd().Commands() {
		if sub.Name() == "done" {
			if sub.Annotations[annotationPrintsPath] != "true" {
				t.Error("done must be marked as printing a path, or the shell will not follow it")
			}
			return
		}
	}
	t.Fatal("there is no done command")
}

// TestShellInitCoversEveryCommand guards the wrapper's invariant: a command
// missing from the passthrough list would have its output swallowed and fed to
// cd, so the list is generated from the command tree and checked here.
func TestShellInitCoversEveryCommand(t *testing.T) {
	for _, shell := range []string{"zsh", "fish"} {
		stdout, _, err := execute(t, "shell-init", shell)
		if err != nil {
			t.Fatalf("shell-init %s: %v", shell, err)
		}
		for _, sub := range newRootCmd().Commands() {
			printsPath := sub.Annotations[annotationPrintsPath] == "true"
			for _, name := range append([]string{sub.Name()}, sub.Aliases...) {
				switch {
				case printsPath && strings.Contains(stdout, name):
					// Passing it through would hand its path to the terminal
					// instead of to cd.
					t.Errorf("the %s function passes %q through, but it prints a path to follow", shell, name)
				case !printsPath && !strings.Contains(stdout, name):
					t.Errorf("the %s function does not pass %q straight through", shell, name)
				}
			}
		}
	}
}

func TestSubcommandArguments(t *testing.T) {
	if _, _, err := execute(t, "list", "extra"); err == nil {
		t.Error("expected list to reject arguments")
	}
	if _, _, err := execute(t, "config", "extra"); err == nil {
		t.Error("expected config to reject arguments")
	}
	if _, _, err := execute(t, "setup", "one", "two"); err == nil {
		t.Error("expected setup to take at most one name")
	}
	if _, _, err := execute(t, "done", "one", "two"); err == nil {
		t.Error("expected done to take at most one name")
	}
}

func TestShellInitUnknownShell(t *testing.T) {
	_, _, err := execute(t, "shell-init", "csh")
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("error = %v, want an unsupported shell error", err)
	}
}
