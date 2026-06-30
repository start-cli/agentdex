package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/modelsdev"
)

// agentFieldSet is the declared field authority for a detected agent: every valid
// --fields key in canonical order, and the subset shown as default list columns.
// It governs --fields validation and the get text-detail ordering, so both
// surfaces stay in step when a field is added or renamed.
var agentFieldSet = newFieldSet(
	[]string{"id", "name", "version", "bin", "found", "config", "config_local", "skills", "providers", "homepage", "provider_env", "models"},
	[]string{"id", "name", "version", "providers"},
)

// agentVerboseFields are the list table columns under --verbose: the default
// columns widened with the binary path and the global config dir.
var agentVerboseFields = []string{"id", "name", "version", "bin", "config", "providers"}

// agentRecord builds the field values for one detected agent. Optional fields that
// are absent (no local config, no skills concept, no enrichment) are simply not
// added; they remain valid to select per agentFieldSet and resolve to a blank.
func agentRecord(a *agentdex.Agent) *record {
	r := newRecord(agentFieldSet)
	r.add("id", a.ID, a.ID)
	r.add("name", a.Name, a.Name)
	r.add("version", a.Version, orDash(a.Version))
	r.add("bin", a.BinaryPath, orDash(a.BinaryPath))
	r.add("found", a.Found, fmt.Sprintf("%t", a.Found))
	r.add("config", a.Config.Global, orDash(a.Config.Global))
	// config_local and skills are added here, in their declared position, so the add
	// order matches agentFieldSet.all and the text detail view renders in that order.
	if a.Config.Local != "" {
		r.add("config_local", a.Config.Local, a.Config.Local)
	}
	if a.Skills.Global != "" {
		r.add("skills", a.Skills.Global, a.Skills.Global)
	}
	r.add("providers", a.Providers, strings.Join(a.Providers, ", "))
	r.add("homepage", a.Homepage, orDash(a.Homepage))
	return r
}

// withProviderEnv adds the provider-env field to an agent record. Provider-env is
// shown whenever a client was consulted, including under --no-models.
func withProviderEnv(r *record, env map[string]bool) {
	if env == nil {
		return
	}
	r.add("provider_env", env, formatProviderEnv(env))
}

// withModels adds the enriched models field: the typed list for JSON and a count
// for the table cell. The detailed model listing in get's text view is rendered
// separately from this summary.
func withModels(r *record, models []modelsdev.Model) {
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
// "VAR (set|unset)" entries in sorted key order.
func formatProviderEnv(env map[string]bool) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		state := "unset"
		if env[k] {
			state = "set"
		}
		parts[i] = fmt.Sprintf("%s (%s)", k, state)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
