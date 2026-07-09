package cli

import (
	"strings"
	"testing"
)

func TestListDetectsInstalledAgents(t *testing.T) {
	newScenario(t, "", "alpha-cli", "beta-tool", "gamma-agent")

	got := runCLI("list")
	if got.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", got.code, got.stderr)
	}
	for _, id := range []string{"alpha-cli", "beta-tool", "gamma-agent"} {
		if !strings.Contains(got.stdout, id) {
			t.Errorf("list output missing %q:\n%s", id, got.stdout)
		}
	}
}

func TestListJSONEnvelope(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "list")
	if got.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	if env.Status != "ok" {
		t.Errorf("status = %q, want ok", env.Status)
	}
	rows, ok := env.Data.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("data = %#v, want one row", env.Data)
	}
	row := rows[0].(map[string]any)
	if row["id"] != "alpha-cli" {
		t.Errorf("row id = %v, want alpha-cli", row["id"])
	}
}

func TestListUnknownFieldRejectedRegardlessOfCardinality(t *testing.T) {
	// --fields validation is a property of the command, not of how many rows it
	// produced: an unknown field is a usage fault whether or not any agent is
	// detected. The empty scenario installs no binaries so detection yields nothing.
	for _, tc := range []struct {
		name string
		bins []string
	}{
		{"empty result set", nil},
		{"non-empty result set", []string{"alpha-cli"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newScenario(t, "", tc.bins...)
			got := runCLI("list", "--fields", "bogus")
			if got.code != codeUsage {
				t.Fatalf("list --fields bogus exit = %d, want 2; stderr=%q", got.code, got.stderr)
			}
		})
	}
}

func TestListValidButAbsentFieldResolvesBlank(t *testing.T) {
	// provider_env is a declared, selectable field but is only populated by get,
	// never by list: selecting it must resolve to a blank, not an unknown-field
	// error.
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "list", "--fields", "id,provider_env")
	if got.code != codeOK {
		t.Fatalf("list --fields id,provider_env exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	if v, ok := row["provider_env"]; !ok || v != "" {
		t.Errorf("provider_env = %v (present=%t), want present and blank", v, ok)
	}
}

func TestListVerboseAddsColumns(t *testing.T) {
	// --verbose widens the default columns with config_dir; plain list shows bin (a
	// default column) but not config_dir. --json is unaffected (it always carries the
	// full record).
	newScenario(t, "", "alpha-cli")

	plain := runCLI("list")
	if plain.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", plain.code, plain.stderr)
	}
	if !strings.Contains(plain.stdout, "BIN") {
		t.Errorf("plain list should show the bin column:\n%s", plain.stdout)
	}
	if strings.Contains(plain.stdout, "CONFIG_DIR") {
		t.Errorf("plain list should not show the config_dir column:\n%s", plain.stdout)
	}

	verbose := runCLI("list", "--verbose")
	if verbose.code != codeOK {
		t.Fatalf("list --verbose exit = %d, stderr=%q", verbose.code, verbose.stderr)
	}
	for _, col := range []string{"BIN", "CONFIG_DIR"} {
		if !strings.Contains(verbose.stdout, col) {
			t.Errorf("list --verbose missing %q column:\n%s", col, verbose.stdout)
		}
	}

	// --verbose is a text-only affordance: it must not widen the JSON payload.
	jsonPlain := runCLI("--json", "list")
	jsonVerbose := runCLI("--json", "list", "--verbose")
	if jsonPlain.stdout != jsonVerbose.stdout {
		t.Errorf("--verbose changed list --json output:\nplain:\n%s\nverbose:\n%s", jsonPlain.stdout, jsonVerbose.stdout)
	}
}

func TestListAllIncludesMissingAgents(t *testing.T) {
	// --all adds the catalogued-but-not-installed agents after the detected ones,
	// with "missing" in the bin cell; the plain list keeps omitting them. In JSON
	// the missing rows carry found:false and a blank bin, never the "missing"
	// marker (a text-surface affordance only).
	newScenario(t, "", "beta-tool")

	plain := runCLI("list")
	if plain.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", plain.code, plain.stderr)
	}
	if strings.Contains(plain.stdout, "alpha-cli") || strings.Contains(plain.stdout, "missing") {
		t.Errorf("plain list should omit missing agents:\n%s", plain.stdout)
	}

	all := runCLI("list", "--all")
	if all.code != codeOK {
		t.Fatalf("list --all exit = %d, stderr=%q", all.code, all.stderr)
	}
	for _, want := range []string{"alpha-cli", "beta-tool", "gamma-agent", "missing"} {
		if !strings.Contains(all.stdout, want) {
			t.Errorf("list --all missing %q:\n%s", want, all.stdout)
		}
	}
	// Detected agents read first: beta-tool (installed) above the missing tail.
	if strings.Index(all.stdout, "beta-tool") > strings.Index(all.stdout, "alpha-cli") {
		t.Errorf("list --all should order detected agents first:\n%s", all.stdout)
	}

	got := runCLI("--json", "list", "--all")
	if got.code != codeOK {
		t.Fatalf("list --all --json exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 3 {
		t.Fatalf("list --all rows = %d, want 3", len(rows))
	}
	byID := map[string]map[string]any{}
	for _, r := range rows {
		row := r.(map[string]any)
		byID[row["id"].(string)] = row
	}
	if found, _ := byID["beta-tool"]["found"].(bool); !found {
		t.Errorf("beta-tool found = %v, want true", byID["beta-tool"]["found"])
	}
	if found, _ := byID["alpha-cli"]["found"].(bool); found {
		t.Errorf("alpha-cli found = %v, want false", byID["alpha-cli"]["found"])
	}
	if bin := byID["alpha-cli"]["bin"]; bin != "" {
		t.Errorf("missing agent bin = %v, want blank", bin)
	}
}

func TestListShowsModelsColumn(t *testing.T) {
	// list always enriches each agent with its models.dev model count, surfacing a
	// MODELS column between PROVIDERS and BIN. The fixture's anthropic provider
	// offers one model, so alpha-cli's enriched list carries it.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("list")
	if got.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "MODELS") {
		t.Errorf("list missing the models column:\n%s", got.stdout)
	}
	// MODELS sits between PROVIDERS and BIN in the default columns.
	pi, mi, bi := strings.Index(got.stdout, "PROVIDERS"), strings.Index(got.stdout, "MODELS"), strings.Index(got.stdout, "BIN")
	if pi < 0 || pi >= mi || mi >= bi {
		t.Errorf("columns out of order, want PROVIDERS < MODELS < BIN:\n%s", got.stdout)
	}

	j := runCLI("--json", "list")
	row := j.envelope(t).Data.([]any)[0].(map[string]any)
	models, ok := row["models"].([]any)
	if !ok || len(models) != 1 {
		t.Errorf("list --json should carry one enriched model for alpha-cli: %v", row["models"])
	}
}

func TestListDegradesWhenModelsUnreachable(t *testing.T) {
	// list always attempts enrichment, but a models.dev outage with no cache must
	// degrade to a zero model count rather than failing the listing, and warn so the
	// zero reads as "unavailable" rather than a genuine empty catalog.
	newScenario(t, closedModelsServer(t), "alpha-cli")

	got := runCLI("list")
	if got.code != codeOK {
		t.Fatalf("list with unreachable models.dev exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "MODELS") {
		t.Errorf("degraded list should still show the models column:\n%s", got.stdout)
	}

	// The degraded JSON carries [] (not null) so it matches the "0" count cell and
	// stays scripting-safe, and a warning explains the zero.
	j := runCLI("--json", "list")
	env := j.envelope(t)
	if !anyContains(env.Warnings, "unreachable") {
		t.Errorf("degraded list should warn that model counts are unavailable: %v", env.Warnings)
	}
	row := env.Data.([]any)[0].(map[string]any)
	models, ok := row["models"].([]any)
	if !ok || len(models) != 0 {
		t.Errorf("degraded list --json should carry an empty models array, got %#v", row["models"])
	}
}

func TestListDegradesOnModelsSchemaDrift(t *testing.T) {
	// Malformed models.dev data must not kill the listing: detection is sound, so
	// list degrades the models column and warns rather than failing the whole
	// command (unlike get/models, where models are central and drift is fatal).
	srv := modelsServer(t, nil, "anthropic") // anthropic ships a malformed model
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "list")
	if got.code != codeOK {
		t.Fatalf("list on models schema drift exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	env := got.envelope(t)
	rows, ok := env.Data.([]any)
	if !ok || len(rows) != 1 || rows[0].(map[string]any)["id"] != "alpha-cli" {
		t.Fatalf("list should still report the detected agent: %v", env.Data)
	}
	if !anyContains(env.Warnings, "model counts omitted") {
		t.Errorf("list should warn that model counts were omitted: %v", env.Warnings)
	}
}

func TestListJSONCarriesFullRecord(t *testing.T) {
	// --json is a data format, not a view: without --fields it carries the full
	// record per row, not just the default table columns, so machine consumers are
	// never silently truncated. bin, config_dir, and homepage are non-default fields.
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "list")
	if got.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	for _, key := range []string{"bin", "config_dir", "homepage"} {
		if _, ok := row[key]; !ok {
			t.Errorf("list --json should carry non-default field %q: %v", key, row)
		}
	}
}

func TestListWarnsOnStaleCatalog(t *testing.T) {
	// list must surface catalog staleness like get and models do: once a version is
	// resolved, a re-resolution that can no longer reach the registry serves the last
	// resolved version and reports stale, which list now warns about rather than
	// silently using out-of-date data. A zero catalog TTL forces re-resolution on
	// every load so the offline second run yields the stale verdict.
	s := newScenario(t, "", "alpha-cli")
	s.writeConfig(t, "color: \"never\"\nsearch_dirs: [\""+s.binDir+"\"]\ncatalog: ttl: \"0s\"\n")

	if got := runCLI("list"); got.code != codeOK {
		t.Fatalf("warm list exit = %d; stderr=%q", got.code, got.stderr)
	}

	s.closeRegistry() // re-resolution can no longer reach the registry

	got := runCLI("--json", "list")
	if got.code != codeOK {
		t.Fatalf("stale list exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if !anyContains(got.envelope(t).Warnings, "stale") {
		t.Errorf("stale list should warn about staleness: %v", got.envelope(t).Warnings)
	}
}
