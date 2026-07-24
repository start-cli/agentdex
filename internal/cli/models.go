package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
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
			idx, err := a.index(cmd)
			if err != nil {
				return err
			}
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			return a.modelsList(cmd, idx, agent, flattenProviders(providers), filter, fields, orderBy, reverse)
		},
	}
	registerFieldsFlag(cmd, &fields)
	registerOrderFlags(cmd, &orderBy, &reverse)
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "Scope to these models.dev provider ids (repeatable or csv)")
	cmd.Flags().StringVar(&agent, "agent", "", "Scope to the providers of this catalogued agent id")
	addFieldsHelpSection(cmd, modelFieldSet)
	return cmd
}

// modelsList browses models through the library, which owns scope resolution, the
// agnostic/home rules, canonical-id assignment, and the newest-first order. The CLI
// applies its arbitrary-field ordering, decides the provider column from the returned
// rows (R15), and renders. Scope faults arrive as typed errors mapped to exit codes
// with the CLI's own remedies (R7); stale-catalog warnings ride the return.
func (a *app) modelsList(cmd *cobra.Command, idx *agentdex.Index, agentID string, providers []string, filter string, fields []string, orderBy string, reverse bool) error {
	q := agentdex.ModelQuery{
		Scope:  agentdex.ModelScope{Agent: agentID, Providers: providers},
		Filter: filter,
	}
	res, err := idx.Models.List(cmd.Context(), q)
	warnings := libWarnings(res.Warnings)
	if err != nil {
		return a.fail(cmd, codeFor(err), modelScopeError(err), warnings...)
	}

	recs := make([]*record, len(res.Items))
	for i := range res.Items {
		m := res.Items[i]
		recs[i] = modelRecord(m.Model, m.Provider, m.CanonicalID)
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
		defaults := modelFieldSet.defaults
		// The provider id disambiguates otherwise identical rows and forms the get
		// composite, so surface it beside id when the returned rows span more than one
		// provider — decided from the rows, not the requested scope (R15).
		if distinctProviders(res.Items) > 1 {
			defaults = insertAfter(defaults, "id", "provider")
		}
		tableCols = orderColumns(defaults, sortKey)
	}
	data, headers, rows, err := tabulate(recs, fields, tableCols, modelFieldSet)
	if err != nil {
		return a.usage(cmd, err)
	}
	empty := emptyListMessage(filter, "models", "No models available.")
	return a.ok(cmd, data, warnings, func(w io.Writer) {
		fmt.Fprintln(w)
		renderTable(w, headers, rows, empty)
		if len(rows) > 0 {
			renderPriceFooter(w, tableCols)
		}
	})
}

// distinctProviders counts the distinct provider ids present in the returned model
// rows, driving the models table's provider column (R15).
func distinctProviders(models []agentdex.Model) int {
	seen := make(map[string]struct{}, len(models))
	for _, m := range models {
		seen[m.Provider] = struct{}{}
	}
	return len(seen)
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
			idx, err := a.index(cmd)
			if err != nil {
				return err
			}
			return a.modelsGet(cmd, idx, args[0], fields)
		},
	}
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, modelFieldSet)
	return cmd
}

// modelsGet fetches one model by its composite id through the library, which owns
// the first-slash split and canonical-id lookup (R9). A malformed composite is a
// usage fault carrying the models-list remedy; the two composite not-found messages
// are complete as the library sets them and are emitted verbatim (R7).
func (a *app) modelsGet(cmd *cobra.Command, idx *agentdex.Index, composite string, fields []string) error {
	m, err := idx.Models.Get(cmd.Context(), composite)
	if err != nil {
		return a.fail(cmd, codeFor(err), modelGetError(err))
	}

	r := modelRecord(m.Model, m.Provider, m.CanonicalID)
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

// modelGetError appends the CLI's own remedy clause to a malformed-composite fault,
// naming a subcommand only the CLI has (R7). It is separate from classification: the
// exit code is taken from the original sentinel by codeFor before this wrapping, so
// the added clause does not disturb the code (a plain errors.New would drop the wrap).
func modelGetError(err error) error {
	if errors.Is(err, agentdex.ErrMalformedModelID) {
		return errors.New(err.Error() + "; run \"agentdex models list\" to see model ids")
	}
	return err
}

// modelScopeError appends the CLI's own remedy clause to the model-scope faults the
// library owns, naming a flag or subcommand only the CLI has (R7). The exit code is
// taken from the underlying sentinel before this wrapping, so the added clause does
// not disturb classification.
func modelScopeError(err error) error {
	switch {
	case errors.Is(err, agentdex.ErrProvidersRequired):
		return errors.New(err.Error() + "; supply --provider with models.dev provider ids")
	case errors.Is(err, agentdex.ErrProvidersNotAllowed):
		return errors.New(err.Error() + "; --provider is only valid for provider-agnostic agents")
	case errors.Is(err, agentdex.ErrAgentUnknown):
		return errors.New(err.Error() + "; run \"agentdex agents list\" to see agent ids")
	default:
		return err
	}
}
