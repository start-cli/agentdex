// Command agentdex is the thin CLI over the agentdex detection library. Version,
// commit, and build date are injected at build time via ldflags into the cli
// package (-X github.com/start-cli/agentdex/internal/cli.Version=… and Commit, Date),
// which the version command reports.
package main

import (
	"os"

	"github.com/start-cli/agentdex/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
