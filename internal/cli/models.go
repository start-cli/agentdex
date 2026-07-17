package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/match"
	"github.com/start-cli/agentdex/modelsdev"
)

func (a *app) newModelsCmd() *cobra.Command {
	var (
		fields    []string
		providers []string
	)
	cmd := &cobra.Command{
		Use:     "models <agent> [query]",
		GroupID: groupCore,
		Short:   "List the models available to an agent",
		Long: "List the provider models available to an agent, with pricing, limits, and " +
			"capabilities. With a query, fuzzy-match a single model; an ambiguous query lists candidates. " +
			"Provider-agnostic agents require --provider with one or more models.dev provider ids.",
		// MaximumNArgs, not RangeArgs(1, 2): the missing-agent case is handled in
		// RunE so it reports through the shared usage path — a helpful message that
		// carries the JSON envelope — rather than cobra's terse arg-count error.
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.usage(cmd, errors.New(`models requires an agent argument; run "agentdex list --all" to see agent ids`))
			}
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

			ka := cat.Agents[id]
			callerProviders := flattenProviders(providers)
			if !ka.Agnostic && len(callerProviders) > 0 {
				return a.fail(cmd, codeUsage, fmt.Errorf("agent %q has catalog providers; --provider is only valid for provider-agnostic agents", id), warnings...)
			}
			providerSet := ka.Provider
			if ka.Agnostic {
				if len(callerProviders) == 0 {
					return a.fail(cmd, codeUsage, fmt.Errorf("%w: %q is provider-agnostic; supply --provider with models.dev provider ids", agentdex.ErrProvidersRequired, id), warnings...)
				}
				providerSet = callerProviders
			}

			client := cfg.ModelsClient()
			if ka.Agnostic {
				if err := agentdex.ValidateCallerProviders(cmd.Context(), client, providerSet); err != nil {
					return a.fail(cmd, modelsCode(err), err, warnings...)
				}
			}
			if len(args) == 2 {
				return a.modelsOne(cmd, cat, id, args[1], client, providerSet, fields, warnings)
			}
			return a.modelsList(cmd, providerSet, client, fields, warnings)
		},
	}
	registerFieldsFlag(cmd, &fields)
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "models.dev provider ids for agnostic agents (repeatable or csv)")
	addFieldsHelpSection(cmd, modelFieldSet)
	return cmd
}

// modelsOne resolves a single model query through the library and reports it,
// exposing the canonical id when the model has a real models.dev agnostic entry.
func (a *app) modelsOne(cmd *cobra.Command, cat *agentdex.Catalog, id, query string, client *modelsdev.Client, providers, fields, warnings []string) error {
	m, providerID, canonicalID, err := cat.ResolveModel(cmd.Context(), id, query, client, providers)
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
			// --fields is the scripting surface: bare values, no footer.
			renderFields(w, fs)
			return
		}
		fmt.Fprintln(w)
		renderDetail(w, fs)
		renderPriceFooter(w, modelFieldSet.all)
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

	// Collect first, then order newest release first across all providers; the
	// records are built from the sorted listing so text and JSON agree.
	type entry struct {
		m         modelsdev.Model
		pid       string
		canonical string
	}
	var entries []entry
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
			entries = append(entries, entry{m: p.Models[key], pid: pid, canonical: canonical})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return newerModel(entries[i].m, entries[j].m) })
	recs := make([]*record, len(entries))
	for i, e := range entries {
		recs[i] = modelRecord(e.m, e.pid, e.canonical)
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
		fmt.Fprintln(w)
		renderTable(w, headers, rows, "No models available.")
		if len(rows) > 0 {
			renderPriceFooter(w, tableCols)
		}
	})
}

// modelsCode classifies a models-command failure. A no-match or ambiguous query
// is not-found; caller/usage faults (missing or unknown providers) are usage;
// recognisable schema drift is a data fault; anything else on this model-centric
// command is a models.dev outage, hence transient.
func modelsCode(err error) int {
	switch {
	case errors.Is(err, agentdex.ErrModelNotFound), errors.Is(err, agentdex.ErrModelAmbiguous):
		return codeNotFound
	case errors.Is(err, agentdex.ErrProvidersRequired), errors.Is(err, agentdex.ErrUnknownProvider):
		return codeUsage
	case errors.Is(err, modelsdev.ErrModelsSchema):
		return codeConfig
	default:
		return codeTransient
	}
}
