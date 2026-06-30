package cli

import (
	"strings"
	"testing"
)

func TestModelsSingleMatchCanonicalID(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "models", "alpha-cli", "sonnet")
	if got.code != codeOK {
		t.Fatalf("models query exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if data["id"] != "claude-sonnet" {
		t.Errorf("id = %v, want the short source id claude-sonnet", data["id"])
	}
	if data["canonical_id"] != "anthropic/claude-sonnet" {
		t.Errorf("canonical_id = %v, want anthropic/claude-sonnet", data["canonical_id"])
	}
}

func TestModelsCanonicalIDFieldIsBareValue(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("models", "alpha-cli", "sonnet", "--fields", "canonical_id")
	if got.code != codeOK {
		t.Fatalf("models --fields exit = %d, stderr=%q", got.code, got.stderr)
	}
	if strings.TrimSpace(got.stdout) != "anthropic/claude-sonnet" {
		t.Errorf("--fields canonical_id stdout = %q, want the bare canonical id", got.stdout)
	}
}

func TestModelsUnknownFieldRejectedOnEmptyResult(t *testing.T) {
	// --fields validation must not depend on result cardinality. alpha-cli's only
	// provider (anthropic) is absent from this models.dev, so the list is empty; an
	// unknown field is still a usage fault (exit 2), not a silent success.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("models", "alpha-cli", "--fields", "bogus")
	if got.code != codeUsage {
		t.Fatalf("models --fields bogus on empty result exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsAmbiguousQueryLists(t *testing.T) {
	// gamma-agent serves google and openai; "model" matches a model in each.
	srv := modelsServer(t, []string{"google", "openai"})
	newScenario(t, srv.URL, "gamma-agent")

	got := runCLI("models", "gamma-agent", "model")
	if got.code != codeNotFound {
		t.Fatalf("ambiguous models exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsListNoQuery(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "models", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "claude-sonnet" {
		t.Errorf("models list data = %v", rows)
	}
}

func TestModelsListJSONCarriesFullRecord(t *testing.T) {
	// models --json without --fields carries the full model record, including the
	// capability fields absent from the default table columns.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "models", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	for _, key := range []string{"provider", "reasoning", "tool_call"} {
		if _, ok := row[key]; !ok {
			t.Errorf("models --json should carry non-default field %q: %v", key, row)
		}
	}
}

func TestModelsTransientWhenUnreachable(t *testing.T) {
	newScenario(t, closedModelsServer(t), "alpha-cli")

	got := runCLI("models", "alpha-cli")
	if got.code != codeTransient {
		t.Fatalf("models unreachable exit = %d, want 75; stderr=%q", got.code, got.stderr)
	}
}

func TestModelsUnknownAgent(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("models", "no-such-agent")
	if got.code != codeNotFound {
		t.Fatalf("models unknown-agent exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
}
