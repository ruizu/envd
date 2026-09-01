package cli

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestExitCodeNil(t *testing.T) {
	if got := exitCode(nil, nil); got != 0 {
		t.Fatalf("nil error -> got %d, want 0", got)
	}
}

func TestExitCodeExitError(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	// Produce a real *exec.ExitError with a known code.
	runErr := exec.Command("sh", "-c", "exit 5").Run()
	var ee *exec.ExitError
	if !errors.As(runErr, &ee) {
		t.Fatalf("setup: expected ExitError, got %v", runErr)
	}
	if got := exitCode(runErr, nil); got != 5 {
		t.Fatalf("ExitError -> got %d, want 5", got)
	}
}

func TestExitCodeCancelled(t *testing.T) {
	// A non-ExitError together with a cancelled context maps to 130.
	got := exitCode(errors.New("context canceled"), context.Canceled)
	if got != 130 {
		t.Fatalf("cancelled -> got %d, want 130", got)
	}
}

func TestExitCodeGenericError(t *testing.T) {
	got := exitCode(errors.New("something failed"), nil)
	if got != 1 {
		t.Fatalf("generic error -> got %d, want 1", got)
	}
}

func TestExitCodeExitErrorTakesPrecedenceOverCancel(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	// If the error is an ExitError, its code wins even if the context was also
	// cancelled (the child's own exit status is the more specific signal).
	runErr := exec.Command("sh", "-c", "exit 4").Run()
	if got := exitCode(runErr, context.Canceled); got != 4 {
		t.Fatalf("got %d, want 4", got)
	}
}
