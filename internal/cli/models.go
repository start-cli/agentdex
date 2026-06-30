package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/match"
	"github.com/start-cli/agentdex/modelsdev"
)

func (a *app) newModelsCmd() *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:   "models <agent> [query]",
		Short: "List the models available to an agent",
		Long: "List the provider models available to an agent, with pricing, limits, and " +
			"capabilities. With a query, fuzzy-match a single model; an ambiguous query lists candidates.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}

			cat, stale, err := agentdex.LoadCatalog(cmd.Context(), cfg.CatalogOptions(cfg.CatalogTTL)...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err)
			}
			var warnings []string
			if stale {
				warnings = append(warnings, "agent catalog is stale: re-resolution failed, using the last resolved version")
			}

			outcome, id, candidates := matchAgent(cat, args[0])
			switch outcome {
			case match.None: // an agent query that matches nothing is not found here
				return a.fail(cmd, codeNotFound, fmt.Errorf("no agent matches %q; valid ids: %s", args[0], strings.Join(catalogIDs(cat), ", ")), warnings...)
			case match.Ambiguous:
				return a.fail(cmd, codeNotFound, fmt.Errorf("ambiguous agent %q: matches %s", args[0], strings.Join(candidates, ", ")), warnings...)
			}
			a.log.Debug("models resolved agent", "id", id)

			client := cfg.ModelsClient()
			if len(args) == 2 {
				return a.modelsOne(cmd, cat, id, args[1], client, fields, warnings)
			}
			return a.modelsList(cmd, cat.Agents[id].Provider, client, fields, warnings)
		},
	}
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "Select output fields (csv)")
	return cmd
}

// modelsOne resolves a single model query through the library and reports it,
// exposing the canonical id when the model has a real models.dev agnostic entry.
func (a *app) modelsOne(cmd *cobra.Command, cat *agentdex.Catalog, id, query string, client *modelsdev.Client, fields, warnings []string) error {
	m, providerID, canonicalID, err := cat.ResolveModel(cmd.Context(), id, query, client)
	if err != nil {
		return a.fail(cmd, modelsCode(err), err, warnings...)
	}
	a.log.Debug("models resolved query", "id", id, "model", m.ID, "provider", providerID, "canonical", canonicalID)

	r := modelRecord(m, providerID, canonicalID)
	fs, err := r.resolve(fields)
	if err != nil {
		return a.usage(cmd, err)
	}
	return a.ok(cmd, jsonObject(fs), warnings, func(w io.Writer) {
		if len(fields) > 0 {
			renderFields(w, fs)
			return
		}
		renderDetail(w, fs)
	})
}

// modelsList reports every model the agent's providers offer. A provider absent
// from a reachable models.dev contributes nothing; an outage with no cache is
// transient for this model-centric command.
func (a *app) modelsList(cmd *cobra.Command, providers []string, client *modelsdev.Client, fields, warnings []string) error {
	ctx := cmd.Context()
	agnostic, err := client.Catalog(ctx)
	if err != nil {
		return a.fail(cmd, modelsCode(err), err, warnings...)
	}

	var recs []*record
	for _, pid := range providers {
		p, found, err := client.Provider(ctx, pid)
		if err != nil {
			return a.fail(cmd, modelsCode(err), err, warnings...)
		}
		if !found {
			a.log.Debug("models provider absent from models.dev", "provider", pid)
			continue
		}
		for _, key := range sortedKeys(p.Models) {
			composite := pid + "/" + key
			canonical := ""
			if _, ok := agnostic.Models[composite]; ok {
				canonical = composite
			}
			recs = append(recs, modelRecord(p.Models[key], pid, canonical))
		}
	}

	// The text table shows the declared default columns unless --fields overrides;
	// the JSON payload carries the full model record (driven by --fields) regardless.
	tableCols := fields
	if len(tableCols) == 0 {
		tableCols = modelFieldSet.defaults
	}
	data, headers, rows, err := tabulate(recs, fields, tableCols, modelFieldSet)
	if err != nil {
		return a.usage(cmd, err)
	}
	return a.ok(cmd, data, warnings, func(w io.Writer) {
		renderTable(w, headers, rows, "No models available.")
	})
}

// modelsCode classifies a models-command failure. A no-match or ambiguous query
// is not-found; recognisable schema drift is a data fault; anything else on this
// model-centric command is a models.dev outage, hence transient.
func modelsCode(err error) int {
	switch {
	case errors.Is(err, agentdex.ErrModelNotFound), errors.Is(err, agentdex.ErrModelAmbiguous):
		return codeNotFound
	case errors.Is(err, modelsdev.ErrModelsSchema):
		return codeConfig
	default:
		return codeTransient
	}
}
