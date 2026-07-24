package catalog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/start-cli/agentdex/internal/catalog"
	"github.com/start-cli/agentdex/internal/catalogtest"
)

// TestProductionRegistryAgainstLocalOCI exercises the real modconfig-backed
// Registry against an in-process OCI registry — the only coverage of the code
// the stub replaces. The fixture catalog module is published to a local
// registry selected via CUE_REGISTRY, and resolve, fetch, the on-disk sourceDir
// contract, and a full loader.Load all run through the production implementation
// with no public registry. It then closes the server to prove the offline
// guarantees the cache design rests on: a within-TTL load is served entirely
// from CUE's content cache with no network, while a first run with no resolved
// version fails with ErrUnavailable.
func TestProductionRegistryAgainstLocalOCI(t *testing.T) {
	_, closeServer := catalogtest.StartRegistry(t)

	prod, err := catalog.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	ctx := context.Background()

	version, err := prod.ResolveLatestVersion(ctx, mainPath)
	if err != nil {
		t.Fatalf("ResolveLatestVersion: %v", err)
	}
	if version != "v1.0.0" {
		t.Errorf("resolved version = %q, want v1.0.0", version)
	}

	canonical := "github.com/start-cli/agentdex/catalog@v1.0.0"
	sourceDir, err := prod.Fetch(ctx, canonical)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, f := range []string{"cue.mod/module.cue", "schema.cue", "agents.cue"} {
		if _, err := os.Stat(filepath.Join(sourceDir, f)); err != nil {
			t.Errorf("fetched sourceDir missing %s: %v", f, err)
		}
	}

	// A first load warms both the resolution cache and CUE's content cache.
	resCacheDir := t.TempDir()
	loader := catalog.New(prod,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(resCacheDir),
	)
	res, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load through production registry: %v", err)
	}
	if res.Version != "v1.0.0" {
		t.Errorf("loaded version = %q, want v1.0.0", res.Version)
	}
	assertFixtureAgents(t, res.Catalog)

	// Take the registry offline. The within-TTL load below must not touch it.
	closeServer()

	offlineRes, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("within-TTL Load with the registry offline: %v", err)
	}
	if offlineRes.Stale {
		t.Error("within-TTL offline load reported stale; the resolution is still fresh")
	}
	assertFixtureAgents(t, offlineRes.Catalog)

	if _, err := prod.ResolveLatestVersion(ctx, mainPath); err == nil {
		t.Error("ResolveLatestVersion succeeded with the registry offline; it requires the network")
	}

	// A first run with no resolved version and no network fails clearly.
	fresh := catalog.New(prod,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
	)
	if _, err := fresh.Load(ctx); !errors.Is(err, catalog.ErrUnavailable) {
		t.Errorf("offline first-run error = %v, want ErrUnavailable", err)
	}
}

func assertFixtureAgents(t *testing.T, cat *catalog.Catalog) {
	t.Helper()
	if len(cat.Agents) != 4 {
		t.Errorf("got %d agents, want 4", len(cat.Agents))
	}
	for id, a := range cat.Agents {
		if a.ID != id {
			t.Errorf("agent %q has ID %q after a real fetch; ID must equal its map key", id, a.ID)
		}
	}
}
