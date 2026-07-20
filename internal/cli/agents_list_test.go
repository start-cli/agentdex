package cli

import (
	"strings"
	"testing"
)

func TestListDetectsInstalledAgents(t *testing.T) {
	newScenario(t, "", "alpha-cli", "beta-tool", "gamma-agent")

	got := runCLI("agents", "list", "--installed")
	if got.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", got.code, got.stderr)
	}
	for _, id := range []string{"alpha-cli", "beta-tool", "gamma-agent"} {
		if !strings.Contains(got.stdout, id) {
			t.Errorf("list output missing %q:\n%s", id, got.stdout)
		}
	}
}

func TestListFilterNarrowsByIDAndName(t *testing.T) {
	// The positional filter is a browse narrowing over id and name: "alpha" matches
	// alpha-cli only, and matching several lists all of them. Detection order and
	// enrichment are unaffected.
	newScenario(t, "", "alpha-cli", "beta-tool", "gamma-agent")

	got := runCLI("--json", "agents", "list", "alpha")
	if got.code != codeOK {
		t.Fatalf("list filter exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "alpha-cli" {
		t.Errorf("filter %q rows = %v, want just alpha-cli", "alpha", rows)
	}

	// A case-insensitive name substring matches too: "tool" hits Beta Tool.
	byName := runCLI("--json", "agents", "list", "TOOL")
	if byName.code != codeOK {
		t.Fatalf("list name filter exit = %d, stderr=%q", byName.code, byName.stderr)
	}
	rows = byName.envelope(t).Data.([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "beta-tool" {
		t.Errorf("name filter %q rows = %v, want just beta-tool", "TOOL", rows)
	}
}

func TestListFilterNoMatchIsEmptyExitZero(t *testing.T) {
	// A filter matching nothing is a normal browse outcome, not not-found.
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "agents", "list", "no-such-agent")
	if got.code != codeOK {
		t.Fatalf("no-match filter exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if rows := got.envelope(t).Data.([]any); len(rows) != 0 {
		t.Errorf("no-match filter data = %v, want empty", rows)
	}

	text := runCLI("agents", "list", "no-such-agent")
	if !strings.Contains(text.stdout, `No agents match "no-such-agent".`) {
		t.Errorf("no-match text output missing filter-aware empty-state line:\n%s", text.stdout)
	}
}

func TestListJSONEnvelope(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "agents", "list", "--installed")
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
			got := runCLI("agents", "list", "--fields", "bogus")
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

	got := runCLI("--json", "agents", "list", "--fields", "id,provider_env")
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

	plain := runCLI("agents", "list")
	if plain.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", plain.code, plain.stderr)
	}
	if !strings.Contains(plain.stdout, "BIN") {
		t.Errorf("plain list should show the bin column:\n%s", plain.stdout)
	}
	if strings.Contains(plain.stdout, "CONFIG_DIR") {
		t.Errorf("plain list should not show the config_dir column:\n%s", plain.stdout)
	}

	verbose := runCLI("agents", "list", "--verbose")
	if verbose.code != codeOK {
		t.Fatalf("list --verbose exit = %d, stderr=%q", verbose.code, verbose.stderr)
	}
	for _, col := range []string{"BIN", "CONFIG_DIR"} {
		if !strings.Contains(verbose.stdout, col) {
			t.Errorf("list --verbose missing %q column:\n%s", col, verbose.stdout)
		}
	}

	// --verbose is a text-only affordance: it must not widen the JSON payload.
	jsonPlain := runCLI("--json", "agents", "list")
	jsonVerbose := runCLI("--json", "agents", "list", "--verbose")
	if jsonPlain.stdout != jsonVerbose.stdout {
		t.Errorf("--verbose changed list --json output:\nplain:\n%s\nverbose:\n%s", jsonPlain.stdout, jsonVerbose.stdout)
	}
}

func TestListDefaultIncludesMissingAgents(t *testing.T) {
	// The default listing is the whole catalog: the catalogued-but-not-installed
	// agents follow the detected ones, with "missing" in the bin cell. --installed
	// narrows to just the detected agents. In JSON the missing rows carry found:false
	// and a blank bin, never the "missing" marker (a text-surface affordance only).
	newScenario(t, "", "beta-tool")

	installed := runCLI("agents", "list", "--installed")
	if installed.code != codeOK {
		t.Fatalf("list --installed exit = %d, stderr=%q", installed.code, installed.stderr)
	}
	if strings.Contains(installed.stdout, "alpha-cli") || strings.Contains(installed.stdout, "missing") {
		t.Errorf("list --installed should omit missing agents:\n%s", installed.stdout)
	}

	all := runCLI("agents", "list")
	if all.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", all.code, all.stderr)
	}
	for _, want := range []string{"alpha-cli", "beta-tool", "gamma-agent", "delta-agent", "missing"} {
		if !strings.Contains(all.stdout, want) {
			t.Errorf("default list missing %q:\n%s", want, all.stdout)
		}
	}
	// Detected agents read first: beta-tool (installed) above the missing tail.
	if strings.Index(all.stdout, "beta-tool") > strings.Index(all.stdout, "alpha-cli") {
		t.Errorf("default list should order detected agents first:\n%s", all.stdout)
	}

	got := runCLI("--json", "agents", "list")
	if got.code != codeOK {
		t.Fatalf("list --json exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	if len(rows) != 4 {
		t.Fatalf("default list rows = %d, want 4", len(rows))
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
	// delta-agent is provider-agnostic and no --provider was given: it lists (never
	// fails the command) with models not-applicable (JSON null), distinct from a
	// home-provider agent's degraded empty list.
	if _, ok := byID["delta-agent"]; !ok {
		t.Fatalf("default list omitted delta-agent:\n%s", got.stdout)
	}
	if models := byID["delta-agent"]["models"]; models != nil {
		t.Errorf("agnostic delta-agent models = %v, want null", models)
	}
}

func TestListShowsModelsColumn(t *testing.T) {
	// list always enriches each agent with its models.dev model count, surfacing a
	// MODELS column between PROVIDERS and BIN. The fixture's anthropic provider
	// offers one model, so alpha-cli's enriched list carries it.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("agents", "list")
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

	j := runCLI("--json", "agents", "list")
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

	got := runCLI("agents", "list")
	if got.code != codeOK {
		t.Fatalf("list with unreachable models.dev exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "MODELS") {
		t.Errorf("degraded list should still show the models column:\n%s", got.stdout)
	}

	// The degraded JSON carries [] (not null) so it matches the "0" count cell and
	// stays scripting-safe, and a warning explains the zero.
	j := runCLI("--json", "agents", "list")
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

	got := runCLI("--json", "agents", "list", "--installed")
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

	got := runCLI("--json", "agents", "list")
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

	if got := runCLI("agents", "list"); got.code != codeOK {
		t.Fatalf("warm list exit = %d; stderr=%q", got.code, got.stderr)
	}

	s.closeRegistry() // re-resolution can no longer reach the registry

	got := runCLI("--json", "agents", "list")
	if got.code != codeOK {
		t.Fatalf("stale list exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if !anyContains(got.envelope(t).Warnings, "stale") {
		t.Errorf("stale list should warn about staleness: %v", got.envelope(t).Warnings)
	}
}
