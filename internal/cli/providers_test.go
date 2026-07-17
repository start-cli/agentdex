package cli

import (
	"strings"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// fakeLookup builds an os.LookupEnv-shaped function from a set of "present" names,
// so env-presence is driven from inputs without t.Setenv.
func fakeLookup(set ...string) func(string) (string, bool) {
	present := make(map[string]bool, len(set))
	for _, name := range set {
		present[name] = true
	}
	return func(name string) (string, bool) {
		if present[name] {
			return "value", true
		}
		return "", false
	}
}

func TestEnvPresenceReadsPresenceOnly(t *testing.T) {
	got := envPresence([]string{"FOO_KEY", "BAR_KEY"}, fakeLookup("FOO_KEY"))
	if !got["FOO_KEY"] {
		t.Errorf("FOO_KEY presence = false, want true")
	}
	if got["BAR_KEY"] {
		t.Errorf("BAR_KEY presence = true, want false")
	}
}

func TestProviderRecordEnvAndPresence(t *testing.T) {
	// A set variable gains the (set) suffix in the env cell and an unset one stays
	// bare; the structured present map carries the booleans without the suffix.
	p := modelsdev.Provider{
		ID:   "acme",
		Name: "Acme",
		Env:  []string{"FOO_KEY", "BAR_KEY"},
		Models: map[string]modelsdev.Model{
			"m1": {ID: "m1"},
			"m2": {ID: "m2"},
		},
	}
	present := envPresence(p.Env, fakeLookup("FOO_KEY"))
	fs, err := providerRecord(p, present).resolve(nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	by := map[string]field{}
	for _, f := range fs {
		by[f.key] = f
	}

	if got, want := by["env"].text, "BAR_KEY, FOO_KEY (set)"; got != want {
		t.Errorf("env cell = %q, want %q", got, want)
	}
	names, ok := by["env"].val.([]string)
	if !ok || len(names) != 2 || names[0] != "BAR_KEY" || names[1] != "FOO_KEY" {
		t.Errorf("env val = %v, want sorted [BAR_KEY FOO_KEY]", by["env"].val)
	}
	pm, ok := by["present"].val.(map[string]bool)
	if !ok || !pm["FOO_KEY"] || pm["BAR_KEY"] {
		t.Errorf("present val = %v, want {FOO_KEY:true BAR_KEY:false}", by["present"].val)
	}
	models, ok := by["models"].val.([]modelsdev.Model)
	if !ok || len(models) != 2 {
		t.Errorf("models val = %v, want a 2-element slice", by["models"].val)
	}
	if by["models"].text != "2" {
		t.Errorf("models cell = %q, want the count 2", by["models"].text)
	}
}

func TestProviderRecordNoEnvBlankCell(t *testing.T) {
	p := modelsdev.Provider{ID: "acme", Name: "Acme"}
	fs, _ := providerRecord(p, envPresence(p.Env, fakeLookup())).resolve(nil)
	for _, f := range fs {
		if f.key == "env" && f.text != "" {
			t.Errorf("env cell for a provider with no declared var = %q, want blank", f.text)
		}
	}
}

func TestProvidersListAllSortedByID(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "providers")
	if got.code != codeOK {
		t.Fatalf("providers exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.(map[string]any)["id"].(string))
	}
	want := []string{"anthropic", "google", "openai"}
	if len(ids) != len(want) {
		t.Fatalf("provider ids = %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Errorf("provider ids = %v, want sorted %v", ids, want)
		}
	}
}

func TestProvidersFilterNarrows(t *testing.T) {
	// "E" matches google and openai (case-insensitive substring), not anthropic,
	// and matching several lists all of them rather than reporting ambiguity.
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "providers", "E")
	if got.code != codeOK {
		t.Fatalf("providers filter exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.(map[string]any)["id"].(string))
	}
	if len(ids) != 2 || ids[0] != "google" || ids[1] != "openai" {
		t.Errorf("filter %q ids = %v, want [google openai]", "E", ids)
	}
}

func TestProvidersFilterNoMatchIsEmptyExitZero(t *testing.T) {
	// A filter matching nothing is a normal browse outcome, not not-found.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "providers", "no-such-provider")
	if got.code != codeOK {
		t.Fatalf("no-match filter exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if rows := got.envelope(t).Data.([]any); len(rows) != 0 {
		t.Errorf("no-match filter data = %v, want empty", rows)
	}

	text := runCLI("providers", "no-such-provider")
	if !strings.Contains(text.stdout, "No providers.") {
		t.Errorf("no-match text output missing empty-state line:\n%s", text.stdout)
	}
}

func TestProvidersJSONModelsIsArrayCellIsCount(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "providers", "anthropic")
	if got.code != codeOK {
		t.Fatalf("providers exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	models, ok := row["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models field = %v, want a 1-element JSON array", row["models"])
	}

	// The MODELS cell renders the array length, so id,models isolates it from any
	// incidental "1" elsewhere in the row.
	text := runCLI("providers", "anthropic", "--fields", "id,models")
	if text.code != codeOK {
		t.Fatalf("providers --fields id,models exit = %d, stderr=%q", text.code, text.stderr)
	}
	if !strings.Contains(text.stdout, "MODELS") {
		t.Errorf("text output missing MODELS column:\n%s", text.stdout)
	}
	cell := ""
	for _, line := range strings.Split(text.stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "anthropic") {
			f := strings.Fields(line)
			cell = f[len(f)-1]
		}
	}
	if cell != "1" {
		t.Errorf("MODELS cell = %q, want the model count 1:\n%s", cell, text.stdout)
	}
}

func TestProvidersFieldsSelectionAndValidation(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "providers", "anthropic", "--fields", "id,present")
	if got.code != codeOK {
		t.Fatalf("providers --fields exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	if _, ok := row["present"]; !ok {
		t.Errorf("--fields id,present should carry present: %v", row)
	}
	if _, ok := row["name"]; ok {
		t.Errorf("--fields id,present should not carry name: %v", row)
	}

	// --fields drives the text table columns too, not just the JSON payload.
	text := runCLI("providers", "anthropic", "--fields", "id,present")
	if text.code != codeOK {
		t.Fatalf("providers --fields text exit = %d, stderr=%q", text.code, text.stderr)
	}
	if !strings.Contains(text.stdout, "PRESENT") {
		t.Errorf("--fields id,present text output should include the PRESENT column:\n%s", text.stdout)
	}
	for _, col := range []string{"NAME", "ENV", "MODELS"} {
		if strings.Contains(text.stdout, col) {
			t.Errorf("--fields id,present text output should drop the default %s column:\n%s", col, text.stdout)
		}
	}

	bad := runCLI("providers", "anthropic", "--fields", "bogus")
	if bad.code != codeUsage {
		t.Fatalf("unknown --fields exit = %d, want 2; stderr=%q", bad.code, bad.stderr)
	}
}

func TestProvidersUnknownFieldRejectedOnEmptyResult(t *testing.T) {
	// Field validation must not depend on result cardinality: a filter matching no
	// provider still rejects an unknown --fields key as a usage fault, not exit 0.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL)

	got := runCLI("providers", "no-such-provider", "--fields", "bogus")
	if got.code != codeUsage {
		t.Fatalf("unknown --fields on empty result exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestProvidersTransientWhenUnreachable(t *testing.T) {
	newScenario(t, closedModelsServer(t))

	got := runCLI("providers")
	if got.code != codeTransient {
		t.Fatalf("providers unreachable exit = %d, want 75; stderr=%q", got.code, got.stderr)
	}
}

func TestProvidersSchemaDriftIsConfig(t *testing.T) {
	// An empty top-level providers map is gross structural drift caught by
	// Catalog's validateTopLevel; per-model faults are not this command's concern.
	srv := modelsServer(t, nil)
	newScenario(t, srv.URL)

	got := runCLI("providers")
	if got.code != codeConfig {
		t.Fatalf("providers schema drift exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}
