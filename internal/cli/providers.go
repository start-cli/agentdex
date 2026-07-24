package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/tui"
	"github.com/start-cli/agentdex/modelsdev"
)

// newProvidersCmd is the providers noun group: a browse verb (list) and an exact
// fetch verb (get) over the model providers models.dev knows.
func (a *app) newProvidersCmd() *cobra.Command {
	return a.newNounCmd(
		"providers", "provider", "Model providers from models.dev and their API-key status",
		a.newProvidersListCmd(),
		a.newProvidersGetCmd(),
	)
}

func (a *app) newProvidersListCmd() *cobra.Command {
	var (
		fields  []string
		orderBy string
		reverse bool
	)
	cmd := &cobra.Command{
		Use:   "list [filter]",
		Short: "List model providers from models.dev",
		Long: "List the models.dev providers usable with --provider on agents and models, " +
			"with each provider's id, display name, API-key environment variables and whether they " +
			"are set, and its model count. Rows are ordered by id by default; --order-by sorts by " +
			"any field (for example models for model count) and --reverse flips the direction. The " +
			"optional filter narrows the list to providers whose id or name contains it " +
			"(case-insensitive); it is a browse narrowing, not a selector, so a filter matching " +
			"nothing prints an empty listing and exits 0.",
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
			return a.providersList(cmd, idx, filter, fields, orderBy, reverse)
		},
	}
	registerFieldsFlag(cmd, &fields)
	registerOrderFlags(cmd, &orderBy, &reverse)
	addFieldsHelpSection(cmd, providerFieldSet)
	return cmd
}

// providersList browses providers through the library, which returns them ordered
// by id with each API-key variable's presence resolved. It then applies the CLI's
// arbitrary-field ordering and renders. A provider listing loads no agent catalog
// and raises no warnings.
func (a *app) providersList(cmd *cobra.Command, idx *agentdex.Index, filter string, fields []string, orderBy string, reverse bool) error {
	res, err := idx.Providers.List(cmd.Context(), agentdex.ProviderQuery{Filter: filter})
	if err != nil {
		return a.fail(cmd, codeFor(err), err)
	}

	recs := make([]*record, len(res.Items))
	for i := range res.Items {
		p := res.Items[i]
		recs[i] = providerRecord(p.Provider, p.EnvPresent)
	}
	sortKey, err := applyOrder(recs, providerFieldSet, orderBy, reverse)
	if err != nil {
		return a.usage(cmd, err)
	}

	tableCols := fields
	if len(tableCols) == 0 {
		tableCols = orderColumns(providerFieldSet.defaults, sortKey)
	}
	data, headers, rows, err := tabulate(recs, fields, tableCols, providerFieldSet)
	if err != nil {
		return a.usage(cmd, err)
	}
	empty := emptyListMessage(filter, "providers", "No providers.")
	return a.ok(cmd, data, nil, func(w io.Writer) {
		fmt.Fprintln(w)
		renderTable(w, headers, rows, empty)
	})
}

func (a *app) newProvidersGetCmd() *cobra.Command {
	var (
		models bool
		fields []string
	)
	cmd := &cobra.Command{
		Use:     "get <id>",
		Aliases: []string{"view", "show"},
		Short:   "Show detail for one models.dev provider",
		Long: "Show detail for one models.dev provider, selected exactly by its provider id: " +
			"its facts (id, name, doc, npm, api), its API-key env presence, and its model count. " +
			"Pass --models to fill the full model table. An id that names no provider is not-found " +
			"(exit 3).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idx, err := a.index(cmd)
			if err != nil {
				return err
			}
			return a.providersGet(cmd, idx, args[0], models, fields)
		},
	}
	cmd.Flags().BoolVar(&models, "models", false, "Fill the per-model table from models.dev")
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, providerFieldSet)
	return cmd
}

// providersGet fetches one provider by exact id through the library and renders it
// as a detail view structurally mirroring agents get: the provider's facts, a
// symmetric-marker provider-env section, and its models as a count by default or the
// full table under --models. An unknown id is not-found, with the CLI naming the
// providers-list remedy the library does not own (R7).
func (a *app) providersGet(cmd *cobra.Command, idx *agentdex.Index, id string, models bool, fields []string) error {
	p, err := idx.Providers.Get(cmd.Context(), id)
	if err != nil {
		return a.fail(cmd, codeFor(err), providersGetError(err))
	}
	present := p.EnvPresent
	// Unlike agents get, the models array is always carried in JSON: a provider's
	// models are already in hand from this one fetch (no enrichment round-trip), so
	// --models governs only the text view, and .data.models stays uniform with
	// providers list.
	r := providerRecord(p.Provider, present)
	fs, err := r.resolve(fields)
	if err != nil {
		return a.usage(cmd, err)
	}
	return a.ok(cmd, jsonObject(fs), nil, func(w io.Writer) {
		if len(fields) > 0 {
			// --fields is the scripting surface: bare values, no sections.
			renderFields(w, fs)
			return
		}
		// In the detail branch fields is empty, so fs is the full resolved record.
		renderProviderDetail(w, fs, p.Provider, present, models)
	})
}

// providersGetError appends the CLI's own remedy clause to a provider not-found
// fault, naming a subcommand only the CLI has (R7). It is kept separate from
// classification so codeFor reads the original ErrNotFound sentinel before this
// wrapping — a plain errors.New would drop the wrap and misclassify the exit code.
func providersGetError(err error) error {
	if errors.Is(err, agentdex.ErrNotFound) {
		return errors.New(err.Error() + "; run \"agentdex providers list\" to see provider ids")
	}
	return err
}

// providerFactFields are the scalar facts rendered inline in the provider detail
// view; env, present, and models are rendered as their own sections below.
var providerFactFields = map[string]bool{"id": true, "name": true, "doc": true, "npm": true, "api": true}

// renderProviderDetail writes the provider detail view, mirroring the agent detail
// structure: inline facts drawn from the already-resolved record, a Provider env
// section with the symmetric (set)/(unset) markers get uses, then models as a count
// line by default or the full table under showModels.
func renderProviderDetail(w io.Writer, fs []field, p modelsdev.Provider, present map[string]bool, showModels bool) {
	detail := make([]field, 0, len(fs))
	for _, f := range fs {
		if !providerFactFields[f.key] {
			continue
		}
		if f.key == "doc" && f.text != "-" {
			f.text = tui.URL.Sprint(f.text)
		}
		detail = append(detail, f)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, tui.Header.Sprint("Provider"))
	renderDetail(w, detail)

	fmt.Fprintln(w)
	fmt.Fprintln(w, tui.Header.Sprint("Provider env"))
	fmt.Fprintln(w, "  "+styledProviderEnv(present))

	fmt.Fprintln(w)
	fmt.Fprintln(w, tui.Header.Sprint("Models"))
	if !showModels {
		fmt.Fprintf(w, "  %d\n", len(p.Models))
		return
	}
	models := make([]modelsdev.Model, 0, len(p.Models))
	for _, key := range sortedKeys(p.Models) {
		models = append(models, p.Models[key])
	}
	sortModelsNewest(models)
	recs := make([]*record, len(models))
	for i, m := range models {
		recs[i] = modelRecord(m, p.ID, "")
	}
	_, headers, rows, _ := tabulate(recs, nil, modelFieldSet.defaults, modelFieldSet)
	renderTable(w, headers, rows, "  (none)")
	if len(rows) > 0 {
		renderPriceFooter(w, modelFieldSet.defaults)
	}
}
