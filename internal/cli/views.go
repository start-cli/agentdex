package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/tui"
	"github.com/start-cli/agentdex/modelsdev"
)

// agentFieldSet is the declared field authority for a detected agent: every valid
// --fields key in canonical order, and the subset shown as default list columns.
// It governs --fields validation and the get text-detail ordering, so both
// surfaces stay in step when a field is added or renamed.
var agentFieldSet = newFieldSet(
	[]string{"id", "name", "version", "bin", "found", "config_dir", "config_local_dir", "skills_dir", "providers", "homepage", "provider_env", "models"},
	[]string{"id", "name", "version", "providers", "models", "bin"},
)

// agentVerboseFields are the list table columns under --verbose: the default
// columns widened with the global config dir. models sits between providers and
// bin in both sets; bin stays last, the widest, most variable column, whose
// "missing" cell is the list --all detection signal.
var agentVerboseFields = []string{"id", "name", "version", "config_dir", "providers", "models", "bin"}

// agentRecord builds the field values for one detected agent. Optional fields that
// are absent (no local config, no skills concept, no enrichment) are simply not
// added; they remain valid to select per agentFieldSet and resolve to a blank.
func agentRecord(a *agentdex.Agent) *record {
	return buildAgentRecord(a, true)
}

// agentRecordWithoutProviders is the agnostic soft-path record: outside facts
// only, omitting the providers field (and never adding provider_env / models).
func agentRecordWithoutProviders(a *agentdex.Agent) *record {
	return buildAgentRecord(a, false)
}

func buildAgentRecord(a *agentdex.Agent, includeProviders bool) *record {
	r := newRecord(agentFieldSet)
	r.add("id", a.ID, a.ID)
	r.add("name", a.Name, a.Name)
	r.add("version", a.Version, orDash(a.Version))
	// A not-found agent renders "missing" in the bin cell (the list --all
	// detection signal); the JSON value stays blank with found carrying the fact.
	binText := orDash(a.BinaryPath)
	if !a.Found {
		binText = "missing"
	}
	r.add("bin", a.BinaryPath, binText)
	r.add("found", a.Found, fmt.Sprintf("%t", a.Found))
	r.add("config_dir", a.Config.Global, orDash(a.Config.Global))
	// config_local_dir and skills_dir are added here, in their declared position, so the add
	// order matches agentFieldSet.all and the text detail view renders in that order.
	if a.Config.Local != "" {
		r.add("config_local_dir", a.Config.Local, a.Config.Local)
	}
	if a.Skills.Global != "" {
		r.add("skills_dir", a.Skills.Global, a.Skills.Global)
	}
	if includeProviders {
		r.add("providers", a.Providers, strings.Join(a.Providers, ", "))
	}
	r.add("homepage", a.Homepage, orDash(a.Homepage))
	return r
}

// withModelsNA marks models as not applicable (agnostic agent without --provider):
// JSON null and text "-", distinct from withModels's nil→[] degrade shape.
func withModelsNA(r *record) {
	r.add("models", nil, "-")
}

// withProviderEnv adds the provider-env field to an agent record. Provider-env is
// shown whenever a client was consulted, independent of Models fill.
func withProviderEnv(r *record, env map[string]bool) {
	if env == nil {
		return
	}
	r.add("provider_env", env, formatProviderEnv(env))
}

// withModels adds the enriched models field: the typed list for JSON and a count
// for the table cell. The detailed model listing in get's text view is rendered
// separately from this summary. A nil list (list's degraded-enrichment case) is
// normalised to an empty slice so the JSON carries [] to match the "0" count cell,
// rather than a null that disagrees with the text and breaks `jq '.models|length'`.
func withModels(r *record, models []modelsdev.Model) {
	if models == nil {
		models = []modelsdev.Model{}
	}
	r.add("models", models, fmt.Sprintf("%d", len(models)))
}

// modelFieldSet is the declared field authority for a model: every valid --fields
// key in canonical order, and the subset shown as default models-table columns.
var modelFieldSet = newFieldSet(
	[]string{"id", "provider", "name", "family", "context", "input", "output", "reasoning", "tool_call", "attachment", "canonical_id"},
	[]string{"id", "name", "context", "input", "output"},
)

// modelRecord builds the field values for one model. canonical_id is added only
// when non-empty (the model has a real models.dev agnostic id); it remains valid
// to select per modelFieldSet, so --fields canonical_id yields a blank for a model
// without one rather than an unknown-field error.
func modelRecord(m modelsdev.Model, providerID, canonicalID string) *record {
	r := newRecord(modelFieldSet)
	r.add("id", m.ID, m.ID)
	r.add("provider", providerID, providerID)
	r.add("name", m.Name, m.Name)
	r.add("family", m.Family, orDash(m.Family))
	r.add("context", m.Limit.Context, fmt.Sprintf("%d", m.Limit.Context))
	r.add("input", costValue(m.Cost, costInput), costText(m.Cost, costInput))
	r.add("output", costValue(m.Cost, costOutput), costText(m.Cost, costOutput))
	r.add("reasoning", m.Reasoning, fmt.Sprintf("%t", m.Reasoning))
	r.add("tool_call", m.ToolCall, fmt.Sprintf("%t", m.ToolCall))
	r.add("attachment", m.Attachment, fmt.Sprintf("%t", m.Attachment))
	if canonicalID != "" {
		r.add("canonical_id", canonicalID, canonicalID)
	}
	return r
}

// newerModel reports whether a sorts before b in a newest-first model listing:
// later release_date first (ISO dates compare lexically), undated models last,
// ties broken by id so the order stays deterministic.
func newerModel(a, b modelsdev.Model) bool {
	if a.ReleaseDate != b.ReleaseDate {
		if a.ReleaseDate == "" {
			return false
		}
		if b.ReleaseDate == "" {
			return true
		}
		return a.ReleaseDate > b.ReleaseDate
	}
	return a.ID < b.ID
}

// sortModelsNewest orders models newest release first for display. This is a
// presentation choice of the CLI: the library keeps its stable by-id order.
func sortModelsNewest(models []modelsdev.Model) {
	sort.SliceStable(models, func(i, j int) bool { return newerModel(models[i], models[j]) })
}

type costKind int

const (
	costInput costKind = iota
	costOutput
)

// costFor returns the per-1M-token price for the given kind and whether pricing is
// known. A nil Cost means models.dev carried no pricing for the model.
func costFor(c *modelsdev.Cost, kind costKind) (float64, bool) {
	if c == nil {
		return 0, false
	}
	if kind == costOutput {
		return c.Output, true
	}
	return c.Input, true
}

// costValue returns the per-1M-token price for JSON, or nil when pricing is
// unknown so the field is null rather than a misleading zero.
func costValue(c *modelsdev.Cost, kind costKind) any {
	v, ok := costFor(c, kind)
	if !ok {
		return nil
	}
	return v
}

// costText renders the per-1M-token price at full precision with trailing zeros
// trimmed ("$3", "$0.075", "$0.0001"), so cheap sub-cent prices stay truthful
// rather than rounding to a misleading "$0.00".
func costText(c *modelsdev.Cost, kind costKind) string {
	v, ok := costFor(c, kind)
	if !ok {
		return "-"
	}
	return "$" + strconv.FormatFloat(v, 'f', -1, 64)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatProviderEnv renders the provider-env map deterministically as
// "VAR (set|unset)" entries in sorted key order. It stays plain so record text,
// table cells, and --fields output carry no colour codes.
func formatProviderEnv(env map[string]bool) string {
	return providerEnvText(env, plainState)
}

// styledProviderEnv is formatProviderEnv with the state markers coloured for the
// detail section: (set) green, (unset) yellow.
func styledProviderEnv(env map[string]bool) string {
	return providerEnvText(env, styledState)
}

func providerEnvText(env map[string]bool, state func(string, bool) string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		text, good := "unset", false
		if env[k] {
			text, good = "set", true
		}
		parts[i] = k + " " + state(text, good)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func plainState(state string, _ bool) string {
	return "(" + state + ")"
}

// styledState renders a bracketed state marker per the start terminal colour
// standard: cyan delimiters, with the state text green when positive and yellow
// when negative.
func styledState(state string, good bool) string {
	inner := tui.Warn
	if good {
		inner = tui.Good
	}
	return tui.Delim.Sprint("(") + inner.Sprint(state) + tui.Delim.Sprint(")")
}
