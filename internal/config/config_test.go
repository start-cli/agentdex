package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.cue")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadMissingFileIsEmptyDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.cue"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got.CatalogModule != "github.com/start-cli/agentdex/catalog@v1" {
		t.Errorf("CatalogModule = %q, want the default module path", got.CatalogModule)
	}
	if got.Color != "auto" {
		t.Errorf("Color = %q, want default auto", got.Color)
	}
	if got.CatalogTTL != DefaultTTL || got.ModelsTTL != DefaultTTL {
		t.Errorf("TTLs = %v/%v, want default %v", got.CatalogTTL, got.ModelsTTL, DefaultTTL)
	}
}

func TestLoadFieldsAndTTLResolution(t *testing.T) {
	path := writeConfig(t, `
cache_ttl: "1h"
catalog: ttl: "2h"
catalog: dir: "./local-catalog"
models: url: "https://mirror.example/catalog.json"
search_dirs: ["/opt/bin", "/usr/local/bin"]
bin_paths: "claude-code": "/custom/claude"
color: "never"
`)
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.CatalogTTL != 2*time.Hour {
		t.Errorf("CatalogTTL = %v, want section ttl 2h", got.CatalogTTL)
	}
	if got.CatalogDir != "./local-catalog" {
		t.Errorf("CatalogDir = %q, want ./local-catalog", got.CatalogDir)
	}
	if got.ModelsTTL != time.Hour {
		t.Errorf("ModelsTTL = %v, want cache_ttl fallback 1h", got.ModelsTTL)
	}
	if got.ModelsURL != "https://mirror.example/catalog.json" {
		t.Errorf("ModelsURL = %q", got.ModelsURL)
	}
	if got.Color != "never" {
		t.Errorf("Color = %q, want never", got.Color)
	}
	if len(got.SearchDirs) != 2 || got.BinPaths["claude-code"] != "/custom/claude" {
		t.Errorf("collection fields decoded wrong: %+v", got)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, `unknown_field: true`)
	_, err := Load(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("Load unknown field err = %v, want ErrConfig", err)
	}
}

func TestLoadRejectsRemovedEnrichModels(t *testing.T) {
	// enrich_models was removed; leftover keys fail closed-schema validation.
	path := writeConfig(t, `enrich_models: false`)
	_, err := Load(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("Load enrich_models err = %v, want ErrConfig", err)
	}
}

func TestLoadRejectsRemovedDisabledAgents(t *testing.T) {
	// disabled_agents was removed; a config.cue still setting it must fail closed
	// rather than silently ignore a key that no longer does anything (R11).
	path := writeConfig(t, `disabled_agents: ["foo"]`)
	_, err := Load(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("Load disabled_agents err = %v, want ErrConfig", err)
	}
}

func TestLoadRejectsBadType(t *testing.T) {
	path := writeConfig(t, `color: "purple"`)
	_, err := Load(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("Load bad enum err = %v, want ErrConfig", err)
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	path := writeConfig(t, `cache_ttl: "not-a-duration"`)
	_, err := Load(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("Load bad duration err = %v, want ErrConfig", err)
	}
}

func TestLoadRejectsSyntaxError(t *testing.T) {
	path := writeConfig(t, `color: "never`)
	_, err := Load(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("Load syntax error err = %v, want ErrConfig", err)
	}
}
