package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version, Commit, and Date are injected at build time via ldflags into this
// package. The defaults make a plain `go build` self-describing as a dev build
// rather than printing empty fields.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func (a *app) newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		GroupID: groupCore,
		Short:   "Print the agentdex version, commit, and build date",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data := map[string]any{"version": Version, "commit": Commit, "date": Date}
			return a.ok(cmd, data, nil, func(w io.Writer) {
				fmt.Fprintf(w, "agentdex %s (commit %s, built %s)\n", Version, Commit, Date)
			})
		},
	}
}
