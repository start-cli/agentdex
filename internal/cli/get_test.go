package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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

	got := runCLI("--json", "get", "alpha-cli")
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
	text := runCLI("get", "alpha-cli")
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

	got := runCLI("--json", "get", "alpha-cli", "--models")
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

	text := runCLI("get", "alpha-cli", "--models")
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

	got := runCLI("--json", "get", "alpha-cli", "--fields", "models")
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

	got := runCLI("--json", "get", "alpha-cli", "--fields", "skills_dir")
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

	got := runCLI("--json", "get", "alpha-cli", "--models", "--fields", "skills_dir")
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

	got := runCLI("get", "alpha-cli", "--no-models")
	if got.code != codeUsage {
		t.Fatalf("--no-models exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestGetSomePresentWarns(t *testing.T) {
	// gamma-agent uses [google, openai]; only google is present upstream.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "gamma-agent")

	got := runCLI("--json", "get", "gamma-agent")
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

	got := runCLI("get", "alpha-cli")
	if got.code != codeConfig {
		t.Fatalf("none-present exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

func TestGetSchemaIsDataError(t *testing.T) {
	srv := modelsServer(t, []string{"google"}, "anthropic")
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("get", "alpha-cli")
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

	got := runCLI("get", "alpha-cli")
	if got.code != codeConfig {
		t.Fatalf("top-level schema drift exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

func TestGetNotInstalled(t *testing.T) {
	// A catalogued-but-not-installed agent still exits 3, but renders everything
	// the catalog knows first: the detail with a "missing" bin to stdout, the
	// error to stderr. Under --json the envelope carries both data and error.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL) // no binaries installed

	got := runCLI("get", "alpha-cli")
	if got.code != codeNotFound {
		t.Fatalf("not-installed exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
	for _, want := range []string{"alpha-cli", "missing", ".alpha"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("not-installed detail missing %q:\n%s", want, got.stdout)
		}
	}
	if !strings.Contains(got.stderr, "not installed") {
		t.Errorf("not-installed error missing from stderr: %q", got.stderr)
	}

	js := runCLI("--json", "get", "alpha-cli")
	if js.code != codeNotFound {
		t.Fatalf("not-installed --json exit = %d, want 3", js.code)
	}
	env := js.envelope(t)
	if env.Status != "error" || !strings.Contains(env.Error, "not installed") {
		t.Errorf("envelope status/error = %q/%q, want error naming not installed", env.Status, env.Error)
	}
	data, ok := env.Data.(map[string]any)
	if !ok || data["found"] != false || data["bin"] != "" {
		t.Errorf("envelope data = %v, want found=false with blank bin", env.Data)
	}
}

func TestGetUnknownQuery(t *testing.T) {
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("get", "no-such-thing")
	if got.code != codeUsage {
		t.Fatalf("unknown exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "alpha-cli") {
		t.Errorf("unknown error should list valid ids: %q", got.stderr)
	}
}

func TestGetUncataloguedProviderMatch(t *testing.T) {
	// "google" is not a catalog agent but is a models.dev provider. Without
	// --models the fallthrough reports identity only — no models dump.
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "get", "google")
	if got.code != codeNotFound {
		t.Fatalf("uncatalogued-provider exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
	data, ok := got.envelope(t).Data.(map[string]any)
	if !ok || data["provider"] != "google" {
		t.Errorf("expected provider data labelled google: %v", got.envelope(t).Data)
	}
	if _, ok := data["models"]; ok {
		t.Errorf("fallthrough without --models should omit models: %v", data)
	}
	if data["name"] == nil || data["name"] == "" {
		t.Errorf("fallthrough should report provider name: %v", data)
	}

	text := runCLI("get", "google")
	if text.code != codeNotFound {
		t.Fatalf("fallthrough text exit = %d, want 3; stderr=%q", text.code, text.stderr)
	}
	if !hasTextSection(text.stdout, "Provider") {
		t.Errorf("fallthrough text should show Provider section:\n%s", text.stdout)
	}
	if hasTextSection(text.stdout, "Models") {
		t.Errorf("fallthrough without --models should omit Models section:\n%s", text.stdout)
	}
}

func TestGetUncataloguedProviderMatchWithModels(t *testing.T) {
	srv := modelsServer(t, []string{"google"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "get", "google", "--models")
	if got.code != codeNotFound {
		t.Fatalf("uncatalogued-provider --models exit = %d, want 3; stderr=%q", got.code, got.stderr)
	}
	data, ok := got.envelope(t).Data.(map[string]any)
	if !ok || data["provider"] != "google" {
		t.Errorf("expected provider data labelled google: %v", got.envelope(t).Data)
	}
	models, ok := data["models"].([]any)
	if !ok || len(models) == 0 {
		t.Errorf("fallthrough --models should include models: %v", data["models"])
	}

	text := runCLI("get", "google", "--models")
	if text.code != codeNotFound {
		t.Fatalf("fallthrough --models text exit = %d, want 3; stderr=%q", text.code, text.stderr)
	}
	if !hasTextSection(text.stdout, "Provider") {
		t.Errorf("fallthrough --models text should keep Provider section:\n%s", text.stdout)
	}
	if !hasTextSection(text.stdout, "Models") {
		t.Errorf("fallthrough --models text should show Models section:\n%s", text.stdout)
	}
}

func TestGetDegradesWhenModelsUnreachable(t *testing.T) {
	newScenario(t, closedModelsServer(t), "alpha-cli")

	got := runCLI("--json", "get", "alpha-cli")
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

	got := runCLI("get", "alpha-cli")
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

	plain := runCLI("get", "alpha-cli")
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

	verbose := runCLI("get", "alpha-cli", "--verbose")
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
	jsonPlain := runCLI("--json", "get", "alpha-cli")
	jsonVerbose := runCLI("--json", "get", "alpha-cli", "--verbose")
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

	got := runCLI("get", "alpha-cli")
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

func TestModelsDemand(t *testing.T) {
	cases := []struct {
		name   string
		flag   bool
		fields []string
		want   bool
	}{
		{"unfiltered", false, nil, false},
		{"empty fields", false, []string{}, false},
		{"flag only", true, nil, true},
		{"fields models", false, []string{"models"}, true},
		{"fields other", false, []string{"skills_dir"}, false},
		{"flag and omit fields", true, []string{"skills_dir"}, true},
		{"fields models among others", false, []string{"id", "models"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelsDemand(tc.flag, tc.fields); got != tc.want {
				t.Errorf("modelsDemand(%v, %v) = %v, want %v", tc.flag, tc.fields, got, tc.want)
			}
		})
	}
}
