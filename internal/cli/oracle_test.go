package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// This file hardens the CLI's observable-behaviour oracle before the library API
// redesign. Each test pins a behaviour the existing suite exercises but does not
// assert, so a rewrite that regresses it fails here rather than passing silently.
// Every assertion is written against the current code and must pass on the
// unmodified repository; anything that needs the new surface belongs to a later
// step, not this file.

// rawEnvelope decodes stdout as a generic JSON object so a test can assert which
// envelope keys are present, distinguishing an absent (omitempty) key from an
// empty value in a way the typed envelope struct cannot.
func rawEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("decode raw envelope from %q: %v", stdout, err)
	}
	return m
}

// staleScenario stands up a scenario whose catalog re-resolves on every load (a
// zero catalog TTL) and then takes the registry offline, so the next catalog
// load falls back to the last resolved version and reports stale. modelsURL wires
// a reachable models.dev for the commands that need one.
func staleScenario(t *testing.T, modelsURL string, bins ...string) *scenario {
	t.Helper()
	s := newScenario(t, modelsURL, bins...)
	s.writeConfig(t, configBody(modelsURL, s.binDir, "catalog: ttl: \"0s\"\n"))
	if got := runCLI("agents", "list"); got.code != codeOK {
		t.Fatalf("warm list exit = %d; stderr=%q", got.code, got.stderr)
	}
	s.closeRegistry()
	return s
}

func TestOracleFailureEnvelopeShapes(t *testing.T) {
	// The --json failure envelope is asserted today only on usage (2) and not-found
	// (3). Pin its shape on config (78), transient (75), and permission (4) too:
	// status "error", a non-empty error, and the omitempty behaviour of the data and
	// warnings keys.
	assertErrorEnvelope := func(t *testing.T, r result, wantCode int) {
		t.Helper()
		if r.code != wantCode {
			t.Fatalf("exit = %d, want %d; stderr=%q", r.code, wantCode, r.stderr)
		}
		m := rawEnvelope(t, r.stdout)
		if m["status"] != "error" {
			t.Errorf("status = %v, want error", m["status"])
		}
		if msg, ok := m["error"].(string); !ok || msg == "" {
			t.Errorf("error = %v, want a non-empty message", m["error"])
		}
		if _, ok := m["data"]; ok {
			t.Errorf("failure envelope should omit data: %v", m["data"])
		}
		if _, ok := m["warnings"]; ok {
			t.Errorf("failure envelope with no warnings should omit the warnings key: %v", m["warnings"])
		}
	}

	t.Run("config", func(t *testing.T) {
		s := newScenario(t, "", "alpha-cli")
		s.writeConfig(t, `color: "not-a-mode"`)
		assertErrorEnvelope(t, runCLI("--json", "agents", "list"), codeConfig)
	})

	t.Run("transient", func(t *testing.T) {
		newScenario(t, closedModelsServer(t))
		assertErrorEnvelope(t, runCLI("--json", "models", "list"), codeTransient)
	})

	t.Run("permission", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses file permissions")
		}
		s := newScenario(t, "", "alpha-cli")
		cfgPath := filepath.Join(s.configDir, "config.cue")
		if err := os.Chmod(cfgPath, 0o000); err != nil {
			t.Fatalf("chmod config unreadable: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(cfgPath, 0o644) })
		assertErrorEnvelope(t, runCLI("--json", "agents", "list"), codePermission)
	})

	t.Run("success omits error key", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		newScenario(t, srv.URL)
		got := runCLI("--json", "providers", "list")
		if got.code != codeOK {
			t.Fatalf("providers list exit = %d, stderr=%q", got.code, got.stderr)
		}
		m := rawEnvelope(t, got.stdout)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
		if _, ok := m["error"]; ok {
			t.Errorf("success envelope should omit the error key: %v", m["error"])
		}
		if _, ok := m["warnings"]; ok {
			t.Errorf("success envelope with no warnings should omit the warnings key: %v", m["warnings"])
		}
	})
}

func TestOracleWarningsSurviveFailureUnderJSON(t *testing.T) {
	// A warning accumulated before a failure must still reach the JSON envelope. With
	// a stale catalog, each failing command carries the stale-catalog warning
	// alongside its error — the behaviour the warnings-on-error rule exists to keep.
	t.Run("agents get unknown id", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		staleScenario(t, srv.URL, "alpha-cli")
		got := runCLI("--json", "agents", "get", "no-such-thing")
		if got.code != codeNotFound {
			t.Fatalf("exit = %d, want 3; stderr=%q", got.code, got.stderr)
		}
		env := got.envelope(t)
		if env.Status != "error" || env.Error == "" {
			t.Errorf("envelope = %+v, want error with a message", env)
		}
		if !anyContains(env.Warnings, "stale") {
			t.Errorf("stale warning missing from failure envelope: %v", env.Warnings)
		}
	})

	t.Run("agents list unknown provider", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		staleScenario(t, srv.URL, "alpha-cli")
		got := runCLI("--json", "agents", "list", "--provider", "bogus")
		if got.code != codeUsage {
			t.Fatalf("exit = %d, want 2; stderr=%q", got.code, got.stderr)
		}
		env := got.envelope(t)
		if env.Status != "error" || env.Error == "" {
			t.Errorf("envelope = %+v, want error with a message", env)
		}
		if !anyContains(env.Warnings, "stale") {
			t.Errorf("stale warning missing from failure envelope: %v", env.Warnings)
		}
	})

	t.Run("models list agnostic agent", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		staleScenario(t, srv.URL, "delta-agent")
		got := runCLI("--json", "models", "list", "--agent", "delta-agent")
		if got.code != codeUsage {
			t.Fatalf("exit = %d, want 2; stderr=%q", got.code, got.stderr)
		}
		env := got.envelope(t)
		if env.Status != "error" || env.Error == "" {
			t.Errorf("envelope = %+v, want error with a message", env)
		}
		if !anyContains(env.Warnings, "stale") {
			t.Errorf("stale warning missing from failure envelope: %v", env.Warnings)
		}
	})
}

// warningsOf returns the warning slice a --json run carries in its envelope,
// decoding it whether the run succeeded or failed.
func warningsOf(t *testing.T, r result) []string {
	t.Helper()
	return r.envelope(t).Warnings
}

// hasExact reports whether ss carries s verbatim.
func hasExact(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestOracleWarningMessagesVerbatim(t *testing.T) {
	// Warnings are asserted only by substring today, so a reworded message passes.
	// Pin each by full-string equality so the verbatim wording is enforced.
	t.Run("stale catalog", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		staleScenario(t, srv.URL, "alpha-cli")
		got := runCLI("--json", "agents", "list")
		if !hasExact(warningsOf(t, got), "agentdex catalog is stale: re-resolution failed, using the last resolved version") {
			t.Errorf("stale warning = %v", got.envelope(t).Warnings)
		}
	})

	t.Run("list models unreachable", func(t *testing.T) {
		newScenario(t, closedModelsServer(t), "alpha-cli")
		got := runCLI("--json", "agents", "list")
		if !hasExact(warningsOf(t, got), "model counts unavailable: models.dev is unreachable and not cached") {
			t.Errorf("list degrade warning = %v", got.envelope(t).Warnings)
		}
	})

	t.Run("agent detail models unreachable", func(t *testing.T) {
		newScenario(t, closedModelsServer(t), "alpha-cli")
		got := runCLI("--json", "agents", "get", "alpha-cli")
		if !hasExact(warningsOf(t, got), "models.dev is unreachable and not cached: model enrichment and provider-env omitted") {
			t.Errorf("get degrade warning = %v", got.envelope(t).Warnings)
		}
	})

	t.Run("list schema drift omission", func(t *testing.T) {
		srv := modelsServer(t, nil, "anthropic")
		newScenario(t, srv.URL, "alpha-cli")
		got := runCLI("--json", "agents", "list", "--installed")
		want := `model counts omitted: provider "anthropic" model "claude-sonnet" malformed: models.dev schema unrecognised`
		if !hasExact(warningsOf(t, got), want) {
			t.Errorf("schema-drift omission warning = %v", got.envelope(t).Warnings)
		}
	})

	t.Run("some providers absent", func(t *testing.T) {
		srv := modelsServer(t, []string{"google"})
		newScenario(t, srv.URL, "gamma-agent")
		got := runCLI("--json", "agents", "get", "gamma-agent")
		if !hasExact(warningsOf(t, got), "some providers are absent from models.dev: openai") {
			t.Errorf("some-present warning = %v", got.envelope(t).Warnings)
		}
	})

	t.Run("not installed", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		newScenario(t, srv.URL) // alpha-cli not installed
		got := runCLI("--json", "agents", "get", "alpha-cli", "--fields", "bin")
		if !hasExact(warningsOf(t, got), `agent "alpha-cli" is catalogued but not installed`) {
			t.Errorf("not-installed warning = %v", got.envelope(t).Warnings)
		}
	})

	t.Run("agnostic needs provider guidance", func(t *testing.T) {
		newScenario(t, "", "delta-agent")
		got := runCLI("--json", "agents", "get", "delta-agent")
		want := `"delta-agent" is provider-agnostic: supply --provider with models.dev provider ids to enrich providers, provider-env, and models`
		if !hasExact(warningsOf(t, got), want) {
			t.Errorf("agnostic guidance warning = %v", got.envelope(t).Warnings)
		}
	})
}

func TestOracleGetDataFaultReportsAgentAndError(t *testing.T) {
	// agents get on a coverage data fault asserts only exit 78 today. Assert that the
	// agent payload is still reported on that fault and pin the error text each emits.
	t.Run("none present", func(t *testing.T) {
		srv := modelsServer(t, []string{"google"})
		newScenario(t, srv.URL, "alpha-cli") // alpha uses anthropic, absent upstream
		got := runCLI("--json", "agents", "get", "alpha-cli")
		if got.code != codeConfig {
			t.Fatalf("exit = %d, want 78; stderr=%q", got.code, got.stderr)
		}
		env := got.envelope(t)
		if env.Error != `catalog data error: no provider of "alpha-cli" is present in models.dev (providers: anthropic)` {
			t.Errorf("error = %q", env.Error)
		}
		data, ok := env.Data.(map[string]any)
		if !ok || data["id"] != "alpha-cli" {
			t.Errorf("data = %v, want the alpha-cli agent payload", env.Data)
		}
	})

	t.Run("schema drift", func(t *testing.T) {
		srv := modelsServer(t, []string{"google"}, "anthropic")
		newScenario(t, srv.URL, "alpha-cli")
		got := runCLI("--json", "agents", "get", "alpha-cli")
		if got.code != codeConfig {
			t.Fatalf("exit = %d, want 78; stderr=%q", got.code, got.stderr)
		}
		env := got.envelope(t)
		if env.Error != `provider "anthropic" model "claude-sonnet" malformed: models.dev schema unrecognised` {
			t.Errorf("error = %q", env.Error)
		}
		data, ok := env.Data.(map[string]any)
		if !ok || data["id"] != "alpha-cli" {
			t.Errorf("data = %v, want the alpha-cli agent payload", env.Data)
		}
	})
}

// modelsCell returns the trailing MODELS-column cell for the row whose first
// field is id, from a text listing whose last column is MODELS.
func modelsCell(t *testing.T, stdout, id string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), id) {
			f := strings.Fields(line)
			return f[len(f)-1]
		}
	}
	t.Fatalf("no row for %q in:\n%s", id, stdout)
	return ""
}

func TestOracleListModelsCellDistinction(t *testing.T) {
	// The JSON null-versus-[] distinction is pinned; the text-cell distinction is
	// not. An agnostic/not-applicable agent renders the "-" cell and a degraded
	// agent renders "0", for both degrade causes.
	t.Run("agnostic renders dash", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		newScenario(t, srv.URL, "delta-agent")
		got := runCLI("agents", "list", "--fields", "id,models")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if cell := modelsCell(t, got.stdout, "delta-agent"); cell != "-" {
			t.Errorf("agnostic MODELS cell = %q, want -", cell)
		}
	})

	t.Run("unreachable degrade renders zero", func(t *testing.T) {
		newScenario(t, closedModelsServer(t), "alpha-cli")
		got := runCLI("agents", "list", "--installed", "--fields", "id,models")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if cell := modelsCell(t, got.stdout, "alpha-cli"); cell != "0" {
			t.Errorf("unreachable-degrade MODELS cell = %q, want 0", cell)
		}
	})

	t.Run("schema drift degrade renders zero", func(t *testing.T) {
		srv := modelsServer(t, nil, "anthropic")
		newScenario(t, srv.URL, "alpha-cli")
		got := runCLI("agents", "list", "--installed", "--fields", "id,models")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if cell := modelsCell(t, got.stdout, "alpha-cli"); cell != "0" {
			t.Errorf("schema-drift-degrade MODELS cell = %q, want 0", cell)
		}
	})
}

// emptyProviderModelsServer serves a valid models.dev catalog (non-empty top-level
// maps) whose "empty" provider carries no models, so a scope to it lists nothing
// without tripping schema validation.
func emptyProviderModelsServer(t *testing.T) string {
	t.Helper()
	cat := modelsdev.Catalog{
		Models: map[string]modelsdev.Model{
			"anthropic/claude-sonnet": {ID: "anthropic/claude-sonnet", Name: "Claude Sonnet", Limit: modelsdev.Limit{Context: 200000}},
		},
		Providers: map[string]modelsdev.Provider{
			"empty":     {ID: "empty", Name: "Empty", Models: map[string]modelsdev.Model{}},
			"anthropic": provider("anthropic", false),
		},
	}
	data, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("marshal empty-provider catalog: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestOracleGenuineEmptyState(t *testing.T) {
	// Only the filter-matched-nothing empty-state line is asserted today. Assert the
	// genuine-empty line (no filter) too.
	t.Run("agents", func(t *testing.T) {
		newScenario(t, "") // no binaries installed
		got := runCLI("agents", "list", "--installed")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "No agents detected.") {
			t.Errorf("empty --installed listing missing the genuine-empty line:\n%s", got.stdout)
		}
	})

	t.Run("models", func(t *testing.T) {
		newScenario(t, emptyProviderModelsServer(t))
		got := runCLI("models", "list", "--provider", "empty")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "No models available.") {
			t.Errorf("empty model scope missing the genuine-empty line:\n%s", got.stdout)
		}
	})

	// The providers genuine-empty line ("No providers.") is unreachable through a
	// valid catalog: an empty top-level providers map is gross schema drift
	// (exit 78), so a provider listing at exit 0 always carries at least one row.
	// It is left unasserted deliberately rather than forced through an invalid
	// fixture the loader would reject.
}

func TestOracleRefreshMalformedModelsIsConfig(t *testing.T) {
	// refresh against a reachable but malformed models.dev is a data fault: exit 78
	// (schema drift as config), matching the other model surfaces.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":{},"providers":{}}`))
	}))
	t.Cleanup(srv.Close)
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("refresh", "models.dev")
	if got.code != codeConfig {
		t.Fatalf("refresh malformed models exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

func TestOracleRefreshReportsTargetIdentityAndWording(t *testing.T) {
	// refresh all and the default target assert only that two caches refreshed, not
	// which. Assert the identity of the refreshed targets and the success wording, so
	// a refresh that silently drops one target fails.
	for _, args := range [][]string{{"refresh", "all"}, {"refresh"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			srv := modelsServer(t, []string{"anthropic"})
			newScenario(t, srv.URL, "alpha-cli")

			got := runCLI(append([]string{"--json"}, args...)...)
			if got.code != codeOK {
				t.Fatalf("%v exit = %d, stderr=%q", args, got.code, got.stderr)
			}
			refreshed := got.envelope(t).Data.(map[string]any)["refreshed"].([]any)
			var ids []string
			for _, r := range refreshed {
				ids = append(ids, r.(string))
			}
			if len(ids) != 2 || ids[0] != "catalog" || ids[1] != "models.dev" {
				t.Errorf("refreshed = %v, want [catalog models.dev]", ids)
			}

			text := runCLI(args...)
			for _, want := range []string{
				"Refreshed agentdex catalog (agent data)",
				"Refreshed models.dev catalog (provider and model data)",
			} {
				if !strings.Contains(text.stdout, want) {
					t.Errorf("%v text missing success line %q:\n%s", args, want, text.stdout)
				}
			}
		})
	}
}

func TestOracleGetSomePresentReportsSurvivingModels(t *testing.T) {
	// some-present asserts the warning but not the surviving data. Assert that the
	// present provider's models still populate.
	srv := modelsServer(t, []string{"google"}) // gamma uses [google, openai]; openai absent
	newScenario(t, srv.URL, "gamma-agent")

	got := runCLI("--json", "agents", "get", "gamma-agent", "--models")
	if got.code != codeOK {
		t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	if !anyContains(env.Warnings, "openai") {
		t.Errorf("expected the absent-provider warning: %v", env.Warnings)
	}
	models, ok := env.Data.(map[string]any)["models"].([]any)
	if !ok || len(models) == 0 {
		t.Fatalf("models = %v, want the present provider's models", env.Data.(map[string]any)["models"])
	}
	if models[0].(map[string]any)["id"] != "google-model" {
		t.Errorf("model id = %v, want google-model", models[0].(map[string]any)["id"])
	}
}

func TestOracleErrorMessagesVerbatim(t *testing.T) {
	// The failures the library is about to own are asserted by exit code alone today.
	// Pin each by full-string equality so the split between library text and
	// CLI-appended remedy is reproduced verbatim by the rewrite.
	cases := []struct {
		name  string
		bins  []string
		args  []string
		agent string // "" installs no fixture binaries beyond bins
		want  string
	}{
		{
			name: "provider on home-provider agent",
			bins: []string{"alpha-cli"},
			args: []string{"agents", "get", "alpha-cli", "--provider", "anthropic"},
			want: `agent "alpha-cli" has catalog providers; --provider is only valid for provider-agnostic agents`,
		},
		{
			name: "agnostic needs provider on agents get",
			bins: []string{"delta-agent"},
			args: []string{"agents", "get", "delta-agent", "--models"},
			want: `providers required for agnostic agent: "delta-agent" is provider-agnostic; supply --provider with models.dev provider ids`,
		},
		{
			name: "agnostic needs provider on models list",
			bins: []string{"delta-agent"},
			args: []string{"models", "list", "--agent", "delta-agent"},
			want: `providers required for agnostic agent: "delta-agent" is provider-agnostic; supply --provider with models.dev provider ids`,
		},
		{
			name: "unknown agent id",
			bins: []string{"alpha-cli"},
			args: []string{"agents", "get", "no-such-thing"},
			want: `no agent "no-such-thing"; run "agentdex agents list" to see agent ids`,
		},
		{
			name: "unknown provider on providers get",
			args: []string{"providers", "get", "no-such-provider"},
			want: `no models.dev provider "no-such-provider"; run "agentdex providers list" to see provider ids`,
		},
		{
			name: "malformed model composite",
			args: []string{"models", "get", "claude-sonnet"},
			want: `model id "claude-sonnet" must be provider-id/model-id; run "agentdex models list" to see model ids`,
		},
		{
			name: "composite unknown provider",
			args: []string{"models", "get", "nope/some-model"},
			want: `no model "nope/some-model": unknown provider "nope"`,
		},
		{
			name: "composite unknown model key",
			args: []string{"models", "get", "anthropic/no-such-model"},
			want: `no model "anthropic/no-such-model" in provider "anthropic"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := modelsServer(t, []string{"anthropic"})
			newScenario(t, srv.URL, tc.bins...)
			got := runCLI(append([]string{"--json"}, tc.args...)...)
			if env := got.envelope(t); env.Error != tc.want {
				t.Errorf("error = %q, want %q", env.Error, tc.want)
			}
		})
	}
}

func TestOracleModelsProviderColumnScopeBased(t *testing.T) {
	// The models table's provider column is not asserted at all. Pin the scope-based
	// rule the CLI applies today: shown for a multi-provider scope, absent for a
	// single-provider one, including the case a filter narrows a multi-provider scope
	// to one provider's rows (the case R15 later inverts to the row-based rule).
	t.Run("multi-provider scope shows column", func(t *testing.T) {
		srv := modelsServer(t, []string{"google", "openai"})
		newScenario(t, srv.URL)
		got := runCLI("models", "list", "--provider", "google,openai")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "PROVIDER") {
			t.Errorf("multi-provider listing should show the PROVIDER column:\n%s", got.stdout)
		}
	})

	t.Run("single-provider scope hides column", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		newScenario(t, srv.URL)
		got := runCLI("models", "list", "--provider", "anthropic")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if strings.Contains(got.stdout, "PROVIDER") {
			t.Errorf("single-provider listing should not show the PROVIDER column:\n%s", got.stdout)
		}
	})

	t.Run("filter narrowing multi scope keeps column", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic", "google"})
		newScenario(t, srv.URL)
		got := runCLI("models", "list", "claude", "--provider", "anthropic,google")
		if got.code != codeOK {
			t.Fatalf("exit = %d, stderr=%q", got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, "PROVIDER") {
			t.Errorf("filter-narrowed multi-provider scope should still show the PROVIDER column (scope-based rule):\n%s", got.stdout)
		}
		rows := runCLI("--json", "models", "list", "claude", "--provider", "anthropic,google").envelope(t).Data.([]any)
		if len(rows) != 1 {
			t.Errorf("filter should narrow to one provider's row, got %d", len(rows))
		}
	})
}

func TestOracleListProviderValidationInstalledAndDegrade(t *testing.T) {
	// Listing-wide provider validation is asserted only where an agnostic agent is
	// catalogued. Assert it holds with --installed narrowing the result to home
	// providers, and that an unreachable models.dev degrades rather than rejects.
	t.Run("installed unknown provider is usage", func(t *testing.T) {
		srv := modelsServer(t, []string{"anthropic"})
		newScenario(t, srv.URL, "alpha-cli")
		got := runCLI("agents", "list", "--installed", "--provider", "bogus")
		if got.code != codeUsage {
			t.Fatalf("exit = %d, want 2; stderr=%q", got.code, got.stderr)
		}
		if !strings.Contains(got.stderr, "unknown provider") {
			t.Errorf("expected unknown-provider error, got %q", got.stderr)
		}
	})

	t.Run("unreachable degrades not rejects", func(t *testing.T) {
		newScenario(t, closedModelsServer(t), "alpha-cli")
		got := runCLI("--json", "agents", "list", "--provider", "anthropic")
		if got.code != codeOK {
			t.Fatalf("exit = %d, want 0; stderr=%q", got.code, got.stderr)
		}
		if !anyContains(got.envelope(t).Warnings, "unreachable") {
			t.Errorf("expected a degrade warning, got %v", got.envelope(t).Warnings)
		}
	})
}

func TestOracleStreamDiscipline(t *testing.T) {
	// Warnings-to-stderr in text mode is spot-checked on one command today. Assert the
	// stream discipline across commands: warnings to stderr in text mode and into the
	// envelope under --json, with data never mixed onto the wrong stream.
	assert := func(t *testing.T, textArgs []string, wantWarnSub string) {
		t.Helper()
		text := runCLI(textArgs...)
		if text.code != codeOK {
			t.Fatalf("%v text exit = %d, stderr=%q", textArgs, text.code, text.stderr)
		}
		if !strings.Contains(text.stderr, "warning:") || !strings.Contains(text.stderr, wantWarnSub) {
			t.Errorf("%v text warning not on stderr: %q", textArgs, text.stderr)
		}
		if strings.Contains(text.stdout, "warning:") {
			t.Errorf("%v text should not print warnings to stdout:\n%s", textArgs, text.stdout)
		}

		js := runCLI(append([]string{"--json"}, textArgs...)...)
		if js.code != codeOK {
			t.Fatalf("%v json exit = %d, stderr=%q", textArgs, js.code, js.stderr)
		}
		if !anyContains(js.envelope(t).Warnings, wantWarnSub) {
			t.Errorf("%v json warning not in envelope: %v", textArgs, js.envelope(t).Warnings)
		}
		if strings.Contains(js.stderr, "warning:") {
			t.Errorf("%v json should carry no warning on stderr: %q", textArgs, js.stderr)
		}
	}

	t.Run("agents get agnostic guidance", func(t *testing.T) {
		newScenario(t, "", "delta-agent")
		assert(t, []string{"agents", "get", "delta-agent"}, "provider-agnostic")
	})

	t.Run("agents list degrade", func(t *testing.T) {
		newScenario(t, closedModelsServer(t), "alpha-cli")
		assert(t, []string{"agents", "list"}, "unreachable")
	})
}
