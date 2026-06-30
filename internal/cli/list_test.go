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
	// --verbose widens the default columns with bin and config; plain list shows
	// neither header. --json is unaffected (it always carries the full record).
	newScenario(t, "", "alpha-cli")

	plain := runCLI("list")
	if plain.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", plain.code, plain.stderr)
	}
	if strings.Contains(plain.stdout, "BIN") || strings.Contains(plain.stdout, "CONFIG") {
		t.Errorf("plain list should not show bin/config columns:\n%s", plain.stdout)
	}

	verbose := runCLI("list", "--verbose")
	if verbose.code != codeOK {
		t.Fatalf("list --verbose exit = %d, stderr=%q", verbose.code, verbose.stderr)
	}
	for _, col := range []string{"BIN", "CONFIG"} {
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

func TestListModelsAddsCountColumn(t *testing.T) {
	// --models surfaces a model column in the default text table; plain list does
	// not. The fixture's anthropic provider offers one model.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	plain := runCLI("list")
	if strings.Contains(plain.stdout, "MODELS") {
		t.Errorf("plain list should not show a models column:\n%s", plain.stdout)
	}

	got := runCLI("list", "--models")
	if got.code != codeOK {
		t.Fatalf("list --models exit = %d, stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "MODELS") {
		t.Errorf("list --models missing the models column:\n%s", got.stdout)
	}
}

func TestListDefaultDoesNotEnrich(t *testing.T) {
	// A closed models server proves default list never reaches the network: it
	// must succeed without ever consulting models.dev.
	newScenario(t, closedModelsServer(t), "alpha-cli")

	got := runCLI("--json", "list")
	if got.code != codeOK {
		t.Fatalf("default list exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	if _, present := row["models"]; present {
		t.Errorf("default list enriched models without --models: %v", row)
	}
}

func TestListJSONCarriesFullRecord(t *testing.T) {
	// --json is a data format, not a view: without --fields it carries the full
	// record per row, not just the default table columns, so machine consumers are
	// never silently truncated. bin, config, and homepage are non-default fields.
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "list")
	if got.code != codeOK {
		t.Fatalf("list exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	for _, key := range []string{"bin", "config", "homepage"} {
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
