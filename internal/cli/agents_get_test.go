package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/start-cli/agentdex"
)

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestGetAllPresent(t *testing.T) {
	// Unfiltered get keeps provider-env and omits Models (opt-in only).
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("get exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["provider_env"]; !ok {
		t.Errorf("provider_env missing from all-present get: %v", data)
	}
	if _, ok := data["models"]; ok {
		t.Errorf("unfiltered get should omit models: %v", data["models"])
	}

	// Text surface: Models section absent; Provider env present. Match whole
	// lines only — temp paths can embed the test name substring "Models".
	text := runCLI("agents", "get", "alpha-cli")
	if text.code != codeOK {
		t.Fatalf("get text exit = %d, stderr=%q", text.code, text.stderr)
	}
	if hasTextSection(text.stdout, "Models") {
		t.Errorf("bare get text should omit Models section:\n%s", text.stdout)
	}
	if !hasTextSection(text.stdout, "Provider env") {
		t.Errorf("bare get text should keep Provider env section:\n%s", text.stdout)
	}
}

func TestGetModelsOptIn(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli", "--models")
	if got.code != codeOK {
		t.Fatalf("get --models exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["provider_env"]; !ok {
		t.Errorf("provider_env missing from --models get: %v", data)
	}
	if models, ok := data["models"].([]any); !ok || len(models) == 0 {
		t.Errorf("models missing or empty with --models: %v", data["models"])
	}

	text := runCLI("agents", "get", "alpha-cli", "--models")
	if text.code != codeOK {
		t.Fatalf("get --models text exit = %d, stderr=%q", text.code, text.stderr)
	}
	if !hasTextSection(text.stdout, "Models") {
		t.Errorf("get --models text should show Models section:\n%s", text.stdout)
	}
}

func TestGetFieldsModelsDemandsFill(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli", "--fields", "models")
	if got.code != codeOK {
		t.Fatalf("get --fields models exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if models, ok := data["models"].([]any); !ok || len(models) == 0 {
		t.Errorf("--fields models should fill models: %v", data["models"])
	}
}

func TestGetFieldsOmitModelsKey(t *testing.T) {
	// Presentation only: field selection drops models from the record either
	// way. Demand skip for non-models fields is TestModelsDemand ("fields
	// other") and unfiltered omit is TestGetAllPresent.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli", "--fields", "skills_dir")
	if got.code != codeOK {
		t.Fatalf("get --fields skills_dir exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["models"]; ok {
		t.Errorf("output should omit models when not selected: %v", data)
	}
	if _, ok := data["skills_dir"]; !ok {
		t.Errorf("expected skills_dir in selection: %v", data)
	}
}

func TestGetModelsFlagFieldsOmitPresentation(t *testing.T) {
	// Demand that --models still fills when fields omit models is covered by
	// TestModelsDemand ("flag and omit fields"). This integration test only
	// checks presentation: selected skills_dir, models key absent from output.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli", "--models", "--fields", "skills_dir")
	if got.code != codeOK {
		t.Fatalf("get --models --fields skills_dir exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["models"]; ok {
		t.Errorf("output should omit models when not selected: %v", data)
	}
	if _, ok := data["skills_dir"]; !ok {
		t.Errorf("expected skills_dir in selection: %v", data)
	}
}

// hasTextSection reports whether stdout contains a whole-line section header
// equal to title. Substring search is unsafe: t.TempDir paths include the test
// name and can embed words like "Models".
func hasTextSection(stdout, title string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == title {
			return true
		}
	}
	return false
}

func TestGetNoModelsFlagRejected(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "get", "alpha-cli", "--no-models")
	if got.code != codeUsage {
		t.Fatalf("--no-models exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestGetSomePresentWarns(t *testing.T) {
	// gamma-agent uses [google, openai]; only google is present upstream.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "gamma-agent")

	got := runCLI("--json", "agents", "get", "gamma-agent")
	if got.code != codeOK {
		t.Fatalf("some-present exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if !anyContains(got.envelope(t).Warnings, "openai") {
		t.Errorf("expected a warning naming the absent provider openai: %v", got.envelope(t).Warnings)
	}
}

func TestGetNonePresentIsDataError(t *testing.T) {
	// alpha-cli uses anthropic, which is absent from this models.dev.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "get", "alpha-cli")
	if got.code != codeConfig {
		t.Fatalf("none-present exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

// mustNotFetchModelsServer returns a models.dev URL whose any access fails the
// test: proof that a code path is answered without touching models.dev.
func mustNotFetchModelsServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("models.dev was fetched; this path must stay offline")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGetNonModelsFieldsSkipModelsDevAndRollup(t *testing.T) {
	// The second demand gate: a --fields selection that demands neither
	// provider_env nor models is answered offline — no models.dev fetch, no
	// coverage rollup, exit 0 even when every catalog provider would be absent
	// (which an unfiltered get reports as exit 78).
	newScenario(t, mustNotFetchModelsServer(t), "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli", "--fields", "skills_dir")
	if got.code != codeOK {
		t.Fatalf("get --fields skills_dir exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["skills_dir"]; !ok {
		t.Errorf("expected skills_dir in selection: %v", data)
	}
}

func TestGetFieldsProvidersIsOfflineCatalogData(t *testing.T) {
	// providers alone is catalog data: filled with no models.dev fetch and no
	// rollup, so absent-upstream providers cannot turn it into exit 78.
	newScenario(t, mustNotFetchModelsServer(t), "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli", "--fields", "providers")
	if got.code != codeOK {
		t.Fatalf("get --fields providers exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	provs, ok := data["providers"].([]any)
	if !ok || len(provs) != 1 || provs[0] != "anthropic" {
		t.Errorf("providers = %v, want the catalog list [anthropic]", data["providers"])
	}
}

func TestGetSchemaIsDataError(t *testing.T) {
	srv := modelsServer(t, []string{"google"}, "anthropic")
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "get", "alpha-cli")
	if got.code != codeConfig {
		t.Fatalf("schema-drift exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

func TestGetTopLevelSchemaIsDataErrorNotOutage(t *testing.T) {
	// A reachable models.dev whose whole document fails validation (empty maps) is a
	// data fault (exit 78), not an outage: the rollup must not degrade it to exit 0
	// with a misleading "unreachable" warning.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":{},"providers":{}}`))
	}))
	t.Cleanup(srv.Close)
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "get", "alpha-cli")
	if got.code != codeConfig {
		t.Fatalf("top-level schema drift exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

func TestGetNotInstalled(t *testing.T) {
	// A catalogued agent is found in the catalog whether or not its binary is
	// installed, so get is a success (exit 0): it renders everything the catalog
	// knows with a "missing" bin, and warns that the agent is not installed
	// rather than failing. Under --json the envelope carries data with the
	// warning and no error.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL) // no binaries installed

	got := runCLI("agents", "get", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("not-installed exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	for _, want := range []string{"alpha-cli", "missing", ".alpha"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("not-installed detail missing %q:\n%s", want, got.stdout)
		}
	}
	if !strings.Contains(got.stderr, "not installed") {
		t.Errorf("not-installed warning missing from stderr: %q", got.stderr)
	}

	js := runCLI("--json", "agents", "get", "alpha-cli")
	if js.code != codeOK {
		t.Fatalf("not-installed --json exit = %d, want 0", js.code)
	}
	env := js.envelope(t)
	if env.Status != "ok" || env.Error != "" {
		t.Errorf("envelope status/error = %q/%q, want ok with no error", env.Status, env.Error)
	}
	if !anyContains(env.Warnings, "not installed") {
		t.Errorf("envelope warnings = %v, want one naming not installed", env.Warnings)
	}
	data, ok := env.Data.(map[string]any)
	if !ok || data["found"] != false || data["bin"] != "" {
		t.Errorf("envelope data = %v, want found=false with blank bin", env.Data)
	}
}

func TestGetNotInstalledStillEnriches(t *testing.T) {
	// Enrichment no longer depends on installation (R4). --models on a not-installed
	// agent fills the same model list an installed one fills, and the not-installed
	// warning is the bare status with no omission suffix (R6, exception one). A purely
	// offline field selection still warns not-installed and carries no omission note.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL) // no binaries installed

	enrich := runCLI("--json", "agents", "get", "alpha-cli", "--models")
	if enrich.code != codeOK {
		t.Fatalf("--models not-installed exit = %d, want 0; stderr=%q", enrich.code, enrich.stderr)
	}
	env := enrich.envelope(t)
	if !hasExact(env.Warnings, `agent "alpha-cli" is catalogued but not installed`) {
		t.Errorf("--models not-installed warnings = %v, want the bare not-installed status", env.Warnings)
	}
	for _, w := range env.Warnings {
		if strings.Contains(w, "omitted") {
			t.Errorf("not-installed warning must no longer carry the omission suffix: %q", w)
		}
	}
	data := env.Data.(map[string]any)
	if models, ok := data["models"].([]any); !ok || len(models) == 0 {
		t.Errorf("--models on a not-installed agent should fill the model list, got %v", data["models"])
	}

	offline := runCLI("--json", "agents", "get", "alpha-cli", "--fields", "bin")
	if offline.code != codeOK {
		t.Fatalf("--fields bin not-installed exit = %d, want 0; stderr=%q", offline.code, offline.stderr)
	}
	for _, w := range offline.envelope(t).Warnings {
		if strings.Contains(w, "omitted") {
			t.Errorf("offline --fields selection should not warn of enrichment omission: %q", w)
		}
	}
}

func TestGetUnknownIDIsNotFound(t *testing.T) {
	// An id that names no catalogued agent is exact-miss not-found (exit 3), with no
	// candidate list. The get verb never fuzzy-resolves.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "get", "no-such-thing")
	if got.code != codeNotFound {
		t.Fatalf("unknown id exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
	// No candidate list: the error must not enumerate the catalog ids.
	if strings.Contains(got.stderr, "alpha-cli") {
		t.Errorf("get miss should not list candidates: %q", got.stderr)
	}
}

func TestGetProviderQueryIsNotFoundNoFallthrough(t *testing.T) {
	// "google" is a models.dev provider but not a catalogued agent. With the
	// fallthrough removed, get treats it as a plain exact miss: exit 3 not-found,
	// no provider payload reclassified onto the agent surface. Provider discovery
	// now lives in providers get.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "agents", "get", "google")
	if got.code != codeNotFound {
		t.Fatalf("provider-name query exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	if env.Status != "error" {
		t.Errorf("status = %q, want error", env.Status)
	}
	if env.Data != nil {
		t.Errorf("provider-name query should carry no data payload: %v", env.Data)
	}
}

func TestGetDegradesWhenModelsUnreachable(t *testing.T) {
	newScenario(t, closedModelsServer(t), "alpha-cli")

	got := runCLI("--json", "agents", "get", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("degrade exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	if !anyContains(env.Warnings, "models.dev") {
		t.Errorf("degrade should warn about models.dev: %v", env.Warnings)
	}
	data := env.Data.(map[string]any)
	if _, ok := data["models"]; ok {
		t.Errorf("degrade should omit models: %v", data)
	}
}

func TestGetProbesVersionOnce(t *testing.T) {
	// A successful get must exec the agent's version probe exactly once: the
	// enriched detection skips the exec and carries the version from the first.
	srv := modelsServer(t, []string{"anthropic"})
	s := newScenario(t, srv.URL)
	counter := filepath.Join(s.home, "probe-count")
	installCountingBin(t, s.binDir, "alpha-cli", counter)

	got := runCLI("agents", "get", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("get exit = %d, stderr=%q", got.code, got.stderr)
	}
	if n := probeCount(t, counter); n != 1 {
		t.Errorf("version probe ran %d times, want 1", n)
	}
	if !strings.Contains(got.stdout, "1.0.0") {
		t.Errorf("get detail missing the probed version:\n%s", got.stdout)
	}
}

func TestGetVerboseAddsDetail(t *testing.T) {
	// --verbose surfaces the found field and annotates resolved paths with on-disk
	// existence; plain get shows neither. --json is unaffected.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	plain := runCLI("agents", "get", "alpha-cli")
	if plain.code != codeOK {
		t.Fatalf("get exit = %d, stderr=%q", plain.code, plain.stderr)
	}
	// The found field key sits at the start of its detail line; the bin line's
	// "(found)" presence annotation is a different, always-on surface.
	if strings.Contains(plain.stdout, "\nfound") {
		t.Errorf("plain get should not show the found field:\n%s", plain.stdout)
	}
	if !strings.Contains(plain.stdout, "(found)") {
		t.Errorf("plain get should annotate the bin line with (found):\n%s", plain.stdout)
	}

	verbose := runCLI("agents", "get", "alpha-cli", "--verbose")
	if verbose.code != codeOK {
		t.Fatalf("get --verbose exit = %d, stderr=%q", verbose.code, verbose.stderr)
	}
	if !strings.Contains(verbose.stdout, "\nfound") {
		t.Errorf("get --verbose should show the found field:\n%s", verbose.stdout)
	}
	if !strings.Contains(verbose.stdout, "exists") && !strings.Contains(verbose.stdout, "missing") {
		t.Errorf("get --verbose should annotate paths with existence:\n%s", verbose.stdout)
	}

	// --json is identical with and without --verbose: verbose is text-only.
	jsonPlain := runCLI("--json", "agents", "get", "alpha-cli")
	jsonVerbose := runCLI("--json", "agents", "get", "alpha-cli", "--verbose")
	if jsonPlain.stdout != jsonVerbose.stdout {
		t.Errorf("--verbose changed --json output:\nplain:\n%s\nverbose:\n%s", jsonPlain.stdout, jsonVerbose.stdout)
	}
}

func TestGetTextDetailDrivenByRecord(t *testing.T) {
	// The text detail must show every inline scalar field the record carries, in
	// declared order, so it cannot drift from the JSON/--fields surfaces. found is
	// routed to a section (omitted), so it must not appear as an inline label.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "get", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("get exit = %d, stderr=%q", got.code, got.stderr)
	}
	for _, key := range []string{"id", "name", "version", "bin", "config_dir", "providers", "homepage"} {
		if !strings.Contains(got.stdout, key) {
			t.Errorf("text detail missing field %q:\n%s", key, got.stdout)
		}
	}
	if strings.Contains(got.stdout, "\nfound") {
		t.Errorf("text detail should not render the found field inline:\n%s", got.stdout)
	}
}

func TestAgentGetLevel(t *testing.T) {
	// agents get maps its requested output to the lowest enrichment level that can
	// fill it (R15): --models or a selected models field needs the full model list;
	// an unfiltered detail or a selected provider_env needs the count level; providers
	// alone is offline catalog data; anything else is offline facts only.
	cases := []struct {
		name   string
		flag   bool
		fields []string
		want   agentdex.Enrich
	}{
		{"unfiltered detail", false, nil, agentdex.EnrichCount},
		{"empty fields is unfiltered", false, []string{}, agentdex.EnrichCount},
		{"provider_env selected", false, []string{"provider_env"}, agentdex.EnrichCount},
		{"providers only", false, []string{"providers"}, agentdex.EnrichProviders},
		{"non-provider fields", false, []string{"id", "bin", "skills_dir"}, agentdex.EnrichNone},
		{"models flag", true, nil, agentdex.EnrichFull},
		{"models field", false, []string{"models"}, agentdex.EnrichFull},
		{"models field among others", false, []string{"id", "models"}, agentdex.EnrichFull},
		{"flag wins over non-provider fields", true, []string{"skills_dir"}, agentdex.EnrichFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentGetLevel(tc.flag, tc.fields); got != tc.want {
				t.Errorf("agentGetLevel(%v, %v) = %v, want %v", tc.flag, tc.fields, got, tc.want)
			}
		})
	}
}
