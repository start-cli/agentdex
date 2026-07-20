package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/config"
	"github.com/start-cli/agentdex/internal/tui"
	"github.com/start-cli/agentdex/modelsdev"
)

// newAgentsCmd is the agents noun group: a browse verb (list) and an exact fetch
// verb (get). The group itself is not runnable; a bare invocation is a usage fault.
func (a *app) newAgentsCmd() *cobra.Command {
	return a.newNounCmd(
		"agents", "agent", "AI coding agents in the catalog and their local detection",
		a.newAgentsListCmd(),
		a.newAgentsGetCmd(),
	)
}

func (a *app) newAgentsListCmd() *cobra.Command {
	var (
		installed bool
		fields    []string
		providers []string
		orderBy   string
		reverse   bool
	)
	cmd := &cobra.Command{
		Use:   "list [filter]",
		Short: "List AI coding agents",
		Long: "List the AI coding agents in the catalog with their local detection status: a " +
			"detected agent shows its resolved binary in the BIN column, while an agent whose " +
			"binary was not found on PATH shows \"missing\". --installed narrows the listing to the " +
			"agents detected on this machine. Each agent's model count is enriched from models.dev, " +
			"served from the local cache when warm and degrading to zero when models.dev cannot be " +
			"reached. Provider-agnostic agents show \"-\" unless --provider is given. Detected agents " +
			"lead and the rows are ordered by id by default; --order-by sorts by any field and " +
			"--reverse flips the direction. An optional filter narrows the list to agents whose id or " +
			"name contains it (case-insensitive); a filter matching nothing prints an empty listing " +
			"and exits 0.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			flags, err := a.mapFlags()
			if err != nil {
				return a.usage(cmd, err)
			}
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}

			cat, stale, err := agentdex.LoadCatalog(cmd.Context(), cfg.CatalogOptions(cfg.CatalogTTL)...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err)
			}
			var warnings []string
			if stale {
				warnings = append(warnings, staleCatalogWarning)
			}

			callerProviders := flattenProviders(providers)
			base := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat))
			if len(callerProviders) > 0 {
				base = append(base, agentdex.WithProviders(callerProviders...))
			}
			if !installed {
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
			if filter != "" {
				agents = filterAgents(agents, filter)
			}
			a.log.Debug("list agents", "count", len(agents), "installed", installed, "filter", filter)

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
			sortKey, err := applyOrder(recs, agentFieldSet, orderBy, reverse)
			if err != nil {
				return a.usage(cmd, err)
			}
			if orderBy == "" {
				// The default view groups detected agents ahead of the not-found tail;
				// the stable sort keeps the id ordering within each group.
				// An explicit --order-by is a pure field sort with no such grouping.
				sort.SliceStable(recs, func(i, j int) bool { return recordFound(recs[i]) && !recordFound(recs[j]) })
			}

			// Compose the table columns: --verbose widens them, then the sort column is
			// pulled leftmost so the ordering is legible. This is a text-table affordance
			// only; the JSON payload always carries the full record (driven by the user's
			// --fields selection), so it is unaffected. An explicit --fields wins over both.
			tableCols := fields
			if len(tableCols) == 0 {
				base := agentFieldSet.defaults
				if a.verbose {
					base = agentVerboseFields
				}
				tableCols = orderColumns(base, sortKey)
			}

			data, headers, rows, err := tabulate(recs, fields, tableCols, agentFieldSet)
			if err != nil {
				return a.usage(cmd, err)
			}
			empty := "No agents catalogued."
			if installed {
				empty = "No agents detected."
			}
			return a.ok(cmd, data, warnings, func(w io.Writer) {
				fmt.Fprintln(w)
				renderTable(w, headers, rows, empty)
			})
		},
	}
	cmd.Flags().BoolVar(&installed, "installed", false, "Limit to agents detected on this machine")
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "models.dev provider ids for agnostic agents' model counts (repeatable or csv)")
	registerFieldsFlag(cmd, &fields)
	registerOrderFlags(cmd, &orderBy, &reverse)
	addFieldsHelpSection(cmd, agentFieldSet)
	return cmd
}

// recordFound reports whether an agent record's found field is set, for the default
// list grouping that leads with detected agents.
func recordFound(r *record) bool {
	found, _ := r.value("found").(bool)
	return found
}

// filterAgents narrows detected agents to those whose id or name contains the
// browse filter (case-insensitive), applied after detection so enrichment and the
// --installed/--provider/--verbose behaviour are unchanged by the filter.
func filterAgents(agents []agentdex.Agent, filter string) []agentdex.Agent {
	needle := strings.ToLower(filter)
	out := make([]agentdex.Agent, 0, len(agents))
	for _, ag := range agents {
		if matchesFilter(ag.ID, ag.Name, needle) {
			out = append(out, ag)
		}
	}
	return out
}

func (a *app) newAgentsGetCmd() *cobra.Command {
	var (
		models    bool
		fields    []string
		providers []string
	)
	cmd := &cobra.Command{
		Use:     "get <id>",
		Aliases: []string{"view", "show"},
		Short:   "Show detail for one agent",
		Long: "Show detection detail for one agent, selected exactly by its catalog id: its " +
			"binary, version, config and skills paths, and provider-env presence. Models are " +
			"off by default; pass --models or include models in --fields to fill the per-model " +
			"list. Provider-agnostic agents omit provider fields until --provider is supplied. " +
			"An id that names no catalogued agent is not-found (exit 3).",
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
				warnings = append(warnings, staleCatalogWarning)
			}

			// Exact fetch: the id is the catalog key, no fuzzy resolution and no
			// provider fallthrough. A miss is not-found with no candidate list.
			id := args[0]
			ka, ok := cat.Agents[id]
			if !ok {
				return a.fail(cmd, codeNotFound, fmt.Errorf("no agent %q; run \"agentdex agents list\" to see agent ids", id), warnings...)
			}
			a.log.Debug("get resolved agent", "id", id)

			callerProviders := flattenProviders(providers)
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
