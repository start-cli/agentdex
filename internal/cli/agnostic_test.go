package cli

import (
	"strings"
	"testing"
)

// delta-agent is the fixture's provider-agnostic agent: agnostic:true, no catalog
// provider list. The caller supplies the models.dev provider set via --provider.

func TestGetAgnosticSoftPathOmitsProviderFields(t *testing.T) {
	// Unfiltered get on an agnostic agent without --provider reports outside facts
	// only, omits providers / provider_env / models, and warns how to enrich.
	newScenario(t, "", "delta-agent")

	got := runCLI("--json", "agents", "get", "delta-agent")
	if got.code != codeOK {
		t.Fatalf("get exit = %d, stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	data := env.Data.(map[string]any)
	for _, key := range []string{"providers", "provider_env", "models"} {
		if _, ok := data[key]; ok {
			t.Errorf("agnostic soft-path get should omit %q: %v", key, data)
		}
	}
	if !anyContains(env.Warnings, "provider-agnostic") {
		t.Errorf("expected a provider-agnostic warning, got %v", env.Warnings)
	}
}

func TestGetAgnosticModelsWithoutProviderIsUsage(t *testing.T) {
	// Demanding models from an agnostic agent without --provider is a usage fault.
	newScenario(t, "", "delta-agent")

	got := runCLI("agents", "get", "delta-agent", "--models")
	if got.code != codeUsage {
		t.Fatalf("get --models exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "provider-agnostic") {
		t.Errorf("expected provider-agnostic error, got %q", got.stderr)
	}
}

func TestGetAgnosticEnrichesWithProvider(t *testing.T) {
	// With --provider the agnostic agent enriches against the caller-supplied set.
	newScenario(t, "", "delta-agent")

	got := runCLI("--json", "agents", "get", "delta-agent", "--models", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("get --models --provider exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	provs, ok := data["providers"].([]any)
	if !ok || len(provs) != 1 || provs[0] != "anthropic" {
		t.Errorf("providers = %v, want [anthropic]", data["providers"])
	}
	if models, ok := data["models"].([]any); !ok || len(models) == 0 {
		t.Errorf("models missing or empty with --provider: %v", data["models"])
	}
}

func TestGetAgnosticUnknownProviderIsUsage(t *testing.T) {
	newScenario(t, "", "delta-agent")

	got := runCLI("agents", "get", "delta-agent", "--models", "--provider", "bogus")
	if got.code != codeUsage {
		t.Fatalf("get --provider bogus exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("expected unknown-provider error, got %q", got.stderr)
	}
}

func TestGetProviderRejectedOnHomeProviderAgent(t *testing.T) {
	// --provider is only meaningful for agnostic agents; a home-provider agent
	// rejects it as a usage error rather than silently ignoring it.
	newScenario(t, "", "alpha-cli")

	got := runCLI("agents", "get", "alpha-cli", "--provider", "anthropic")
	if got.code != codeUsage {
		t.Fatalf("get --provider on home-provider agent exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
}

func TestGetProviderNameQueryIsNotFound(t *testing.T) {
	// A query matching no catalogued agent is an exact miss (exit 3) whether or not
	// --provider is supplied: the fallthrough that once reclassified it as a
	// models.dev provider is gone.
	newScenario(t, "")

	got := runCLI("agents", "get", "anthropic", "--provider", "openai")
	if got.code != codeNotFound {
		t.Fatalf("provider-name query exit = %d, want %d; stderr=%q", got.code, codeNotFound, got.stderr)
	}
}

func TestModelsAgnosticAgentWithoutProviderIsUsage(t *testing.T) {
	newScenario(t, "", "delta-agent")

	got := runCLI("models", "list", "--agent", "delta-agent")
	if got.code != codeUsage {
		t.Fatalf("models list --agent exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "provider-agnostic") {
		t.Errorf("expected provider-agnostic error, got %q", got.stderr)
	}
}

func TestModelsAgnosticAgentWithProviderLists(t *testing.T) {
	newScenario(t, "", "delta-agent")

	got := runCLI("--json", "models", "list", "--agent", "delta-agent", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("models list --agent --provider exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) == 0 {
		t.Errorf("models list --agent delta-agent --provider anthropic listed nothing: %s", got.stdout)
	}
}

func TestModelsAgnosticAgentUnknownProviderIsUsage(t *testing.T) {
	// The agnostic-enrichment role of --provider is validated too: an unknown id is
	// a usage fault, not a silent empty listing.
	newScenario(t, "", "delta-agent")

	got := runCLI("models", "list", "--agent", "delta-agent", "--provider", "bogus")
	if got.code != codeUsage {
		t.Fatalf("models list --agent --provider bogus exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("expected unknown-provider error, got %q", got.stderr)
	}
}

func TestModelsProviderRejectedOnHomeProviderAgent(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("models", "list", "--agent", "alpha-cli", "--provider", "anthropic")
	if got.code != codeUsage {
		t.Fatalf("models list --agent home-provider --provider exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
}

func TestGetAgnosticSoftPathNotInstalled(t *testing.T) {
	// Not-installed outranks soft path: exit 3 with the not-installed error, but
	// the payload keeps the soft-path shape — outside facts, the three provider
	// fields omitted, and the agnostic warning.
	newScenario(t, "") // delta binary not installed

	got := runCLI("--json", "agents", "get", "delta-agent")
	if got.code != codeNotFound {
		t.Fatalf("get exit = %d, want %d; stderr=%q", got.code, codeNotFound, got.stderr)
	}
	env := got.envelope(t)
	if !strings.Contains(env.Error, "not installed") {
		t.Errorf("envelope error = %q, want not-installed", env.Error)
	}
	if !anyContains(env.Warnings, "provider-agnostic") {
		t.Errorf("expected a provider-agnostic warning, got %v", env.Warnings)
	}
	data := env.Data.(map[string]any)
	for _, key := range []string{"providers", "provider_env", "models"} {
		if _, ok := data[key]; ok {
			t.Errorf("not-installed soft-path get should omit %q: %v", key, data)
		}
	}
	if data["found"] != false {
		t.Errorf("found = %v, want false", data["found"])
	}
}

func TestGetAgnosticProviderNotInstalled(t *testing.T) {
	// With --provider and not Found: exit 3, providers carries the caller ids,
	// provider_env and models stay omitted (no models.dev client until Found),
	// and no soft-path warning — the caller already supplied providers.
	newScenario(t, "") // delta binary not installed

	got := runCLI("--json", "agents", "get", "delta-agent", "--provider", "anthropic")
	if got.code != codeNotFound {
		t.Fatalf("get --provider exit = %d, want %d; stderr=%q", got.code, codeNotFound, got.stderr)
	}
	env := got.envelope(t)
	data := env.Data.(map[string]any)
	provs, ok := data["providers"].([]any)
	if !ok || len(provs) != 1 || provs[0] != "anthropic" {
		t.Errorf("providers = %v, want [anthropic]", data["providers"])
	}
	for _, key := range []string{"provider_env", "models"} {
		if _, ok := data[key]; ok {
			t.Errorf("not-installed get --provider should omit %q: %v", key, data)
		}
	}
	if anyContains(env.Warnings, "provider-agnostic") {
		t.Errorf("soft-path warning should not fire with --provider: %v", env.Warnings)
	}
}

func TestGetAgnosticBareProviderKeepsProviderEnvOmitsModels(t *testing.T) {
	// Bare --provider without Models demand: provider-env is filled (a client is
	// attached), Models stays omitted per the OR rule.
	newScenario(t, "", "delta-agent")

	got := runCLI("--json", "agents", "get", "delta-agent", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("get --provider exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["provider_env"]; !ok {
		t.Errorf("provider_env missing from bare --provider get: %v", data)
	}
	if _, ok := data["models"]; ok {
		t.Errorf("bare --provider get should omit models: %v", data["models"])
	}
}

func TestGetAgnosticNonProviderFieldsStayOffline(t *testing.T) {
	// A --fields selection with no provider-related field is answered from the
	// catalog alone: no models.dev fetch, no agnostic warning, exit 0.
	newScenario(t, mustNotFetchModelsServer(t), "delta-agent")

	got := runCLI("--json", "agents", "get", "delta-agent", "--fields", "skills_dir")
	if got.code != codeOK {
		t.Fatalf("get --fields skills_dir exit = %d, stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	if len(env.Warnings) != 0 {
		t.Errorf("non-provider field selection should carry no warnings: %v", env.Warnings)
	}
	data := env.Data.(map[string]any)
	if _, ok := data["skills_dir"]; !ok {
		t.Errorf("expected skills_dir in selection: %v", data)
	}
}

func TestGetAgnosticFieldsProvidersValidatesCallerIds(t *testing.T) {
	// providers is caller input on an agnostic agent, not catalog truth: selecting
	// it with --provider validates the ids rather than echoing them at exit 0.
	newScenario(t, "", "delta-agent")

	got := runCLI("agents", "get", "delta-agent", "--fields", "providers", "--provider", "bogus")
	if got.code != codeUsage {
		t.Fatalf("get --fields providers --provider bogus exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("expected unknown-provider error, got %q", got.stderr)
	}

	valid := runCLI("--json", "agents", "get", "delta-agent", "--fields", "providers", "--provider", "anthropic")
	if valid.code != codeOK {
		t.Fatalf("get --fields providers --provider anthropic exit = %d, stderr=%q", valid.code, valid.stderr)
	}
	data := valid.envelope(t).Data.(map[string]any)
	provs, ok := data["providers"].([]any)
	if !ok || len(provs) != 1 || provs[0] != "anthropic" {
		t.Errorf("providers = %v, want [anthropic]", data["providers"])
	}
}

func TestGetAgnosticNonProviderFieldsWithProviderStaysOffline(t *testing.T) {
	// A selection with no provider-related field stays offline even when
	// --provider is supplied: nothing caller-provided is reported, so nothing
	// needs validating.
	newScenario(t, mustNotFetchModelsServer(t), "delta-agent")

	got := runCLI("--json", "agents", "get", "delta-agent", "--fields", "skills_dir", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("get --fields skills_dir --provider exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["skills_dir"]; !ok {
		t.Errorf("expected skills_dir in selection: %v", data)
	}
}

func TestModelsListDuplicateProviderDeduplicates(t *testing.T) {
	// A repeated --provider id is deduplicated by flattenProviders, so the scoped
	// listing carries each model once rather than twice.
	newScenario(t, "", "delta-agent")

	list := runCLI("--json", "models", "list", "--agent", "delta-agent", "--provider", "anthropic,anthropic")
	if list.code != codeOK {
		t.Fatalf("models list with duplicate --provider exit = %d, stderr=%q", list.code, list.stderr)
	}
	if rows := list.envelope(t).Data.([]any); len(rows) != 1 {
		t.Errorf("models rows = %d, want 1 (deduplicated)", len(rows))
	}
}

func TestGetAgnosticProviderDegradesWithWarningWhenUnreachable(t *testing.T) {
	// An unreachable-and-uncached models.dev degrades the --provider enrichment
	// exactly like the home-provider path: exit 0, provider_env and models
	// omitted, and a warning so the silence reads as an outage.
	newScenario(t, closedModelsServer(t), "delta-agent")

	got := runCLI("--json", "agents", "get", "delta-agent", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("get --provider degrade exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	if !anyContains(env.Warnings, "models.dev") {
		t.Errorf("degrade should warn about models.dev: %v", env.Warnings)
	}
	data := env.Data.(map[string]any)
	for _, key := range []string{"provider_env", "models"} {
		if _, ok := data[key]; ok {
			t.Errorf("degraded get --provider should omit %q: %v", key, data)
		}
	}
}

func TestListAgnosticProviderShowsCount(t *testing.T) {
	// With --provider, an agnostic row matches the home-provider shape: a real
	// model array in JSON, a count in text, not the null/- marker.
	newScenario(t, "", "delta-agent")

	got := runCLI("--json", "agents", "list", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("list --provider exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 1 {
		t.Fatalf("list rows = %d, want 1", len(rows))
	}
	row := rows[0].(map[string]any)
	models, ok := row["models"].([]any)
	if !ok || len(models) == 0 {
		t.Errorf("agnostic models with --provider = %v, want a non-empty array", row["models"])
	}
}

func TestListAgnosticUnknownProviderIsUsage(t *testing.T) {
	// An unknown caller-supplied id fails the listing as a usage fault, unlike the
	// missing-provider case, which soft-skips enrichment and keeps listing.
	newScenario(t, "", "delta-agent")

	got := runCLI("agents", "list", "--provider", "bogus")
	if got.code != codeUsage {
		t.Fatalf("list --provider bogus exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("expected unknown-provider error, got %q", got.stderr)
	}
}

func TestListUnknownProviderIsUsageWithoutAgnosticInstalled(t *testing.T) {
	// --provider is validated at the boundary: an unknown id fails as usage even
	// when no agnostic agent is installed to enrich against it, so the outcome does
	// not depend on which binaries happen to be present.
	newScenario(t, "", "alpha-cli") // only a home-provider agent installed; delta absent

	got := runCLI("agents", "list", "--provider", "bogus")
	if got.code != codeUsage {
		t.Fatalf("list --provider bogus exit = %d, want %d; stderr=%q", got.code, codeUsage, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("expected unknown-provider error, got %q", got.stderr)
	}
}
