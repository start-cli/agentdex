package catalog_test

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modregistrytest"

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
	reg, closeServer := startLocalRegistry(t)

	t.Setenv("CUE_REGISTRY", reg.Host()+"+insecure")
	t.Setenv("CUE_CACHE_DIR", cueCacheDir(t))

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

// startLocalRegistry publishes the catalog-valid fixture to an in-process OCI
// registry and returns it with a once-guarded close so the test can take it
// offline mid-run without a double-close at cleanup. modregistrytest expects
// each module under a directory named path_version, with slashes in the
// (major-stripped) module path replaced by underscores.
func startLocalRegistry(t *testing.T) (*modregistrytest.Registry, func()) {
	t.Helper()
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	const moduleDir = "github.com_start-cli_agentdex_catalog_v1.0.0"

	fsys := fstest.MapFS{}
	for _, rel := range []string{"cue.mod/module.cue", "schema.cue", "agents.cue"} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("read fixture %s: %v", rel, err)
		}
		fsys[path.Join(moduleDir, rel)] = &fstest.MapFile{Data: data}
	}

	reg, err := modregistrytest.New(fsys, "")
	if err != nil {
		t.Fatalf("start local registry: %v", err)
	}
	var once sync.Once
	closeServer := func() { once.Do(reg.Close) }
	t.Cleanup(closeServer)
	return reg, closeServer
}

// cueCacheDir returns a fresh CUE content-cache directory. CUE writes extracted
// modules read-only, so t.TempDir's RemoveAll cannot unlink them; modcache.RemoveAll
// handles the read-only tree.
func cueCacheDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "agentdex-cue-cache")
	if err != nil {
		t.Fatalf("create cue cache dir: %v", err)
	}
	t.Cleanup(func() { _ = modcache.RemoveAll(dir) })
	return dir
}
