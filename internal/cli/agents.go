package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/tui"
)

// newAgentsCmd is the agents noun group: a browse verb (list) and an exact fetch
// verb (get). The group itself is not runnable; a bare invocation is a usage fault.
func (a *app) newAgentsCmd() *cobra.Command {
	cmd := a.newNounCmd(
		"agents", "agent", "AI coding agents in the catalog and their local detection",
		a.newAgentsListCmd(),
		a.newAgentsGetCmd(),
	)
	// --search-dir and --bin-path steer agent binary resolution, so they belong to
	// the agents group rather than the root, where they would surface on the provider
	// and model commands that resolve no binary.
	f := cmd.PersistentFlags()
	// StringArray, not StringSlice: a directory path can legally contain a comma, so
	// values are taken literally rather than csv-split, matching --bin-path.
	f.StringArrayVar(&a.searchDirs, "search-dir", nil, "Extra binary search locations (repeatable)")
	f.StringArrayVar(&a.binPaths, "bin-path", nil, "Override an agent's binary path as id=path (repeatable)")
	return cmd
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
			idx, err := a.index(cmd)
			if err != nil {
				return err
			}
			filter := ""
			if len(args) == 1 {
				filter = args[0]
			}
			// The listing always requests EnrichFull: its JSON payload carries each
			// agent's full model array while the text column renders that array's
			// length, so a lower level would change the models key's shape (R15). The
			// library owns detection, enrichment, the agnostic/home rules, boundary
			// provider validation, degrade classification, and the by-id order (R8, R14).
			res, err := idx.Agents.List(cmd.Context(), agentdex.AgentQuery{
				Filter:    filter,
				Installed: installed,
				Providers: flattenProviders(providers),
				Enrich:    agentdex.EnrichFull,
			})
			warnings := libWarnings(res.Warnings)
			if err != nil {
				return a.fail(cmd, codeFor(err), err, warnings...)
			}
			a.log.Debug("list agents", "count", len(res.Items), "installed", installed, "filter", filter)

			recs := make([]*record, len(res.Items))
			for i := range res.Items {
				ag := &res.Items[i]
				r := agentRecord(ag)
				if ag.Enrichment == agentdex.EnrichNotApplicable {
					// Not applicable: JSON null / text "-", not the degrade [] / 0 shape.
					withModelsNA(r)
				} else {
					withModels(r, ag.Models)
				}
				recs[i] = r
			}
			sortKey, err := applyOrder(recs, agentFieldSet, orderBy, reverse)
			if err != nil {
				return a.usage(cmd, err)
			}
			if orderBy == "" {
				// The default view groups detected agents ahead of the not-found tail;
				// the stable sort keeps the id ordering within each group. An explicit
				// --order-by is a pure field sort with no such grouping (R14).
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
			fallback := "No agents catalogued."
			if installed {
				fallback = "No agents detected."
			}
			empty := emptyListMessage(filter, "agents", fallback)
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
			idx, err := a.index(cmd)
			if err != nil {
				return err
			}
			id := args[0]

			// Map the requested output to the lowest enrichment level that can fill it,
			// so a field selection never pays for data it does not show (R15). The
			// library owns the agnostic/home rules, coverage, not-installed status, and
			// the warnings; the CLI only translates its own flags into a level and maps
			// the returned facts to exit codes and remedies.
			detail, err := idx.Agents.Get(cmd.Context(), id, agentdex.AgentGetQuery{
				Providers: flattenProviders(providers),
				Enrich:    agentGetLevel(models, fields),
			})
			warnings := libWarnings(detail.Warnings)
			if err != nil {
				return a.fail(cmd, codeFor(err), agentGetError(err, id), warnings...)
			}

			// An agnostic agent resolved without a provider set is not-applicable. An
			// unfiltered detail is a browse: emit the guidance warning and exit 0. A
			// --fields or --models selection that names one of the unfillable fields is
			// an explicit request the CLI cannot honour, so it is the usage fault (R15).
			if detail.Enrichment == agentdex.EnrichNotApplicable {
				if namedProviderField(models, fields) {
					uerr := fmt.Errorf("%w: %q is provider-agnostic; supply --provider with models.dev provider ids", agentdex.ErrProvidersRequired, id)
					return a.fail(cmd, codeUsage, uerr, warnings...)
				}
				return a.reportSoftPathAgent(cmd, &detail.Agent, warnings)
			}

			// Coverage data faults report the agent and then exit 78: the caller maps the
			// verdict to policy, the library never fails on it (R5, R15). The none-present
			// message is rebuilt from the resolved provider set; the drift message is the
			// models.dev decode failure carried in Coverage.Err.
			switch detail.Coverage.Status {
			case agentdex.CoverageNonePresent:
				cerr := fmt.Errorf("catalog data error: no provider of %q is present in models.dev (providers: %s)", id, strings.Join(detail.Providers, ", "))
				return a.reportAgentError(cmd, &detail.Agent, fields, codeConfig, cerr, warnings)
			case agentdex.CoverageSchemaDrift:
				return a.reportAgentError(cmd, &detail.Agent, fields, codeConfig, detail.Coverage.Err, warnings)
			}
			return a.reportAgent(cmd, &detail.Agent, fields, warnings)
		},
	}
	cmd.Flags().BoolVar(&models, "models", false, "Fill the per-model list from models.dev")
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "models.dev provider ids for agnostic agents (repeatable or csv)")
	registerFieldsFlag(cmd, &fields)
	addFieldsHelpSection(cmd, agentFieldSet)
	return cmd
}

// agentGetLevel maps the requested output to the lowest enrichment level that fills
// it (R15): --models or a selected models field needs the full model list; an
// unfiltered detail or a selected provider_env needs the count level (provider-env
// and coverage); providers alone is offline catalog data; anything else is offline
// facts only.
func agentGetLevel(models bool, fields []string) agentdex.Enrich {
	switch {
	case models || containsField(fields, "models"):
		return agentdex.EnrichFull
	case len(fields) == 0 || containsField(fields, "provider_env"):
		return agentdex.EnrichCount
	case containsField(fields, "providers"):
		return agentdex.EnrichProviders
	default:
		return agentdex.EnrichNone
	}
}

// namedProviderField reports whether the requested output explicitly names a field
// the not-applicable state leaves empty — providers, provider_env, or models, or the
// --models flag. An unfiltered detail names none, so it is a browse (R15).
func namedProviderField(models bool, fields []string) bool {
	return models || containsField(fields, "providers") ||
		containsField(fields, "provider_env") || containsField(fields, "models")
}

// containsField reports whether fields names key.
func containsField(fields []string, key string) bool {
	for _, f := range fields {
		if f == key {
			return true
		}
	}
	return false
}

// agentGetError appends the CLI's own remedy clause to the get faults the library
// owns, naming a subcommand or flag only the CLI has (R7). The exit code is taken
// from the underlying sentinel before this wrapping.
func agentGetError(err error, id string) error {
	switch {
	case errors.Is(err, agentdex.ErrAgentUnknown):
		return errors.New(err.Error() + "; run \"agentdex agents list\" to see agent ids")
	case errors.Is(err, agentdex.ErrProvidersNotAllowed):
		return errors.New(err.Error() + "; --provider is only valid for provider-agnostic agents")
	default:
		return err
	}
}

// reportAgent renders an agent at exit 0: the JSON record under --json, selected
// fields for scripting under --fields, otherwise the full detail view.
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

// reportSoftPathAgent renders the not-applicable (agnostic, no provider set) payload
// at exit 0: the without-providers record, so the three provider-related keys are
// absent.
func (a *app) reportSoftPathAgent(cmd *cobra.Command, agent *agentdex.Agent, warnings []string) error {
	r := agentRecordWithoutProviders(agent)
	fs, _ := r.resolve(nil)
	return a.ok(cmd, jsonObject(fs), warnings, func(w io.Writer) {
		renderAgentDetailFields(w, r, agent, a.verbose)
	})
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
			if agent.Detection.Found {
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
	d := agent.Detection
	var path string
	var exists bool
	switch key {
	case "config_dir":
		path, exists = d.Config.Global, d.Config.GlobalExists
	case "config_local_dir":
		path, exists = d.Config.Local, d.Config.LocalExists
	case "skills_dir":
		path, exists = d.Skills.Global, d.Skills.GlobalExists
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
