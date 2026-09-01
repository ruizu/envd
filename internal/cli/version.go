package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build metadata. These are intended to be overridden at build time via
// -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/ruizu/envd/internal/cli.version=v1.2.3 \
//	  -X github.com/ruizu/envd/internal/cli.commit=abc1234 \
//	  -X github.com/ruizu/envd/internal/cli.date=2026-09-01"
//
// When left unset, versionInfo falls back to Go's embedded build info.
var (
	version = ""
	commit  = ""
	date    = ""
)

// versionInfo returns the resolved version, commit, and date, filling in any
// unset values from the binary's embedded build info when available.
func versionInfo() (v, c, d string) {
	v, c, d = version, commit, date

	if v == "" || c == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if v == "" && bi.Main.Version != "" {
				v = bi.Main.Version
			}
			for _, s := range bi.Settings {
				if c == "" && s.Key == "vcs.revision" {
					c = s.Value
				}
				if d == "" && s.Key == "vcs.time" {
					d = s.Value
				}
			}
		}
	}

	if v == "" {
		v = "(devel)"
	}
	if c == "" {
		c = "unknown"
	}
	if d == "" {
		d = "unknown"
	}
	return v, c, d
}

// newVersionCmd returns the `version` subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, c, d := versionInfo()
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"envd %s\n  commit: %s\n  built:  %s\n  go:     %s\n",
				v, c, d, runtime.Version())
			return err
		},
	}
}
