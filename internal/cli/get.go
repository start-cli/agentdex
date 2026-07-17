package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/config"
	"github.com/start-cli/agentdex/internal/match"
	"github.com/start-cli/agentdex/internal/tui"
	"github.com/start-cli/agentdex/modelsdev"
)

func (a *app) newGetCmd() *cobra.Command {
	var (
		models    bool
		fields    []string
		providers []string
	)
	cmd := &cobra.Command{
		Use:     "get <agent>",
		Aliases: []string{"view", "show"},
		GroupID: groupCore,
		Short:   "Show detail for one agent",
		Long: "Show detection detail for one agent: its binary, version, config and skills " +
			"paths, and provider-env presence. Models are off by default; pass --models or " +
			"include models in --fields to fill the per-model list. Provider-agnostic agents " +
			"omit provider fields until --provider is supplied.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			outcome, id, candidates := matchAgent(cat, args[0])
			switch outcome {
			case match.Ambiguous:
				return a.fail(cmd, codeNotFound, fmt.Errorf("ambiguous agent %q: matches %s", args[0], strings.Join(candidates, ", ")), warnings...)
			case match.None:
				// The provider fallthrough reports a models.dev provider, not an
				// agent; --provider is meaningless there and is rejected rather
				// than silently dropped.
				if len(callerProviders) > 0 {
					return a.fail(cmd, codeUsage, fmt.Errorf("%q is not a catalogued agent; --provider is only valid for provider-agnostic agents", args[0]), warnings...)
				}
				return a.getFallthrough(cmd, cfg.ModelsClient(), cat, args[0], models, warnings)
			}
			a.log.Debug("get resolved agent", "id", id)

			ka := cat.Agents[id]
			if !ka.Agnostic && len(callerProviders) > 0 {
				return a.fail(cmd, codeUsage, fmt.Errorf("agent %q has catalog providers; --provider is only valid for provider-agnostic agents", id), warnings...)
			}

			// Soft path: unfiltered agnostic get without --provider and without --models.
			softPath := ka.Agnostic && len(callerProviders) == 0 && len(fields) == 0 && !models
			if softPath {
				return a.getAgnosticSoftPath(cmd, cfg, flags, cat, id, warnings)
			}
			if ka.Agnostic && len(callerProviders) == 0 && providerRelatedDemand(models, fields) {
				return a.fail(cmd, codeUsage, fmt.Errorf("%w: %q is provider-agnostic; supply --provider with models.dev provider ids", agentdex.ErrProvidersRequired, id), warnings...)
			}

			detectOpts := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat))
			if len(callerProviders) > 0 {
				detectOpts = append(detectOpts, agentdex.WithProviders(callerProviders...))
			}
			agent, found, err := agentdex.DetectOne(cmd.Context(), id, detectOpts...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err, warnings...)
			}
			if !found {
				// A not-installed agent triggers no models.dev round-trip, so an
				// agnostic agent's caller-supplied providers are reported here as-is,
				// unvalidated; unknown ids are rejected only on the Found path
				// (getAgnosticEnrich). The catalogued detail still reports.
				err := fmt.Errorf("agent %q (%s) is catalogued but not installed", id, agent.Name)
				return a.reportAgentError(cmd, agent, fields, codeNotFound, err, warnings)
			}

			// Non-models.dev field selection: offline catalog facts only. An
			// agnostic agent with caller-supplied providers is excluded whenever a
			// provider-related field is selected: providers is caller input there,
			// not catalog truth, so it must pass validation before being reported.
			callerDataDemand := ka.Agnostic && providerRelatedDemand(models, fields)
			if !modelsDevDemand(fields) && !callerDataDemand {
				return a.reportAgent(cmd, agent, fields, warnings)
			}

			if ka.Agnostic {
				return a.getAgnosticEnrich(cmd, cfg, flags, cat, agent, callerProviders, modelsDemand(models, fields), fields, warnings)
			}
			return a.getCoverage(cmd, cfg, flags, cat, agent, modelsDemand(models, fields), fields, warnings)
		},
	}
	cmd.Flags().BoolVar(&models, "models", false, "Fill the per-model list from models.dev")
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "models.dev provider ids for agnostic agents (repeatable or csv)")
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, agentFieldSet)
	return cmd
}

// modelsDemand is the OR rule for catalog-agent Models fill: --models, or a
// non-empty --fields selection that includes models. Empty fields is unfiltered
// and does not demand Models on its own.
func modelsDemand(modelsFlag bool, fields []string) bool {
	if modelsFlag {
		return true
	}
	for _, f := range fields {
		if f == "models" {
			return true
		}
	}
	return false
}

// providerRelatedDemand is the first gate: soft-path / ErrProvidersRequired
// demand. True when the requested output intersects {providers, provider_env,
// models}, or --models is set. Unfiltered (empty fields, no --models) counts as
// demanding all three.
func providerRelatedDemand(modelsFlag bool, fields []string) bool {
	if modelsFlag {
		return true
	}
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		switch f {
		case "providers", "provider_env", "models":
			return true
		}
	}
	return false
}

// modelsDevDemand is the second gate: attach models.dev and enter getCoverage
// only when the requested output intersects {provider_env, models}. Unfiltered
// get demands the provider-env path. providers alone is offline catalog data.
func modelsDevDemand(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		if f == "provider_env" || f == "models" {
			return true
		}
	}
	return false
}

// flattenProviders normalises --provider values: StringSlice already csv-splits,
// but empty entries from accidental commas are dropped, as are duplicate ids
// (a repeated id would double-list models and break unique query resolution).
func flattenProviders(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// getAgnosticSoftPath is unfiltered get on an agnostic agent without --provider:
// outside facts only, omit the three provider-related fields, warn how to enrich.
// Exit 0 when Found; exit 3 with the not-installed error when not Found. The
// soft path is structurally unfiltered — a --fields selection never enters it.
func (a *app) getAgnosticSoftPath(cmd *cobra.Command, cfg *config.Config, flags config.Flags, cat *agentdex.Catalog, id string, warnings []string) error {
	detectOpts := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat))
	agent, found, err := agentdex.DetectOne(cmd.Context(), id, detectOpts...)
	if err != nil {
		return a.fail(cmd, codeFor(err), err, warnings...)
	}
	warnings = append(warnings, fmt.Sprintf("%q is provider-agnostic: supply --provider with models.dev provider ids to enrich providers, provider-env, and models", id))
	if !found {
		err := fmt.Errorf("agent %q (%s) is catalogued but not installed", id, agent.Name)
		return a.reportSoftPathAgentError(cmd, agent, codeNotFound, err, warnings)
	}
	return a.reportSoftPathAgent(cmd, agent, warnings)
}

// getAgnosticEnrich enriches a found agnostic agent against caller-supplied
// providers with no catalog-data-error rollup. Unknown provider ids surface as
// ErrUnknownProvider (usage). Models fill follows modelsDemand.
func (a *app) getAgnosticEnrich(cmd *cobra.Command, cfg *config.Config, flags config.Flags, cat *agentdex.Catalog, agent *agentdex.Agent, providers []string, enrich bool, fields, warnings []string) error {
	client := cfg.ModelsClient()
	mopts := []agentdex.ModelsOption{}
	if enrich {
		mopts = append(mopts, agentdex.EnrichModels())
	}
	enrichOpts := append(cfg.LibraryOptions(flags),
		agentdex.WithCatalog(cat),
		agentdex.WithProviders(providers...),
		agentdex.WithModels(client, mopts...),
		agentdex.WithSkipVersion(),
	)
	full, _, err := agentdex.DetectOne(cmd.Context(), agent.ID, enrichOpts...)
	if err != nil {
		return a.fail(cmd, codeFor(err), err, warnings...)
	}
	if full.ProviderEnv == nil {
		// With a non-empty caller provider set, a nil ProviderEnv means enrich
		// degraded: models.dev was never consulted. Warn like getCoverage does.
		warnings = append(warnings, "models.dev is unreachable and not cached: model enrichment and provider-env omitted")
	}
	full.Version = agent.Version
	sortModelsNewest(full.Models)
	return a.reportAgent(cmd, full, fields, warnings)
}

// addHelpSection injects a titled, pre-formatted section into the command's help,
// rendered between Flags and Global Flags so it mirrors cobra's own section layout
// rather than being buried in the description. body is emitted verbatim, so the
// caller owns its indentation and line breaks.
func addHelpSection(cmd *cobra.Command, title, body string) {
	section := "\n\n" + title + ":\n" + body
	tmpl := strings.Replace(cmd.UsageTemplate(),
		"{{if .HasAvailableInheritedFlags}}",
		section+"{{if .HasAvailableInheritedFlags}}", 1)
	cmd.SetUsageTemplate(tmpl)
}

// addFieldsHelpSection injects a "Fields" section listing the valid --fields keys.
// The list is drawn from the field set --fields validates against, so the help can
// never drift from what is accepted.
func addFieldsHelpSection(cmd *cobra.Command, set fieldSet) {
	// Split the keys across two indented rows so the section stays compact rather
	// than running to one wide line.
	half := (len(set.all) + 1) / 2
	rows := "  " + strings.Join(set.all[:half], ", ") + "\n  " + strings.Join(set.all[half:], ", ")
	addHelpSection(cmd, "Fields", rows)
}

// registerFieldsFlag adds the shared --fields flag and accepts the singular
// --field as an alias, so a common slip resolves to the same flag instead of
// failing with an unknown-flag usage error. The alias is invisible in help;
// --fields stays the one documented name.
func registerFieldsFlag(cmd *cobra.Command, fields *[]string) {
	cmd.Flags().StringSliceVar(fields, "fields", nil, "Select output fields (csv)")
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "field" {
			name = "fields"
		}
		return pflag.NormalizedName(name)
	})
}

// coverage is the per-provider models.dev rollup verdict for a detected agent.
type coverage int

const (
	coverageAllPresent coverage = iota
	coverageSomePresent
	coverageNonePresent
	coverageNoProviders
	coverageUnreachable
	coverageSchema
)

// rollup probes each provider through the public modelsdev client and composes the
// agent-level verdict. It branches on the schema sentinel before the outage verdict
// so a reachable models.dev serving malformed data is always a data fault, never
// misread as an outage — whether the drift is one provider's model (found, error)
// or a whole-document fault that fails the load (not found, error). Only a non-schema
// load failure is an outage. The first provider call that fails to load decides the
// outage verdict; once any call succeeds the catalog is memoised and no later call
// can report an outage.
func (a *app) rollup(ctx context.Context, client *modelsdev.Client, providers []string) (coverage, []string, []string, error) {
	if len(providers) == 0 {
		return coverageNoProviders, nil, nil, nil
	}
	var present, absent []string
	for _, pid := range providers {
		_, found, err := client.Provider(ctx, pid)
		switch {
		case errors.Is(err, modelsdev.ErrModelsSchema):
			return coverageSchema, nil, nil, err
		case err != nil:
			return coverageUnreachable, nil, nil, err
		case !found:
			absent = append(absent, pid)
		default:
			present = append(present, pid)
		}
	}
	switch {
	case len(present) == 0:
		return coverageNonePresent, present, absent, nil
	case len(absent) > 0:
		return coverageSomePresent, present, absent, nil
	default:
		return coverageAllPresent, present, absent, nil
	}
}

// getCoverage applies the coverage rollup table to a detected agent: it reports
// and exits per the verdict, enriching from the present providers on the exit-0
// rows and keeping the unreachable degrade distinct from the absent-provider data
// fault.
func (a *app) getCoverage(cmd *cobra.Command, cfg *config.Config, flags config.Flags, cat *agentdex.Catalog, agent *agentdex.Agent, enrich bool, fields, warnings []string) error {
	client := cfg.ModelsClient()
	verdict, present, absent, rerr := a.rollup(cmd.Context(), client, agent.Providers)
	a.log.Debug("get coverage rollup", "agent", agent.ID, "verdict", verdict, "present", present, "absent", absent)

	switch verdict {
	case coverageUnreachable:
		warnings = append(warnings, "models.dev is unreachable and not cached: model enrichment and provider-env omitted")
		return a.reportAgent(cmd, agent, fields, warnings)

	case coverageSchema:
		return a.reportAgentError(cmd, agent, fields, codeConfig, rerr, warnings)

	case coverageNonePresent:
		err := fmt.Errorf("catalog data error: no provider of %q is present in models.dev (providers: %s)", agent.ID, strings.Join(agent.Providers, ", "))
		return a.reportAgentError(cmd, agent, fields, codeConfig, err, warnings)

	case coverageNoProviders:
		return a.reportAgent(cmd, agent, fields, warnings)
	}

	// All-present or some-present: enrich from the present providers for display.
	// Provider-env shows regardless of enrich, so a client is always attached. The
	// version was already probed by the first detection, so this pass skips the exec
	// and carries that version forward — a successful get probes the binary once.
	mopts := []agentdex.ModelsOption{}
	if enrich {
		mopts = append(mopts, agentdex.EnrichModels())
	}
	enrichOpts := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat), agentdex.WithModels(client, mopts...), agentdex.WithSkipVersion())
	full, _, err := agentdex.DetectOne(cmd.Context(), agent.ID, enrichOpts...)
	if err != nil {
		return a.fail(cmd, codeFor(err), err, warnings...)
	}
	full.Version = agent.Version
	// Newest release first for display; both the text table and the JSON models
	// array follow this order.
	sortModelsNewest(full.Models)
	if verdict == coverageSomePresent {
		warnings = append(warnings, fmt.Sprintf("some providers are absent from models.dev: %s", strings.Join(absent, ", ")))
	}
	return a.reportAgent(cmd, full, fields, warnings)
}

// getFallthrough handles a query that matched no catalog agent: it is classified
// against models.dev's providers. A unique provider match reports that provider
// at exit 3; the models dump is included only when withModels is true. No provider
// match is genuinely unknown at exit 2. If models.dev cannot be reached the query
// cannot be classified at all, so it exits transient rather than asserting either
// verdict.
func (a *app) getFallthrough(cmd *cobra.Command, client *modelsdev.Client, cat *agentdex.Catalog, query string, withModels bool, warnings []string) error {
	outcome, prov, _, err := matchProvider(cmd.Context(), client, query)
	if err != nil {
		return a.fail(cmd, codeTransient, fmt.Errorf("cannot classify %q: models.dev is unreachable: %w", query, err), warnings...)
	}
	if outcome != match.Unique {
		return a.fail(cmd, codeUsage, fmt.Errorf("no such agent %q; valid ids: %s", query, strings.Join(catalogIDs(cat), ", ")), warnings...)
	}
	a.log.Debug("get fallthrough matched provider", "query", query, "provider", prov.ID, "models", withModels)

	data := map[string]any{
		"provider": prov.ID,
		"name":     prov.Name,
	}
	note := fmt.Errorf("%q is not a catalogued agent; showing models.dev provider %q (install details unavailable, not catalogued)", query, prov.ID)

	var modelRecs []*record
	if withModels {
		models := make([]modelsdev.Model, 0, len(prov.Models))
		for _, key := range sortedKeys(prov.Models) {
			models = append(models, prov.Models[key])
		}
		sortModelsNewest(models)
		modelRecs = make([]*record, len(models))
		for i, m := range models {
			modelRecs[i] = modelRecord(m, prov.ID, "")
		}
		data["models"] = jsonRecords(modelRecs)
	}

	return a.failData(cmd, codeNotFound, note, data, func(w io.Writer) {
		fmt.Fprintln(w)
		fmt.Fprintln(w, tui.Header.Sprint("Provider"))
		renderDetail(w, []field{
			{key: "provider", text: prov.ID},
			{key: "name", text: prov.Name},
		})
		if !withModels {
			return
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, tui.Header.Sprint("Models"))
		_, headers, rows, _ := tabulate(modelRecs, nil, modelFieldSet.defaults, modelFieldSet)
		renderTable(w, headers, rows, "No models.")
		if len(rows) > 0 {
			renderPriceFooter(w, modelFieldSet.defaults)
		}
	}, warnings)
}

// reportAgent renders a detected agent at exit 0: the JSON record under --json,
// selected fields for scripting under --fields, otherwise the full detail view.
func (a *app) reportAgent(cmd *cobra.Command, agent *agentdex.Agent, fields, warnings []string) error {
	r := agentReportRecord(agent)
	fs, err := r.resolve(fields)
	if err != nil {
		return a.usage(cmd, err)
	}
	return a.ok(cmd, jsonObject(fs), warnings, func(w io.Writer) {
		if len(fields) > 0 {
			renderFields(w, fs)
			return
		}
		renderAgentDetail(w, agent, a.verbose)
	})
}

// reportAgentError reports the agent and then fails: the data-fault rows that
// surface the agent and still exit non-zero.
func (a *app) reportAgentError(cmd *cobra.Command, agent *agentdex.Agent, fields []string, code int, cause error, warnings []string) error {
	r := agentReportRecord(agent)
	fs, ferr := r.resolve(fields)
	if ferr != nil {
		return a.usage(cmd, ferr)
	}
	return a.failData(cmd, code, cause, jsonObject(fs), func(w io.Writer) {
		if len(fields) > 0 {
			renderFields(w, fs)
			return
		}
		renderAgentDetail(w, agent, a.verbose)
	}, warnings)
}

// agentReportRecord builds the get record, including provider-env and the enriched
// models list when they were populated.
func agentReportRecord(agent *agentdex.Agent) *record {
	r := agentRecord(agent)
	withProviderEnv(r, agent.ProviderEnv)
	if agent.Models != nil {
		withModels(r, agent.Models)
	}
	return r
}

// reportSoftPathAgent renders the soft-path success payload at exit 0: the
// without-providers record, so the three provider-related keys are absent.
func (a *app) reportSoftPathAgent(cmd *cobra.Command, agent *agentdex.Agent, warnings []string) error {
	r := agentRecordWithoutProviders(agent)
	fs, _ := r.resolve(nil)
	return a.ok(cmd, jsonObject(fs), warnings, func(w io.Writer) {
		renderAgentDetailFields(w, r, agent, a.verbose)
	})
}

// reportSoftPathAgentError is reportSoftPathAgent for not-installed (exit 3).
func (a *app) reportSoftPathAgentError(cmd *cobra.Command, agent *agentdex.Agent, code int, cause error, warnings []string) error {
	r := agentRecordWithoutProviders(agent)
	fs, _ := r.resolve(nil)
	return a.failData(cmd, code, cause, jsonObject(fs), func(w io.Writer) {
		renderAgentDetailFields(w, r, agent, a.verbose)
	}, warnings)
}

// detailSections are the record fields rendered as their own labelled sections
// below the inline scalar fields, rather than as inline detail lines. Every other
// field flows into the inline detail straight from the record.
var detailSections = map[string]bool{"provider_env": true, "models": true}

// pathFields are the detail fields whose value is a filesystem path, styled with
// tui.Path in the text view. Colour lives here, not in the record text, so table
// cells and --fields output stay plain.
var pathFields = map[string]bool{"bin": true, "config_dir": true, "config_local_dir": true, "skills_dir": true}

// renderAgentDetailFields writes the Agent heading and the inline scalar fields
// of the given agent record. Fields are driven from the record in its declared
// order, so a field added or renamed on the record reaches this view without a
// second list to maintain; section fields (detailSections) are skipped for their
// own rendering by the caller. found is shown only under verbose (a plain get
// detail renders only a found agent, so it is implied); under verbose the
// resolved config and skills paths are annotated with on-disk existence.
func renderAgentDetailFields(w io.Writer, r *record, agent *agentdex.Agent, verbose bool) {
	fs, _ := r.resolve(nil)
	detail := make([]field, 0, len(fs))
	for _, f := range fs {
		if detailSections[f.key] {
			continue
		}
		if f.key == "found" && !verbose {
			continue
		}
		if pathFields[f.key] && f.text != "-" && f.text != "missing" {
			f.text = tui.Path.Sprint(f.text)
		}
		if f.key == "homepage" && f.text != "-" {
			f.text = tui.URL.Sprint(f.text)
		}
		// The bin line always states presence, mirroring provider env's (set)/(unset):
		// a found agent shows the path with "(found)", a not-installed one already
		// reads "missing" from the record.
		if f.key == "bin" {
			if agent.Found {
				f.text += " " + styledState("found", true)
			} else {
				f.text = tui.Warn.Sprint(f.text)
			}
		}
		if verbose {
			if note := existenceNote(f.key, agent); note != "" {
				f.text += " " + styledState(note, note == "exists")
			}
		}
		detail = append(detail, f)
	}
	// A leading blank line sets the first heading off from the shell prompt,
	// matching every heading-topped text surface.
	fmt.Fprintln(w)
	fmt.Fprintln(w, tui.Header.Sprint("Agent"))
	renderDetail(w, detail)
}

// renderAgentDetail writes the full text detail view: the inline fields of the
// report record, then the provider_env and models sections when populated.
func renderAgentDetail(w io.Writer, agent *agentdex.Agent, verbose bool) {
	renderAgentDetailFields(w, agentReportRecord(agent), agent, verbose)

	if agent.ProviderEnv != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, tui.Header.Sprint("Provider env"))
		fmt.Fprintln(w, "  "+styledProviderEnv(agent.ProviderEnv))
	}
	if len(agent.Models) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, tui.Header.Sprint("Models"))
		recs := make([]*record, len(agent.Models))
		for i, m := range agent.Models {
			recs[i] = modelRecord(m, "", "")
		}
		_, headers, rows, _ := tabulate(recs, nil, modelFieldSet.defaults, modelFieldSet)
		renderTable(w, headers, rows, "  (none)")
		renderPriceFooter(w, modelFieldSet.defaults)
	}
}

// existenceNote reports the on-disk existence of a resolved path field for the
// verbose detail view, or "" for a field that names no directory or carries no
// path (so an absent-concept "-" is not annotated as a missing directory).
func existenceNote(key string, agent *agentdex.Agent) string {
	var path string
	var exists bool
	switch key {
	case "config_dir":
		path, exists = agent.Config.Global, agent.Config.GlobalExists
	case "config_local_dir":
		path, exists = agent.Config.Local, agent.Config.LocalExists
	case "skills_dir":
		path, exists = agent.Skills.Global, agent.Skills.GlobalExists
	default:
		return ""
	}
	if path == "" {
		return ""
	}
	if exists {
		return "exists"
	}
	return "missing"
}

// jsonRecords resolves records to their full present-field JSON maps, for nesting
// under a parent payload (the provider-fallthrough models list).
func jsonRecords(recs []*record) []map[string]any {
	out := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		fs, _ := r.resolve(nil)
		out = append(out, jsonObject(fs))
	}
	return out
}
