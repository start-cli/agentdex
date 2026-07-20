package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// slashKeyModelsServer serves a models.dev catalog whose one provider carries a
// model whose key contains slashes, so a test can prove the composite splits on the
// first slash only. The agnostic map carries the full composite so canonical_id
// resolves.
func slashKeyModelsServer(t *testing.T) string {
	t.Helper()
	const pid, key = "mixlayer", "qwen/qwen3.5-122b"
	composite := pid + "/" + key
	model := modelsdev.Model{ID: key, Name: "Qwen 3.5", Limit: modelsdev.Limit{Context: 128000}}
	cat := modelsdev.Catalog{
		Models: map[string]modelsdev.Model{composite: {ID: composite, Name: "Qwen 3.5", Limit: modelsdev.Limit{Context: 128000}}},
		Providers: map[string]modelsdev.Provider{
			pid: {ID: pid, Name: "Mixlayer", Env: []string{"MIXLAYER_API_KEY"}, Models: map[string]modelsdev.Model{key: model}},
		},
	}
	data, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("marshal slash-key catalog: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestModelsGetSplitsOnFirstSlashOnly(t *testing.T) {
	// The composite splits on the first slash: everything after it is the model key,
	// slashes and all. mixlayer/qwen/qwen3.5-122b is provider "mixlayer", key
	// "qwen/qwen3.5-122b" — not provider "mixlayer/qwen" or a truncated key.
	newScenario(t, slashKeyModelsServer(t))

	got := runCLI("--json", "models", "get", "mixlayer/qwen/qwen3.5-122b")
	if got.code != codeOK {
		t.Fatalf("slash-key models get exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if data["provider"] != "mixlayer" {
		t.Errorf("provider = %v, want mixlayer (split on the first slash only)", data["provider"])
	}
	if data["id"] != "qwen/qwen3.5-122b" {
		t.Errorf("id = %v, want the slash-bearing model key qwen/qwen3.5-122b", data["id"])
	}
	if data["canonical_id"] != "mixlayer/qwen/qwen3.5-122b" {
		t.Errorf("canonical_id = %v, want the full composite", data["canonical_id"])
	}
}

func TestModelsGetCompositeHit(t *testing.T) {
	// models get takes the composite provider-id/model-id and fetches it exactly:
	// the short source id stays the id field, the composite is the canonical id.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "models", "get", "anthropic/claude-sonnet")
	if got.code != codeOK {
		t.Fatalf("models get exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if data["id"] != "claude-sonnet" {
		t.Errorf("id = %v, want the short source id claude-sonnet", data["id"])
	}
	if data["provider"] != "anthropic" {
		t.Errorf("provider = %v, want anthropic", data["provider"])
	}
	if data["canonical_id"] != "anthropic/claude-sonnet" {
		t.Errorf("canonical_id = %v, want anthropic/claude-sonnet", data["canonical_id"])
	}
}

func TestModelsGetCanonicalIDFieldIsBareValue(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "get", "anthropic/claude-sonnet", "--fields", "canonical_id")
	if got.code != codeOK {
		t.Fatalf("models get --fields exit = %d, stderr=%q", got.code, got.stderr)
	}
	if strings.TrimSpace(got.stdout) != "anthropic/claude-sonnet" {
		t.Errorf("--fields canonical_id stdout = %q, want the bare canonical id", got.stdout)
	}
}

func TestModelsGetMissingSlashIsUsage(t *testing.T) {
	// A value with no slash is not a composite id: usage error pointing at the
	// browse verb.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "get", "claude-sonnet")
	if got.code != codeUsage {
		t.Fatalf("no-slash models get exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "models list") {
		t.Errorf("no-slash error should point at models list: %q", got.stderr)
	}
}

func TestModelsGetUnknownProviderIsNotFound(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "get", "nope/some-model")
	if got.code != codeNotFound {
		t.Fatalf("unknown-provider composite exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsGetUnknownModelKeyIsNotFound(t *testing.T) {
	// The provider exists but carries no such model key: still exact-miss not-found.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "get", "anthropic/no-such-model")
	if got.code != codeNotFound {
		t.Fatalf("unknown-key composite exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsGetPriceFooter(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "get", "anthropic/claude-sonnet")
	if got.code != codeOK {
		t.Fatalf("models get exit = %d, stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, priceUnitNote) {
		t.Errorf("models get detail missing the price footer:\n%s", got.stdout)
	}
}

func TestModelsListGlobalAcrossProviders(t *testing.T) {
	// With no scope models list spans every provider models.dev knows.
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "models", "list")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 3 {
		t.Fatalf("global models list rows = %d, want 3 (one per provider)", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.(map[string]any)["provider"].(string)] = true
	}
	for _, pid := range []string{"anthropic", "google", "openai"} {
		if !seen[pid] {
			t.Errorf("global models list missing provider %q: %v", pid, seen)
		}
	}
}

func TestModelsListProviderScope(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "models", "list", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("models list --provider exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "claude-sonnet" {
		t.Errorf("--provider anthropic rows = %v, want just claude-sonnet", rows)
	}
}

func TestModelsListFilterComposesWithScope(t *testing.T) {
	// The positional filter narrows over model id and name and composes with the
	// provider scope. Under --provider anthropic, "claude" matches, "gemini" does not.
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL)

	hit := runCLI("--json", "models", "list", "claude", "--provider", "anthropic")
	if hit.code != codeOK {
		t.Fatalf("models list filter+scope exit = %d, stderr=%q", hit.code, hit.stderr)
	}
	if rows := hit.envelope(t).Data.([]any); len(rows) != 1 {
		t.Errorf("filter claude under anthropic rows = %d, want 1", len(rows))
	}

	miss := runCLI("--json", "models", "list", "gemini", "--provider", "anthropic")
	if miss.code != codeOK {
		t.Fatalf("models list filter miss exit = %d, want 0; stderr=%q", miss.code, miss.stderr)
	}
	if rows := miss.envelope(t).Data.([]any); len(rows) != 0 {
		t.Errorf("filter gemini under anthropic rows = %d, want 0", len(rows))
	}

	text := runCLI("models", "list", "gemini", "--provider", "anthropic")
	if !strings.Contains(text.stdout, `No models match "gemini".`) {
		t.Errorf("no-match text output missing filter-aware empty-state line:\n%s", text.stdout)
	}
}

func TestModelsListAgentScope(t *testing.T) {
	// --agent scopes to a home-provider agent's catalog providers.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "models", "list", "--agent", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("models list --agent exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "claude-sonnet" {
		t.Errorf("--agent alpha-cli rows = %v, want just claude-sonnet", rows)
	}
}

func TestModelsListUnknownAgentIsNotFound(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("models", "list", "--agent", "no-such-agent")
	if got.code != codeNotFound {
		t.Fatalf("models list --agent unknown exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsListUnknownProviderIsUsageDirectScope(t *testing.T) {
	// An unknown --provider id is a usage fault in the standalone direct-scope role,
	// validated against a reachable models.dev, never a silent empty listing.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "list", "--provider", "bogus")
	if got.code != codeUsage {
		t.Fatalf("models list --provider bogus exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("expected unknown-provider error, got %q", got.stderr)
	}
}

func TestModelsListJSONCarriesFullRecord(t *testing.T) {
	// models list --json without --fields carries the full model record, including
	// the capability fields absent from the default table columns.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "models", "list", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	for _, key := range []string{"provider", "reasoning", "tool_call"} {
		if _, ok := row[key]; !ok {
			t.Errorf("models list --json should carry non-default field %q: %v", key, row)
		}
	}
}

func TestModelsListNewestFirst(t *testing.T) {
	// openai's fixture model is newer than google's, so it lists first even though
	// google-model sorts first by id. JSON follows the same order.
	srv := modelsServer(t, []string{"google", "openai"})
	newScenario(t, srv.URL)

	got := runCLI("models", "list", "--provider", "google,openai")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	if strings.Index(got.stdout, "openai-model") > strings.Index(got.stdout, "google-model") {
		t.Errorf("models list should order newest release first:\n%s", got.stdout)
	}

	js := runCLI("--json", "models", "list", "--provider", "google,openai")
	rows := js.envelope(t).Data.([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["id"] != "openai-model" {
		t.Errorf("models --json order = %v, want openai-model first", rows)
	}
}

func TestModelsListPriceFooter(t *testing.T) {
	// A table showing price columns carries the unit footer; a --fields selection
	// without a price column stays footer-free (it is the scripting surface).
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "list", "--provider", "anthropic")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, priceUnitNote) {
		t.Errorf("models list missing the price footer:\n%s", got.stdout)
	}

	noPrices := runCLI("models", "list", "--provider", "anthropic", "--fields", "id,name")
	if strings.Contains(noPrices.stdout, priceUnitNote) {
		t.Errorf("--fields without price columns should omit the footer:\n%s", noPrices.stdout)
	}
}

func TestModelsListUnknownFieldRejectedOnEmptyResult(t *testing.T) {
	// --fields validation must not depend on result cardinality. A filter matching
	// no model still rejects an unknown --fields key as a usage fault, not exit 0.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("models", "list", "no-such-model", "--provider", "anthropic", "--fields", "bogus")
	if got.code != codeUsage {
		t.Fatalf("models list --fields bogus on empty result exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsListTransientWhenUnreachable(t *testing.T) {
	newScenario(t, closedModelsServer(t))

	got := runCLI("models", "list")
	if got.code != codeTransient {
		t.Fatalf("models list unreachable exit = %d, want 75; stderr=%q", got.code, got.stderr)
	}
}
