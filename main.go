// Command envd loads secrets from a secret backend, sets them as environment
// variables, and then executes the provided command.
package main

import (
	"os"

	"github.com/ruizu/envd/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
