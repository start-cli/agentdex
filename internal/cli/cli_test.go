package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionEnvelope(t *testing.T) {
	got := runCLI("--json", "version")
	if got.code != codeOK {
		t.Fatalf("version exit = %d", got.code)
	}
	data := got.envelope(t).Data.(map[string]any)
	for _, k := range []string{"version", "commit", "date"} {
		if _, ok := data[k]; !ok {
			t.Errorf("version data missing %q: %v", k, data)
		}
	}
}

func TestMalformedConfigExits78(t *testing.T) {
	s := newScenario(t, "", "alpha-cli")
	s.writeConfig(t, `color: "not-a-mode"`)

	got := runCLI("list")
	if got.code != codeConfig {
		t.Fatalf("malformed config exit = %d, want 78; stderr=%q", got.code, got.stderr)
	}
}

func TestUnreadableConfigExits4(t *testing.T) {
	// A config.cue that cannot be read for a permission reason is distinct from a
	// validity fault: it exits permission (4), not config (78).
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	s := newScenario(t, "", "alpha-cli")
	cfgPath := filepath.Join(s.configDir, "config.cue")
	if err := os.Chmod(cfgPath, 0o000); err != nil {
		t.Fatalf("chmod config unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgPath, 0o644) })

	got := runCLI("list")
	if got.code != codePermission {
		t.Fatalf("unreadable config exit = %d, want 4; stderr=%q", got.code, got.stderr)
	}
}

func TestMalformedConfigDoesNotBreakVersion(t *testing.T) {
	s := newScenario(t, "", "alpha-cli")
	s.writeConfig(t, `bogus_field: 1`)

	got := runCLI("version")
	if got.code != codeOK {
		t.Fatalf("version with malformed config exit = %d, want 0", got.code)
	}
}

func TestEnrichPrecedenceConfigDisables(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	s := newScenario(t, srv.URL, "alpha-cli")
	// enrich_models false makes get default to no per-model enrichment.
	s.writeConfig(t, configBody(srv.URL, s.binDir, "enrich_models: false\n"))

	got := runCLI("--json", "get", "alpha-cli")
	if got.code != codeOK {
		t.Fatalf("get exit = %d, stderr=%q", got.code, got.stderr)
	}
	data := got.envelope(t).Data.(map[string]any)
	if _, ok := data["models"]; ok {
		t.Errorf("enrich_models:false should omit models by default: %v", data)
	}
	if _, ok := data["provider_env"]; !ok {
		t.Errorf("provider_env should still show: %v", data)
	}
}

func TestEnrichPrecedenceFlagOverridesConfig(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	s := newScenario(t, srv.URL, "alpha-cli")
	s.writeConfig(t, configBody(srv.URL, s.binDir, "enrich_models: false\n"))

	got := runCLI("--json", "get", "alpha-cli", "--models")
	if got.code != codeOK {
		t.Fatalf("get --models exit = %d, stderr=%q", got.code, got.stderr)
	}
	if _, ok := got.envelope(t).Data.(map[string]any)["models"]; !ok {
		t.Errorf("--models should override enrich_models:false")
	}
}

func TestListFieldsSelection(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "list", "--fields", "id,version")
	if got.code != codeOK {
		t.Fatalf("list --fields exit = %d, stderr=%q", got.code, got.stderr)
	}
	row := got.envelope(t).Data.([]any)[0].(map[string]any)
	if len(row) != 2 {
		t.Errorf("expected exactly id,version: %v", row)
	}
	if _, ok := row["id"]; !ok {
		t.Errorf("missing id: %v", row)
	}
}

func TestUnknownFieldIsUsageError(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("list", "--fields", "nope")
	if got.code != codeUsage {
		t.Fatalf("unknown field exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestInvalidColorFlagIsUsageError(t *testing.T) {
	// An out-of-range --color value is settled in preRun before any command runs,
	// so it is a usage fault (exit 2) regardless of the subcommand.
	newScenario(t, "", "alpha-cli")

	got := runCLI("--color", "rainbow", "list")
	if got.code != codeUsage {
		t.Fatalf("invalid --color exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}

	// Under --json the usage fault must still arrive as the standard envelope on
	// stdout, not as plain stderr text, like every other usage error.
	gotJSON := runCLI("--json", "--color", "rainbow", "list")
	if gotJSON.code != codeUsage {
		t.Fatalf("invalid --color --json exit = %d, want 2; stderr=%q", gotJSON.code, gotJSON.stderr)
	}
	env := gotJSON.envelope(t)
	if env.Status != "error" || !strings.Contains(env.Error, "--color") {
		t.Errorf("invalid --color --json envelope = %+v, want an error naming --color", env)
	}
}

func TestMalformedBinPathIsUsageError(t *testing.T) {
	// A --bin-path entry that is not id=path cannot be mapped to an override, so it
	// fails fast as a usage error.
	newScenario(t, "", "alpha-cli")

	got := runCLI("list", "--bin-path", "no-equals-sign")
	if got.code != codeUsage {
		t.Fatalf("malformed --bin-path exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

// configBody builds a config.cue with the standard search dir and models URL plus
// extra lines, for tests that need a bespoke configuration.
func configBody(modelsURL, binDir, extra string) string {
	var b strings.Builder
	b.WriteString("color: \"never\"\n")
	b.WriteString("search_dirs: [\"" + binDir + "\"]\n")
	if modelsURL != "" {
		b.WriteString("models: url: \"" + modelsURL + "\"\n")
	}
	b.WriteString(extra)
	return b.String()
}
