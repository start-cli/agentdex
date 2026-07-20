package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/config"
	"github.com/start-cli/agentdex/modelsdev"
)

// newModelsCmd is the models noun group: a browse verb (list) over models across
// providers and an exact fetch verb (get) by composite provider-id/model-id.
func (a *app) newModelsCmd() *cobra.Command {
	return a.newNounCmd(
		"models", "model", "Models available from models.dev providers",
		a.newModelsListCmd(),
		a.newModelsGetCmd(),
	)
}

func (a *app) newModelsListCmd() *cobra.Command {
	var (
		fields    []string
		providers []string
		agent     string
		orderBy   string
		reverse   bool
	)
	cmd := &cobra.Command{
		Use:   "list [filter]",
		Short: "List models across providers",
		Long: "List models across models.dev providers, with pricing, limits, and capabilities. " +
			"Rows are newest release first by default; --order-by sorts by any field (for example " +
			"total for combined price) and --reverse flips the direction. With no scope it lists " +
			"every provider's models; --provider scopes to the given models.dev provider ids and " +
			"--agent scopes to a catalogued agent's providers. The optional filter narrows the " +
			"listing to models whose id or name contains it (case-insensitive) and composes with " +
			"any scope. A provider-agnostic --agent requires --provider; a home-provider --agent " +
			"rejects it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			client := cfg.ModelsClient()
			providerSet, warnings, ferr := a.resolveModelsScope(cmd, cfg, client, agent, flattenProviders(providers))
			if ferr != nil {
				return ferr
			}
			return a.modelsList(cmd, providerSet, client, filter, fields, orderBy, reverse, warnings)
		},
	}
	registerFieldsFlag(cmd, &fields)
	registerOrderFlags(cmd, &orderBy, &reverse)
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "Scope to these models.dev provider ids (repeatable or csv)")
	cmd.Flags().StringVar(&agent, "agent", "", "Scope to the providers of this catalogued agent id")
	addFieldsHelpSection(cmd, modelFieldSet)
	return cmd
}

// resolveModelsScope resolves the provider set the listing spans from the --agent
// and --provider scopes, validating every caller-supplied provider id against a
// reachable models.dev in both roles it plays — the standalone direct scope and the
// enrichment set for an agnostic --agent — so an unknown id is a usage fault, never
// a silent empty listing. It returns any stale-catalog warnings alongside the set.
// A non-nil error is already rendered (an *exitError), so the caller returns it
// verbatim.
func (a *app) resolveModelsScope(cmd *cobra.Command, cfg *config.Config, client *modelsdev.Client, agentID string, callerProviders []string) ([]string, []string, error) {
	ctx := cmd.Context()
	if agentID != "" {
		cat, stale, err := agentdex.LoadCatalog(ctx, cfg.CatalogOptions(cfg.CatalogTTL)...)
		if err != nil {
			return nil, nil, a.fail(cmd, codeFor(err), err)
		}
		var warnings []string
		if stale {
			warnings = append(warnings, staleCatalogWarning)
		}
		ka, ok := cat.Agents[agentID]
		if !ok {
			return nil, nil, a.fail(cmd, codeNotFound, fmt.Errorf("no agent %q; run \"agentdex agents list\" to see agent ids", agentID), warnings...)
		}
		if ka.Agnostic {
			if len(callerProviders) == 0 {
				return nil, nil, a.fail(cmd, codeUsage, fmt.Errorf("%w: %q is provider-agnostic; supply --provider with models.dev provider ids", agentdex.ErrProvidersRequired, agentID), warnings...)
			}
			if err := agentdex.ValidateCallerProviders(ctx, client, callerProviders); err != nil {
				return nil, nil, a.fail(cmd, modelsCode(err), err, warnings...)
			}
			return callerProviders, warnings, nil
		}
		if len(callerProviders) > 0 {
			return nil, nil, a.fail(cmd, codeUsage, fmt.Errorf("agent %q has catalog providers; --provider is only valid for provider-agnostic agents", agentID), warnings...)
		}
		return ka.Provider, warnings, nil
	}

	if len(callerProviders) > 0 {
		if err := agentdex.ValidateCallerProviders(ctx, client, callerProviders); err != nil {
			return nil, nil, a.fail(cmd, modelsCode(err), err)
		}
		return callerProviders, nil, nil
	}

	// No scope: list across every provider models.dev knows.
	cat, err := client.Catalog(ctx)
	if err != nil {
		return nil, nil, a.fail(cmd, modelsCode(err), err)
	}
	return sortedKeys(cat.Providers), nil, nil
}

// modelsList reports every model the scoped providers offer, narrowed by the
// browse filter and ordered by --order-by (newest release first by default). A
// provider absent from a reachable models.dev contributes nothing; an outage with
// no cache is transient for this model-centric command.
func (a *app) modelsList(cmd *cobra.Command, providers []string, client *modelsdev.Client, filter string, fields []string, orderBy string, reverse bool, warnings []string) error {
	ctx := cmd.Context()
	agnostic, err := client.Catalog(ctx)
	if err != nil {
		return a.fail(cmd, modelsCode(err), err, warnings...)
	}

	needle := strings.ToLower(filter)
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
			m := p.Models[key]
			if needle != "" && !matchesFilter(m.ID, m.Name, needle) {
				continue
			}
			composite := pid + "/" + key
			canonical := ""
			if _, ok := agnostic.Models[composite]; ok {
				canonical = composite
			}
			recs = append(recs, modelRecord(m, pid, canonical))
		}
	}
	sortKey, err := applyOrder(recs, modelFieldSet, orderBy, reverse)
	if err != nil {
		return a.usage(cmd, err)
	}

	// The text table shows the declared default columns unless --fields overrides,
	// with the sort column pulled leftmost so the ordering is legible; the JSON
	// payload carries the full model record (driven by --fields) regardless.
	tableCols := fields
	if len(tableCols) == 0 {
		tableCols = orderColumns(modelFieldSet.defaults, sortKey)
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

func (a *app) newModelsGetCmd() *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:     "get <provider-id/model-id>",
		Aliases: []string{"view", "show"},
		Short:   "Show detail for one model",
		Long: "Show detail for one model, selected exactly by its composite provider-id/model-id " +
			"(for example anthropic/claude-opus-4-5). The composite splits on the first slash: the " +
			"prefix is the provider id and the whole remainder is the model key, which may itself " +
			"contain slashes. A value with no slash is a usage error; an unknown provider or model " +
			"is not-found (exit 3).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			return a.modelsGet(cmd, cfg.ModelsClient(), args[0], fields)
		},
	}
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, modelFieldSet)
	return cmd
}

// modelsGet fetches one model by its composite id, composed in the CLI from
// modelsdev.Client.Provider plus a map lookup rather than a library API. The
// composite splits on the first slash only — a models.dev provider id never
// contains a slash, while a model key may — so the prefix is the provider id and
// the whole remainder is the model key. canonical_id is computed from the full
// input composite as the agnostic-catalog lookup key.
func (a *app) modelsGet(cmd *cobra.Command, client *modelsdev.Client, composite string, fields []string) error {
	pid, key, ok := strings.Cut(composite, "/")
	if !ok {
		return a.usage(cmd, fmt.Errorf("model id %q must be provider-id/model-id; run \"agentdex models list\" to see model ids", composite))
	}
	ctx := cmd.Context()
	p, found, err := client.Provider(ctx, pid)
	if err != nil {
		return a.fail(cmd, modelsCode(err), err)
	}
	if !found {
		return a.fail(cmd, codeNotFound, fmt.Errorf("no model %q: unknown provider %q", composite, pid))
	}
	m, ok := p.Models[key]
	if !ok {
		return a.fail(cmd, codeNotFound, fmt.Errorf("no model %q in provider %q", composite, pid))
	}

	agnostic, err := client.Catalog(ctx)
	if err != nil {
		return a.fail(cmd, modelsCode(err), err)
	}
	canonical := ""
	if _, ok := agnostic.Models[composite]; ok {
		canonical = composite
	}
	a.log.Debug("models get resolved", "composite", composite, "provider", pid, "canonical", canonical)

	r := modelRecord(m, pid, canonical)
	fs, err := r.resolve(fields)
	if err != nil {
		return a.usage(cmd, err)
	}
	return a.ok(cmd, jsonObject(fs), nil, func(w io.Writer) {
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

// modelsCode classifies a models-command failure. Caller/usage faults (missing or
// unknown providers) are usage; recognisable schema drift is a data fault; anything
// else on this model-centric command is a models.dev outage, hence transient.
func modelsCode(err error) int {
	switch {
	case errors.Is(err, agentdex.ErrProvidersRequired), errors.Is(err, agentdex.ErrUnknownProvider):
		return codeUsage
	case errors.Is(err, modelsdev.ErrModelsSchema):
		return codeConfig
	default:
		return codeTransient
	}
}
