package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/modelsdev"
)

func (a *app) newListCmd() *cobra.Command {
	var (
		all       bool
		fields    []string
		providers []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		GroupID: groupCore,
		Short:   "List detected agents",
		Long: "List the AI coding agents detected on this machine. Each agent's model count " +
			"is enriched from models.dev, served from the local cache when warm and degrading " +
			"to zero when models.dev cannot be reached. Provider-agnostic agents show \"-\" " +
			"unless --provider is given. --all adds the catalogued agents whose binary was " +
			"not found, with \"missing\" in the BIN column.",
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

			callerProviders := flattenProviders(providers)
			base := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat))
			if len(callerProviders) > 0 {
				base = append(base, agentdex.WithProviders(callerProviders...))
			}
			if all {
				base = append(base, agentdex.IncludeMissing())
			}
			// Probe models.dev once for reachability, reusing this client for the
			// enrichment below so the catalog is fetched at most once (the client
			// memoises it). Enrichment is served from the warm cache with no network
			// and degrades to a nil model list when models.dev is unreachable, so a
			// model count column is shown without ever failing the listing. But an
			// unreachable-and-uncached models.dev warns, so the resulting zero reads as
			// "unavailable" rather than a genuine empty catalog — as loud as get's
			// degrade and the schema-drift branch below. A malformed catalog is left to
			// that branch (Catalog does not surface per-provider drift). The defensive
			// copy keeps base intact for the schema-drift fallback.
			client := cfg.ModelsClient()
			// Validate caller-supplied provider ids at the boundary so an unknown id
			// is a usage fault regardless of whether an agnostic agent is installed to
			// enrich against it. Schema drift and unreachability defer to the
			// enrichment path's existing tolerance below.
			if len(callerProviders) > 0 {
				if verr := agentdex.ValidateCallerProviders(cmd.Context(), client, callerProviders); errors.Is(verr, agentdex.ErrUnknownProvider) {
					return a.fail(cmd, codeFor(verr), verr, warnings...)
				}
			}
			opts := append(append([]agentdex.Option(nil), base...), agentdex.WithModels(client, agentdex.EnrichModels()))
			if _, cerr := client.Catalog(cmd.Context()); cerr != nil && !errors.Is(cerr, modelsdev.ErrModelsSchema) {
				warnings = append(warnings, "model counts unavailable: models.dev is unreachable and not cached")
				opts = base
			}

			agents, err := agentdex.Detect(cmd.Context(), opts...)
			if errors.Is(err, modelsdev.ErrModelsSchema) {
				// Malformed models.dev data would otherwise kill the whole listing over
				// an auxiliary column. Detection itself is sound, so re-detect without
				// enrichment and warn: the drift stays loud, but list keeps working.
				warnings = append(warnings, fmt.Sprintf("model counts omitted: %v", err))
				agents, err = agentdex.Detect(cmd.Context(), base...)
			}
			if err != nil {
				// Unknown caller providers are usage faults; list otherwise soft-skips
				// agnostic agents without providers so a mixed catalog never fails.
				return a.fail(cmd, codeFor(err), err)
			}
			a.log.Debug("list detected agents", "count", len(agents), "all", all)

			// Under --all, detected agents read first; the not-found tail keeps the
			// library's by-id order within each group.
			sort.SliceStable(agents, func(i, j int) bool { return agents[i].Found && !agents[j].Found })

			recs := make([]*record, len(agents))
			for i := range agents {
				r := agentRecord(&agents[i])
				ka, ok := cat.Agents[agents[i].ID]
				if ok && ka.Agnostic && len(callerProviders) == 0 {
					// Not applicable: JSON null / text "-", not the degrade [] / 0 shape.
					withModelsNA(r)
				} else {
					withModels(r, agents[i].Models)
				}
				recs[i] = r
			}

			// Compose the table columns: --verbose widens them. This is a text-table
			// affordance only; the JSON payload always carries the full record (driven
			// by the user's --fields selection), so it is unaffected. An explicit
			// --fields wins over both.
			tableCols := fields
			if len(tableCols) == 0 {
				tableCols = agentFieldSet.defaults
				if a.verbose {
					tableCols = agentVerboseFields
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
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "models.dev provider ids for agnostic agents' model counts (repeatable or csv)")
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, agentFieldSet)
	return cmd
}
