package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ruizu/envd/internal/backend"
	"github.com/spf13/cobra"
)

// newRunCmd builds the `run` subcommand, which resolves secrets and executes
// the target command with them injected as environment variables.
func newRunCmd() *cobra.Command {
	var (
		envs        []string
		backendName string
		profile     string
		region      string
	)

	runCmd := &cobra.Command{
		Use:   "run [command] [args...]",
		Short: "Resolve secrets and execute the given command",
		Example: `  envd run --env SECRET_VAR1=aws_secret_id1 cli-to-run
  envd run --env SECRET_VAR1=id1 --env SECRET_VAR2=id2 cli-to-run param1 param2
  envd run --backend=aws --profile=test_profile --env SECRET_VAR1=id1 cli-to-run`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mappings, err := ParseEnvs(envs)
			if err != nil {
				return err
			}

			return run(cmd.Context(), backendName, backend.Options{
				Profile: profile,
				Region:  region,
			}, mappings, args[0], args[1:])
		},
	}

	runCmd.Flags().StringArrayVarP(&envs, "env", "e", nil,
		"secret mapping VAR=secret_id (repeatable)")
	runCmd.Flags().StringVar(&backendName, "backend", "aws", "secret backend to use")
	runCmd.Flags().StringVar(&profile, "profile", "", "backend credentials/config profile (optional)")
	runCmd.Flags().StringVar(&region, "region", "", "backend region (optional)")

	// Stop parsing flags at the first positional argument (the target command)
	// so the child command's own flags pass through untouched. envd's flags must
	// appear after `run` and before the target command.
	runCmd.Flags().SetInterspersed(false)

	return runCmd
}

// run constructs the backend and executes the target command with the resolved
// secrets injected. It is the wiring layer; runExec holds the testable logic.
func run(ctx context.Context, backendName string, opts backend.Options, mappings []SecretMapping, command string, cmdArgs []string) error {
	b, err := backend.New(ctx, backendName, opts)
	if err != nil {
		return err
	}
	return runExec(ctx, b, mappings, command, cmdArgs)
}

// runExec resolves each mapping against the given backend, layers the results
// on top of the current environment, and executes the target command.
func runExec(ctx context.Context, b backend.Backend, mappings []SecretMapping, command string, cmdArgs []string) error {
	// Start from the current environment and layer resolved secrets on top.
	// Later entries take precedence, so injected secrets override any existing
	// variable of the same name.
	env := os.Environ()
	for _, m := range mappings {
		value, err := b.GetSecret(ctx, m.SecretID)
		if err != nil {
			return err
		}
		env = append(env, fmt.Sprintf("%s=%s", m.EnvVar, value))
	}

	path, err := exec.LookPath(command)
	if err != nil {
		return fmt.Errorf("locating command %q: %w", command, err)
	}

	c := exec.CommandContext(ctx, path, cmdArgs...)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	// On context cancellation (SIGINT/SIGTERM to envd), ask the child to
	// terminate gracefully with SIGTERM rather than the default SIGKILL, and
	// force-kill only if it fails to exit within the grace period.
	//
	// If the process has already exited when cancellation fires, Signal returns
	// os.ErrProcessDone; treat that as success so it does not mask the real
	// exit status. Note: on Windows, Signal only honors Kill, so the graceful
	// SIGTERM path degrades to a forced termination there.
	c.Cancel = func() error {
		err := c.Process.Signal(syscall.SIGTERM)
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	c.WaitDelay = 10 * time.Second

	return c.Run()
}
