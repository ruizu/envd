package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// stubBackend returns canned secret values for testing and can simulate errors.
type stubBackend struct {
	values map[string]string
	err    error
}

func (s *stubBackend) Resolve(_ context.Context, id string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.values[id], nil
}

func requireSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available on this platform")
	}
}

// captureEnvValue runs the target command via runExec so that it writes the
// value of envVar to a temp file, then returns the file's contents. This
// exercises the real runExec injection + exec path.
func captureEnvValue(t *testing.T, b *stubBackend, mappings []SecretMapping, envVar string) string {
	t.Helper()
	requireSh(t)
	outFile := filepath.Join(t.TempDir(), "out")
	// printf %s writes the raw value with no trailing newline or interpretation.
	script := `printf '%s' "$` + envVar + `" > "` + outFile + `"`
	if err := runExec(context.Background(), b, mappings, "sh", []string{"-c", script}); err != nil {
		t.Fatalf("runExec failed: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return string(data)
}

func TestRunExecInjectsSingleSecret(t *testing.T) {
	b := &stubBackend{values: map[string]string{"secret-id": "resolved-value"}}
	got := captureEnvValue(t, b, []SecretMapping{{EnvVar: "MY_SECRET", SecretID: "secret-id"}}, "MY_SECRET")
	if got != "resolved-value" {
		t.Fatalf("got %q, want %q", got, "resolved-value")
	}
}

func TestRunExecMultipleSecrets(t *testing.T) {
	b := &stubBackend{values: map[string]string{"id1": "v1", "id2": "v2"}}
	mappings := []SecretMapping{
		{EnvVar: "A", SecretID: "id1"},
		{EnvVar: "B", SecretID: "id2"},
	}
	if got := captureEnvValue(t, b, mappings, "A"); got != "v1" {
		t.Errorf("A: got %q, want %q", got, "v1")
	}
	if got := captureEnvValue(t, b, mappings, "B"); got != "v2" {
		t.Errorf("B: got %q, want %q", got, "v2")
	}
}

func TestRunExecNoSecretsStillRuns(t *testing.T) {
	requireSh(t)
	// With no mappings, the backend is never consulted and the command runs.
	b := &stubBackend{err: errors.New("backend should not be called")}
	if err := runExec(context.Background(), b, nil, "sh", []string{"-c", "exit 0"}); err != nil {
		t.Fatalf("expected success with no secrets, got: %v", err)
	}
}

func TestRunExecValueWithSpacesAndSpecialChars(t *testing.T) {
	// Verifies fmt.Sprintf("%s=%s") + exec preserves arbitrary bytes: spaces,
	// tabs, quotes, shell metacharacters, $-expansions, and unicode.
	value := "a b\tc \"q\" 'x' $HOME `id` ; rm -rf / | cat && echo=z é 日本"
	b := &stubBackend{values: map[string]string{"id": value}}
	got := captureEnvValue(t, b, []SecretMapping{{EnvVar: "WEIRD", SecretID: "id"}}, "WEIRD")
	if got != value {
		t.Fatalf("special-char value not preserved:\n got:  %q\n want: %q", got, value)
	}
}

func TestRunExecSecretOverridesExistingEnv(t *testing.T) {
	// A resolved secret must override a pre-existing environment variable of
	// the same name (later env entries win).
	t.Setenv("OVERRIDE_ME", "original")
	b := &stubBackend{values: map[string]string{"id": "from-secret"}}
	got := captureEnvValue(t, b, []SecretMapping{{EnvVar: "OVERRIDE_ME", SecretID: "id"}}, "OVERRIDE_ME")
	if got != "from-secret" {
		t.Fatalf("expected secret to override existing env; got %q", got)
	}
}

func TestRunExecInheritsParentEnv(t *testing.T) {
	// Variables not injected should still be visible to the child (inherited).
	t.Setenv("INHERITED_VAR", "inherited-value")
	b := &stubBackend{values: map[string]string{}}
	got := captureEnvValue(t, b, nil, "INHERITED_VAR")
	if got != "inherited-value" {
		t.Fatalf("expected inherited env var to be visible; got %q", got)
	}
}

func TestRunExecBackendErrorPropagates(t *testing.T) {
	requireSh(t)
	sentinel := errors.New("boom")
	b := &stubBackend{err: sentinel}
	err := runExec(context.Background(), b, []SecretMapping{{EnvVar: "X", SecretID: "id"}}, "sh", []string{"-c", "exit 0"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected backend error to propagate, got: %v", err)
	}
}

func TestRunExecUnknownCommand(t *testing.T) {
	b := &stubBackend{values: map[string]string{}}
	err := runExec(context.Background(), b, nil, "this-command-does-not-exist-xyz", nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestRunExecPropagatesExitCode(t *testing.T) {
	requireSh(t)
	b := &stubBackend{values: map[string]string{}}
	err := runExec(context.Background(), b, nil, "sh", []string{"-c", "exit 3"})
	if err == nil {
		t.Fatal("expected non-nil error for non-zero exit")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("expected exit code 3, got: %v", err)
	}
}

func TestRunExecContextCancellationTerminatesChild(t *testing.T) {
	requireSh(t)
	b := &stubBackend{values: map[string]string{}}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the child starts; this simulates envd receiving
	// SIGINT/SIGTERM (which cancels the signal-derived context).
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		// A child that would otherwise sleep for a long time.
		done <- runExec(ctx, b, nil, "sh", []string{"-c", "sleep 30"})
	}()

	select {
	case err := <-done:
		// The child was terminated via the cancellation path, so Run returns
		// a non-nil error rather than a clean exit.
		if err == nil {
			t.Fatal("expected non-nil error after context cancellation")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runExec did not return after context cancellation (child not terminated)")
	}
}

func TestRunExecGracefulSIGTERMAllowsChildCleanup(t *testing.T) {
	requireSh(t)
	b := &stubBackend{values: map[string]string{}}
	outFile := filepath.Join(t.TempDir(), "cleanup")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	// The child traps SIGTERM, writes a marker file, and exits cleanly. This
	// proves envd sends SIGTERM (graceful) rather than SIGKILL, giving the
	// child a chance to run cleanup handlers.
	script := `trap 'echo cleaned > "` + outFile + `"; exit 0' TERM; sleep 30 & wait`
	_ = runExec(ctx, b, nil, "sh", []string{"-c", script})

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected child cleanup to run on SIGTERM, but marker file missing: %v", err)
	}
	if string(data) != "cleaned\n" {
		t.Fatalf("unexpected cleanup marker contents: %q", string(data))
	}
}
