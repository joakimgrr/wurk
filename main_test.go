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

func TestShellInitUnknownShell(t *testing.T) {
	_, _, err := execute(t, "shell-init", "csh")
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("error = %v, want an unsupported shell error", err)
	}
}
