package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/config"
	"github.com/start-cli/agentdex/internal/match"
	"github.com/start-cli/agentdex/internal/tui"
	"github.com/start-cli/agentdex/modelsdev"
)

func (a *app) newGetCmd() *cobra.Command {
	var (
		noModels bool
		models   bool
		tree     bool
		fields   []string
	)
	cmd := &cobra.Command{
		Use:     "get <agent>",
		Aliases: []string{"view", "show"},
		GroupID: groupCore,
		Short:   "Show detail for one agent",
		Long: "Show detection detail for one agent: its binary, version, config and skills " +
			"paths, provider-env presence, and the models its providers offer. Model enrichment " +
			"is on by default (configurable); --no-models opts out while keeping provider-env.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.requireConfig()
			if err != nil {
				return a.failConfig(cmd, err)
			}
			if models && noModels {
				return a.usage(cmd, errors.New("--models and --no-models are mutually exclusive"))
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

			outcome, id, candidates := matchAgent(cat, args[0])
			switch outcome {
			case match.Ambiguous:
				return a.fail(cmd, codeNotFound, fmt.Errorf("ambiguous agent %q: matches %s", args[0], strings.Join(candidates, ", ")), warnings...)
			case match.None:
				return a.getFallthrough(cmd, cfg.ModelsClient(), cat, args[0], warnings)
			}
			a.log.Debug("get resolved agent", "id", id)

			detectOpts := append(cfg.LibraryOptions(flags), agentdex.WithCatalog(cat))
			agent, found, err := agentdex.DetectOne(cmd.Context(), id, detectOpts...)
			if err != nil {
				return a.fail(cmd, codeFor(err), err, warnings...)
			}
			if tree {
				return a.getTree(cmd, agent, warnings)
			}
			if !found {
				// Render the catalogued detail — resolved config paths, providers,
				// homepage, bin reading "missing" — before the not-installed error,
				// so a miss still reports everything the catalog knows.
				err := fmt.Errorf("agent %q (%s) is catalogued but not installed", id, agent.Name)
				return a.reportAgentError(cmd, agent, fields, codeNotFound, err, warnings)
			}

			enrich := cfg.EnrichModels
			if noModels {
				enrich = false
			} else if models {
				enrich = true
			}
			return a.getCoverage(cmd, cfg, flags, cat, agent, enrich, fields, warnings)
		},
	}
	cmd.Flags().BoolVar(&models, "models", false, "Force per-model enrichment on")
	cmd.Flags().BoolVar(&noModels, "no-models", false, "Skip per-model enrichment (provider-env still shows)")
	cmd.Flags().BoolVar(&tree, "tree", false, "Print the config directory tree instead of detail")
	cmd.Flags().StringSliceVar(&fields, "fields", nil, "Select output fields (csv)")
	return cmd
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
// against models.dev's providers. A unique provider match reports that provider's
// data at exit 3; no provider match is genuinely unknown at exit 2. If models.dev
// cannot be reached the query cannot be classified at all, so it exits transient
// rather than asserting either verdict.
func (a *app) getFallthrough(cmd *cobra.Command, client *modelsdev.Client, cat *agentdex.Catalog, query string, warnings []string) error {
	outcome, prov, _, err := matchProvider(cmd.Context(), client, query)
	if err != nil {
		return a.fail(cmd, codeTransient, fmt.Errorf("cannot classify %q: models.dev is unreachable: %w", query, err), warnings...)
	}
	if outcome != match.Unique {
		return a.fail(cmd, codeUsage, fmt.Errorf("no such agent %q; valid ids: %s", query, strings.Join(catalogIDs(cat), ", ")), warnings...)
	}
	a.log.Debug("get fallthrough matched provider", "query", query, "provider", prov.ID)

	models := make([]modelsdev.Model, 0, len(prov.Models))
	for _, key := range sortedKeys(prov.Models) {
		models = append(models, prov.Models[key])
	}
	sortModelsNewest(models)
	recs := make([]*record, len(models))
	for i, m := range models {
		recs[i] = modelRecord(m, prov.ID, "")
	}
	data := map[string]any{
		"provider": prov.ID,
		"name":     prov.Name,
		"models":   jsonRecords(recs),
	}
	_, headers, rows, _ := tabulate(recs, nil, modelFieldSet.defaults, modelFieldSet)
	note := fmt.Errorf("%q is not a catalogued agent; showing models.dev provider %q (install details unavailable, not catalogued)", query, prov.ID)
	return a.failData(cmd, codeNotFound, note, data, func(w io.Writer) {
		fmt.Fprintln(w)
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

// detailSections are the record fields rendered as their own labelled sections
// below the inline scalar fields, rather than as inline detail lines. Every other
// field flows into the inline detail straight from the record.
var detailSections = map[string]bool{"provider_env": true, "models": true}

// pathFields are the detail fields whose value is a filesystem path, styled with
// tui.Path in the text view. Colour lives here, not in the record text, so table
// cells and --fields output stay plain.
var pathFields = map[string]bool{"bin": true, "config": true, "config_local": true, "skills": true}

// renderAgentDetail writes the full text detail view. The inline scalar fields are
// driven from the agent record in its declared order, so a field added or renamed
// on the record reaches this view without a second list to maintain; provider_env
// and models carry their own section rendering below. found is shown only under
// verbose (a plain get detail renders only a found agent, so it is implied); under
// verbose the resolved config and skills paths are annotated with on-disk existence.
func renderAgentDetail(w io.Writer, agent *agentdex.Agent, verbose bool) {
	fs, _ := agentReportRecord(agent).resolve(nil)
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
	case "config":
		path, exists = agent.Config.Global, agent.Config.GlobalExists
	case "config_local":
		path, exists = agent.Config.Local, agent.Config.LocalExists
	case "skills":
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

// getTree prints the agent's config directory tree without parsing contents.
func (a *app) getTree(cmd *cobra.Command, agent *agentdex.Agent, warnings []string) error {
	root := agent.Config.Global
	entries, err := walkTree(root)
	if err != nil {
		return a.fail(cmd, codeFailure, fmt.Errorf("walk config tree: %w", err), warnings...)
	}
	data := map[string]any{"agent": agent.ID, "config": root, "entries": entries}
	return a.ok(cmd, data, warnings, func(w io.Writer) {
		fmt.Fprintln(w)
		fmt.Fprintln(w, root)
		if len(entries) == 0 {
			fmt.Fprintln(w, tui.Muted.Sprint("  (empty or not present)"))
			return
		}
		for _, e := range entries {
			fmt.Fprintf(w, "  %s\n", e)
		}
	})
}

// walkTree returns the relative paths under root in walk order. A missing root is
// not an error: it yields no entries, matching a not-yet-created config dir.
func walkTree(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if path == root {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			rel += string(os.PathSeparator)
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
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
