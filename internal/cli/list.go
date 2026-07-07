package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
)

func (a *app) newListCmd() *cobra.Command {
	var models bool
	var all bool
	var fields []string
	cmd := &cobra.Command{
		Use:     "list",
		GroupID: groupCore,
		Short:   "List detected agents",
		Long: "List the AI coding agents detected on this machine. --all adds the catalogued " +
			"agents whose binary was not found, with \"missing\" in the BIN column. Model " +
			"enrichment is off by default to stay offline-fast once the catalog is cached; " +
			"--models opts in.",
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
			if all {
				opts = append(opts, agentdex.IncludeMissing())
			}
			if models {
				// list attaches a client only under --models so a default list never
				// blocks on the network.
				opts = append(opts, agentdex.WithModels(cfg.ModelsClient(), agentdex.EnrichModels()))
			}

			agents, err := agentdex.Detect(cmd.Context(), opts...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err)
			}
			a.log.Debug("list detected agents", "count", len(agents), "models", models, "all", all)

			// Under --all, detected agents read first; the not-found tail keeps the
			// library's by-id order within each group.
			sort.SliceStable(agents, func(i, j int) bool { return agents[i].Found && !agents[j].Found })

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
			empty := "No agents detected."
			if all {
				empty = "No agents catalogued."
			}
			return a.ok(cmd, data, warnings, func(w io.Writer) {
				fmt.Fprintln(w)
				renderTable(w, headers, rows, empty)
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Include catalogued agents that were not detected")
	cmd.Flags().BoolVar(&models, "models", false, "Enrich each agent with its models.dev models")
	registerFieldsFlag(cmd, &fields)
	return cmd
}
