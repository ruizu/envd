package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// newRootCmd builds the root envd command with all subcommands registered.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "envd",
		Short: "Load secret and parameter values from a backend into env vars, then exec a command",
		Long: "envd resolves one or more --env VAR=secret_id mappings against a backend\n" +
			"(currently AWS Secrets Manager and Systems Manager Parameter Store),\n" +
			"injects them as environment variables, and then executes the provided\n" +
			"command with those variables set.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

// Execute builds and runs the root command. It returns the exit code that the
// program should terminate with. SIGINT and SIGTERM are forwarded to the child
// process via context cancellation so it can shut down gracefully.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := newRootCmd().ExecuteContext(ctx)
	return exitCode(err, ctx.Err())
}

// exitCode maps a command error (and the root context's error) to a process
// exit code. It also prints a diagnostic to stderr for unexpected errors.
//
//   - nil error                    -> 0
//   - *exec.ExitError              -> the child's own exit code
//   - context cancelled (signal)   -> 130 (128 + SIGINT), quietly
//   - any other error              -> 1, with a message on stderr
func exitCode(err, ctxErr error) int {
	if err == nil {
		return 0
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}

	// A cancelled context means envd received SIGINT/SIGTERM and terminated
	// the child. Exit quietly with the conventional 128+signal code rather
	// than printing a "context canceled" error.
	if errors.Is(ctxErr, context.Canceled) {
		return 130 // 128 + SIGINT(2); conventional shell code for Ctrl-C
	}

	fmt.Fprintln(os.Stderr, "envd:", err)
	return 1
}
