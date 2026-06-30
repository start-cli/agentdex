package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
)

func (a *app) newListCmd() *cobra.Command {
	var models bool
	var fields []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List detected agents",
		Long: "List the AI coding agents detected on this machine. Model enrichment is " +
			"off by default to stay offline-fast once the catalog is cached; --models opts in.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			flags, err := a.mapFlags()
			if err != nil {
				return a.usage(cmd, err)
			}

			cat, stale, err := agentdex.LoadCatalog(cmd.Context(), cfg.CatalogOptions(cfg.CatalogTTL)...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err)
			}
			var warnings []string
			if stale {
				warnings = append(warnings, "agent catalog is stale: re-resolution failed, using the last resolved version")
			}

			opts := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat))
			if models {
				// list attaches a client only under --models so a default list never
				// blocks on the network.
				opts = append(opts, agentdex.WithModels(cfg.ModelsClient(), agentdex.EnrichModels()))
			}

			agents, err := agentdex.Detect(cmd.Context(), opts...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err)
			}
			a.log.Debug("list detected agents", "count", len(agents), "models", models)

			recs := make([]*record, len(agents))
			for i := range agents {
				r := agentRecord(&agents[i])
				if models {
					withModels(r, agents[i].Models)
				}
				recs[i] = r
			}

			// Compose the table columns: --verbose widens them, --models appends a
			// model-count column. These are text-table affordances only; the JSON
			// payload always carries the full record (driven by the user's --fields
			// selection), so neither widens it. An explicit --fields wins over both.
			tableCols := fields
			if len(tableCols) == 0 {
				tableCols = agentFieldSet.defaults
				if a.verbose {
					tableCols = agentVerboseFields
				}
				if models {
					tableCols = append(append([]string(nil), tableCols...), "models")
				}
			}

			data, headers, rows, err := tabulate(recs, fields, tableCols, agentFieldSet)
			if err != nil {
				return a.usage(cmd, err)
			}
			return a.ok(cmd, data, warnings, func(w io.Writer) {
				renderTable(w, headers, rows, "No agents detected.")
			})
		},
	}
	cmd.Flags().BoolVar(&models, "models", false, "Enrich each agent with its models.dev models")
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "Select output fields (csv)")
	return cmd
}
