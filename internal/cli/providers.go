package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex/internal/tui"
	"github.com/start-cli/agentdex/modelsdev"
)

// newProvidersCmd is the providers noun group: a browse verb (list) and an exact
// fetch verb (get) over the models.dev providers agentdex can enrich against.
func (a *app) newProvidersCmd() *cobra.Command {
	return a.newNounCmd(
		"providers", "provider", "models.dev providers agentdex can enrich against",
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
		Short: "List the models.dev providers agentdex can enrich against",
		Long: "List the models.dev providers usable with --provider on agents and models, " +
			"with each provider's id, display name, API-key environment variables and whether they " +
			"are set, and its model count. Rows are ordered by id by default; --order-by sorts by " +
			"any field (for example models for model count) and --reverse flips the direction. The " +
			"optional filter narrows the list to providers whose id or name contains it " +
			"(case-insensitive); it is a browse narrowing, not a selector, so a filter matching " +
			"nothing prints an empty listing and exits 0.",
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
			return a.providersList(cmd, cfg.ModelsClient(), filter, fields, orderBy, reverse)
		},
	}
	registerFieldsFlag(cmd, &fields)
	registerOrderFlags(cmd, &orderBy, &reverse)
	addFieldsHelpSection(cmd, providerFieldSet)
	return cmd
}

// providersList fetches the merged models.dev catalog, narrows it by the filter,
// and reports the providers ordered by --order-by (by id by default). It reads
// env-var presence at the boundary through os.LookupEnv and loads no agent catalog:
// a provider listing is a pure models.dev surface.
func (a *app) providersList(cmd *cobra.Command, client *modelsdev.Client, filter string, fields []string, orderBy string, reverse bool) error {
	cat, err := client.Catalog(cmd.Context())
	if err != nil {
		return a.fail(cmd, providersCode(err), err)
	}

	needle := strings.ToLower(filter)
	var recs []*record
	for _, id := range sortedKeys(cat.Providers) {
		p := cat.Providers[id]
		if needle != "" && !matchesFilter(p.ID, p.Name, needle) {
			continue
		}
		recs = append(recs, providerRecord(p, envPresence(p.Env, os.LookupEnv)))
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
	return a.ok(cmd, data, nil, func(w io.Writer) {
		fmt.Fprintln(w)
		renderTable(w, headers, rows, "No providers.")
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
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			return a.providersGet(cmd, cfg.ModelsClient(), args[0], models, fields)
		},
	}
	cmd.Flags().BoolVar(&models, "models", false, "Fill the per-model table from models.dev")
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, providerFieldSet)
	return cmd
}

// providersGet fetches one provider by exact id and renders it as a detail view
// structurally mirroring agents get: the provider's facts, a symmetric-marker
// provider-env section, and its models as a count by default or the full table
// under --models. Env presence is read at the boundary through os.LookupEnv. An
// unknown id is not-found; an outage with no cache is transient and gross schema
// drift is config, per providersCode.
func (a *app) providersGet(cmd *cobra.Command, client *modelsdev.Client, id string, models bool, fields []string) error {
	p, found, err := client.Provider(cmd.Context(), id)
	if err != nil {
		return a.fail(cmd, providersCode(err), err)
	}
	if !found {
		return a.fail(cmd, codeNotFound, fmt.Errorf("no models.dev provider %q; run \"agentdex providers list\" to see provider ids", id))
	}
	a.log.Debug("providers get resolved", "provider", id, "models", models)

	present := envPresence(p.Env, os.LookupEnv)
	// Unlike agents get, the models array is always carried in JSON: a provider's
	// models are already in hand from this one fetch (no enrichment round-trip), so
	// --models governs only the text view, and .data.models stays uniform with
	// providers list.
	r := providerRecord(p, present)
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
		renderProviderDetail(w, fs, p, present, models)
	})
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

// envPresence reads whether each of a provider's API-key variables is set, through
// an injected lookup (os.LookupEnv in production) so record building is testable
// from inputs without t.Setenv. Only presence is read, never the value.
func envPresence(env []string, lookup func(string) (string, bool)) map[string]bool {
	present := make(map[string]bool, len(env))
	for _, name := range env {
		_, ok := lookup(name)
		present[name] = ok
	}
	return present
}

// providersCode classifies a providers-command failure. A no-result models.dev
// outage is transient; recognisable gross schema drift is a data fault, config,
// never transient. This mirrors the tail of modelsCode without the selector-match
// codes, which this browse surface cannot raise.
func providersCode(err error) int {
	if errors.Is(err, modelsdev.ErrModelsSchema) {
		return codeConfig
	}
	return codeTransient
}
