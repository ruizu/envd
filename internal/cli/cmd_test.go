package cli

import (
	"bytes"
	"strings"
	"testing"
)

// execRoot runs the root command with the given args, capturing output and
// returning the resulting error. It exercises the real cobra wiring.
func execRoot(args ...string) (string, error) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestCmdRunRequiresCommand(t *testing.T) {
	// `run` with no positional command must fail MinimumNArgs before any
	// backend interaction.
	_, err := execRoot("run", "--env", "A=id")
	if err == nil {
		t.Fatal("expected error when no command is given to run")
	}
	if !strings.Contains(err.Error(), "at least 1 arg") {
		t.Fatalf("expected MinimumNArgs error, got: %v", err)
	}
}

func TestCmdUnknownSubcommand(t *testing.T) {
	_, err := execRoot("not-a-subcommand")
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected 'unknown command' error, got: %v", err)
	}
}

func TestCmdRunInvalidEnvValue(t *testing.T) {
	// Invalid --env value must be rejected by ParseEnvs before the backend is
	// contacted.
	_, err := execRoot("run", "--env", "bad-name=id", "some-cmd")
	if err == nil {
		t.Fatal("expected error for invalid --env value")
	}
	if !strings.Contains(err.Error(), "invalid --env") {
		t.Fatalf("expected 'invalid --env' error, got: %v", err)
	}
}

func TestCmdRunUnknownBackend(t *testing.T) {
	// A bogus backend name reaches backend.New and errors there, which also
	// proves that flags and the command name parsed correctly.
	_, err := execRoot("run", "--env", "A=id", "--backend", "bogus", "some-cmd", "--child-flag", "v")
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("expected 'unknown backend' error (implies args parsed), got: %v", err)
	}
}

func TestCmdVersion(t *testing.T) {
	out, err := execRoot("version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "envd ") || !strings.Contains(out, "go:") {
		t.Fatalf("unexpected version output: %q", out)
	}
}

func TestCmdVersionRejectsArgs(t *testing.T) {
	_, err := execRoot("version", "extra")
	if err == nil {
		t.Fatal("expected error when version is given extra args")
	}
}
