package agentdex

import (
	"context"
	"errors"
	"testing"

	"github.com/start-cli/agentdex/internal/catalog"
	"github.com/start-cli/agentdex/internal/catalogtest"
)

// TestLoadCatalogMapsToPublicTypes drives the loader through a stub registry and
// asserts the root-package mapping yields a public Catalog whose KnownAgent.ID
// values equal their map keys across the fixture's multiple entries and
// providers, with the optional fields populated as authored.
func TestLoadCatalogMapsToPublicTypes(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	stub := catalogtest.Serve("v1.0.0", dir)
	loader := catalog.New(stub,
		catalog.WithModulePath("github.com/start-cli/agentdex/catalog@v1"),
		catalog.WithCacheDir(t.TempDir()),
	)

	cat, stale, err := loadCatalog(context.Background(), loader)
	if err != nil {
		t.Fatalf("loadCatalog: %v", err)
	}
	if stale {
		t.Error("stale = true, want false on a fresh resolve")
	}

	if len(cat.Agents) != 3 {
		t.Fatalf("got %d agents, want 3", len(cat.Agents))
	}
	for id, a := range cat.Agents {
		if a.ID != id {
			t.Errorf("agent %q has ID %q; ID must equal its map key", id, a.ID)
		}
	}

	alpha := cat.Agents["alpha-cli"]
	if alpha.Name != "Alpha CLI" || alpha.Bin != "alpha" {
		t.Errorf("alpha-cli name/bin = %q/%q", alpha.Name, alpha.Bin)
	}
	if alpha.Description != "Synthetic Anthropic-backed agent." {
		t.Errorf("alpha-cli description = %q", alpha.Description)
	}
	if alpha.Config != (PathPair{Global: "~/.alpha", Local: ".alpha"}) {
		t.Errorf("alpha-cli config = %+v", alpha.Config)
	}
	if alpha.Skills == nil || *alpha.Skills != (PathPair{Global: "~/.alpha/skills", Local: ".alpha/skills"}) {
		t.Errorf("alpha-cli skills = %+v", alpha.Skills)
	}
	if alpha.Version == nil || alpha.Version.Pattern != "v([0-9.]+)" || len(alpha.Version.Args) != 1 {
		t.Errorf("alpha-cli version = %+v", alpha.Version)
	}
	if len(alpha.Provider) != 1 || alpha.Provider[0] != "anthropic" {
		t.Errorf("alpha-cli provider = %v", alpha.Provider)
	}

	// beta-tool: optional skills/local-config absent, single non-anthropic provider.
	beta := cat.Agents["beta-tool"]
	if beta.Skills != nil {
		t.Errorf("beta-tool skills = %+v, want nil", beta.Skills)
	}
	if beta.Config.Local != "" {
		t.Errorf("beta-tool local config = %q, want empty", beta.Config.Local)
	}
	if beta.Version == nil || beta.Version.Pattern != "" {
		t.Errorf("beta-tool version = %+v", beta.Version)
	}

	// gamma-agent: multiple providers, no version probe.
	gamma := cat.Agents["gamma-agent"]
	if gamma.Version != nil {
		t.Errorf("gamma-agent version = %+v, want nil", gamma.Version)
	}
	if len(gamma.Provider) != 2 || gamma.Provider[0] != "google" || gamma.Provider[1] != "openai" {
		t.Errorf("gamma-agent providers = %v", gamma.Provider)
	}
}

func TestLoadCatalogUnavailableMapsToSentinel(t *testing.T) {
	stub := &catalogtest.StubRegistry{
		OnResolve: func(context.Context, string) (string, error) {
			return "", errors.New("network unreachable")
		},
		OnFetch: func(context.Context, string) (string, error) { return "", nil },
	}
	loader := catalog.New(stub,
		catalog.WithModulePath("github.com/start-cli/agentdex/catalog@v1"),
		catalog.WithCacheDir(t.TempDir()),
	)

	_, _, err := loadCatalog(context.Background(), loader)
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Errorf("error = %v, want ErrCatalogUnavailable", err)
	}
}
