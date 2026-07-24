package agentdex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/start-cli/agentdex/internal/catalogtest"
	"github.com/start-cli/agentdex/internal/modelsdevtest"
	"github.com/start-cli/agentdex/modelsdev"
)

// openRegistry opens an Index backed by the in-process OCI registry, sharing one
// resolution-cache directory so a second Index can reuse a prior resolution. It
// returns the Index, the resolution-cache dir, and the registry closer so a test
// can take the registry offline mid-run to reach the stale and cold-offline paths.
func openRegistry(t *testing.T, resCache string, opts ...Option) (*Index, func()) {
	t.Helper()
	_, closeReg := catalogtest.StartRegistry(t)
	base := []Option{WithCacheDir(resCache), WithModelsURL(modelsdevtest.MustNotFetch(t))}
	idx, err := Open(context.Background(), append(base, opts...)...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return idx, closeReg
}

// modelsCatalogJSON marshals a models.dev catalog.json exposing the named providers,
// with one canonical id so the top-level shape validates.
func modelsCatalogJSON(t *testing.T, present ...string) []byte {
	t.Helper()
	cat := modelsdev.Catalog{
		Models: map[string]modelsdev.Model{
			"anthropic/claude-sonnet": {ID: "anthropic/claude-sonnet", Name: "Claude Sonnet", Limit: modelsdev.Limit{Context: 200000}},
		},
		Providers: map[string]modelsdev.Provider{},
	}
	for _, pid := range present {
		cat.Providers[pid] = modelsdevtest.Provider(pid, false)
	}
	data, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("marshal models catalog: %v", err)
	}
	return data
}

// mutableModelsServer serves a models.dev catalog.json whose provider set the test
// can swap at runtime, so a refresh can be proved to serve fresh data through the
// same Index. set must be called before the first fetch.
func mutableModelsServer(t *testing.T) (url string, set func(present ...string)) {
	t.Helper()
	var mu sync.Mutex
	var data []byte
	set = func(present ...string) {
		b := modelsCatalogJSON(t, present...)
		mu.Lock()
		data = b
		mu.Unlock()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		b := data
		mu.Unlock()
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, set
}

func TestCatalogStaleColdOfflineIsErrCatalogUnavailable(t *testing.T) {
	// Registry up only long enough to point CUE at it, then offline with no prior
	// resolution: the first catalog-touching call must fail clearly (R2, R12).
	idx, closeReg := openRegistry(t, t.TempDir())
	closeReg()

	_, err := idx.CatalogStale(context.Background())
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("CatalogStale cold-offline error = %v, want ErrCatalogUnavailable", err)
	}
}

func TestCatalogStaleFreshThenStaleWithWarningInjection(t *testing.T) {
	ctx := context.Background()
	resCache := t.TempDir()

	// Warm the resolution and content caches through one Index while the registry
	// is reachable; the resolution is fresh, so no stale warning.
	warm, closeReg := openRegistry(t, resCache)
	stale, err := warm.CatalogStale(ctx)
	if err != nil {
		t.Fatalf("warm CatalogStale: %v", err)
	}
	if stale {
		t.Fatal("freshly resolved catalog reported stale")
	}
	res, err := warm.Agents.List(ctx, AgentQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("warm List: %v", err)
	}
	if hasWarning(res.Warnings, WarnStaleCatalog) {
		t.Error("fresh catalog listing carried a stale warning")
	}

	// Take the registry offline, then force re-resolution through a second Index over
	// the same resolution cache: re-resolution fails, the last resolved version is
	// reused, and the catalog is stale.
	closeReg()
	idx, err := Open(ctx,
		WithCacheDir(resCache),
		WithCatalogTTL(0),
		WithModelsURL(modelsdevtest.MustNotFetch(t)),
	)
	if err != nil {
		t.Fatalf("Open stale index: %v", err)
	}

	staleRes, err := idx.Agents.List(ctx, AgentQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("stale List: %v", err)
	}
	msg, ok := warningMsg(staleRes.Warnings, WarnStaleCatalog)
	if !ok {
		t.Fatalf("stale listing missing WarnStaleCatalog, got %v", staleRes.Warnings)
	}
	const want = "agentdex catalog is stale: re-resolution failed, using the last resolved version"
	if msg != want {
		t.Errorf("stale warning = %q, want %q", msg, want)
	}
	if s, err := idx.CatalogStale(ctx); err != nil || !s {
		t.Errorf("CatalogStale = (%v, %v), want (true, nil)", s, err)
	}

	// The warning rides the error return too: an unknown agent id under a stale
	// catalog still carries it (R6).
	d, err := idx.Agents.Get(ctx, "no-such-agent", AgentGetQuery{Enrich: EnrichNone})
	if !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("Get unknown error = %v, want ErrAgentUnknown", err)
	}
	if !hasWarning(d.Warnings, WarnStaleCatalog) {
		t.Errorf("error return dropped the stale warning, got %v", d.Warnings)
	}

	// And on Models.List scoped to an agnostic agent with no providers (R6).
	mres, err := idx.Models.List(ctx, ModelQuery{Scope: ModelScope{Agent: "delta-agent"}})
	if !errors.Is(err, ErrProvidersRequired) {
		t.Fatalf("Models.List agnostic error = %v, want ErrProvidersRequired", err)
	}
	if !hasWarning(mres.Warnings, WarnStaleCatalog) {
		t.Errorf("Models.List error dropped the stale warning, got %v", mres.Warnings)
	}
}

func TestRefreshCatalogSuccess(t *testing.T) {
	ctx := context.Background()
	idx, _ := openRegistry(t, t.TempDir())

	refreshed, err := idx.Refresh(ctx, TargetCatalog)
	if err != nil {
		t.Fatalf("Refresh catalog: %v", err)
	}
	if !refreshed.Catalog {
		t.Error("Refreshed.Catalog = false, want true")
	}
	if refreshed.Models {
		t.Error("Refreshed.Models = true on a catalog-only refresh")
	}
	if s, err := idx.CatalogStale(ctx); err != nil || s {
		t.Errorf("CatalogStale after a successful refresh = (%v, %v), want (false, nil)", s, err)
	}
}

func TestRefreshCatalogStaleFallbackIsErrorAndKeepsState(t *testing.T) {
	ctx := context.Background()
	resCache := t.TempDir()
	idx, closeReg := openRegistry(t, resCache)

	// Warm so a prior resolution exists to fall back on.
	before, err := idx.Agents.List(ctx, AgentQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("warm List: %v", err)
	}

	closeReg()
	refreshed, err := idx.Refresh(ctx, TargetCatalog)
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("Refresh stale error = %v, want ErrCatalogUnavailable", err)
	}
	if refreshed.Catalog {
		t.Error("Refreshed.Catalog = true on a stale-fallback refresh")
	}

	// A failed refresh leaves the index serving what it served before, without a
	// stale warning, because the prior state was untouched (R13).
	after, err := idx.Agents.List(ctx, AgentQuery{Enrich: EnrichNone})
	if err != nil {
		t.Fatalf("post-failure List: %v", err)
	}
	if len(after.Items) != len(before.Items) {
		t.Errorf("post-failure listing has %d agents, want %d", len(after.Items), len(before.Items))
	}
	if hasWarning(after.Warnings, WarnStaleCatalog) {
		t.Error("a failed refresh left the index reporting stale")
	}
}

func TestRefreshModelsServesFreshDataThroughSameIndex(t *testing.T) {
	ctx := context.Background()
	url, set := mutableModelsServer(t)
	set("anthropic")

	dir := catalogtest.WriteModule(t, testCatalog)
	idx, err := Open(ctx, WithCatalogDir(dir), WithCacheDir(t.TempDir()), WithModelsURL(url))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	first, err := idx.Providers.List(ctx, ProviderQuery{})
	if err != nil {
		t.Fatalf("first Providers.List: %v", err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != "anthropic" {
		t.Fatalf("first listing = %v, want [anthropic]", providerIDs(first.Items))
	}

	// Swap the source. The memoised client must keep serving the old set until a
	// refresh installs a fresh client — this is the trap R13 exists to close.
	set("anthropic", "openai")
	stillOld, err := idx.Providers.List(ctx, ProviderQuery{})
	if err != nil {
		t.Fatalf("pre-refresh Providers.List: %v", err)
	}
	if len(stillOld.Items) != 1 {
		t.Fatalf("pre-refresh listing = %v, want the memoised [anthropic]", providerIDs(stillOld.Items))
	}

	refreshed, err := idx.Refresh(ctx, TargetModels)
	if err != nil {
		t.Fatalf("Refresh models: %v", err)
	}
	if !refreshed.Models || refreshed.Catalog {
		t.Errorf("Refreshed = %+v, want {Catalog:false Models:true}", refreshed)
	}

	fresh, err := idx.Providers.List(ctx, ProviderQuery{})
	if err != nil {
		t.Fatalf("post-refresh Providers.List: %v", err)
	}
	if len(fresh.Items) != 2 {
		t.Errorf("post-refresh listing = %v, want [anthropic openai]", providerIDs(fresh.Items))
	}
}

func TestRefreshModelsUnreachableIsError(t *testing.T) {
	ctx := context.Background()
	dir := catalogtest.WriteModule(t, testCatalog)
	idx, err := Open(ctx, WithCatalogDir(dir), WithCacheDir(t.TempDir()), WithModelsURL(modelsdevtest.Closed(t)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := idx.Refresh(ctx, TargetModels); !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("Refresh unreachable error = %v, want ErrModelsUnavailable", err)
	}
}

func TestRefreshModelsSchemaDriftIsError(t *testing.T) {
	ctx := context.Background()
	// A reachable models.dev serving gross drift (empty top-level maps): a data
	// fault, not an outage (R13).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":{},"providers":{}}`))
	}))
	t.Cleanup(srv.Close)

	dir := catalogtest.WriteModule(t, testCatalog)
	idx, err := Open(ctx, WithCatalogDir(dir), WithCacheDir(t.TempDir()), WithModelsURL(srv.URL))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := idx.Refresh(ctx, TargetModels); !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Fatalf("Refresh drift error = %v, want modelsdev.ErrModelsSchema", err)
	}
}

func TestRefreshDirectoryCatalogNotRefreshed(t *testing.T) {
	ctx := context.Background()
	url, set := mutableModelsServer(t)
	set("anthropic")

	dir := catalogtest.WriteModule(t, testCatalog)
	idx, err := Open(ctx, WithCatalogDir(dir), WithCacheDir(t.TempDir()), WithModelsURL(url))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	refreshed, err := idx.Refresh(ctx, TargetCatalog)
	if err != nil {
		t.Fatalf("Refresh directory catalog: %v", err)
	}
	if refreshed.Catalog {
		t.Error("a directory catalog reported as refreshed; it has no version to re-resolve")
	}

	// TargetAll over a directory catalog still refreshes models.dev.
	all, err := idx.Refresh(ctx, TargetAll)
	if err != nil {
		t.Fatalf("Refresh all over directory catalog: %v", err)
	}
	if all.Catalog {
		t.Error("Refreshed.Catalog = true for a directory catalog under TargetAll")
	}
	if !all.Models {
		t.Error("Refreshed.Models = false under TargetAll over a directory catalog")
	}
}

func TestRefreshAllStopsAtFirstCatalogFailure(t *testing.T) {
	ctx := context.Background()
	resCache := t.TempDir()
	// MustNotFetch models.dev proves models is never attempted once the catalog
	// target fails (R13).
	idx, closeReg := openRegistry(t, resCache)
	if _, err := idx.CatalogStale(ctx); err != nil {
		t.Fatalf("warm CatalogStale: %v", err)
	}
	closeReg()

	refreshed, err := idx.Refresh(ctx, TargetAll)
	if !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("Refresh all error = %v, want ErrCatalogUnavailable", err)
	}
	if refreshed.Catalog || refreshed.Models {
		t.Errorf("Refreshed = %+v, want both false when the catalog target fails first", refreshed)
	}
}

func TestIndexConcurrentUseUnderRace(t *testing.T) {
	ctx := context.Background()
	srv := modelsdevtest.Server(t, []string{"anthropic", "openai", "google"})
	idx, _ := openRegistry(t, t.TempDir(), WithModelsURL(srv.URL))

	var wg sync.WaitGroup
	work := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				t.Errorf("concurrent op: %v", err)
			}
		}()
	}

	for i := 0; i < 8; i++ {
		work(func() error {
			_, err := idx.Agents.List(ctx, AgentQuery{Enrich: EnrichCount})
			return err
		})
		work(func() error {
			_, err := idx.Providers.List(ctx, ProviderQuery{})
			return err
		})
		work(func() error {
			_, err := idx.Models.List(ctx, ModelQuery{})
			return err
		})
	}
	// Refreshes land mid-flight, replacing the state other goroutines read.
	work(func() error {
		_, err := idx.Refresh(ctx, TargetModels)
		return err
	})
	work(func() error {
		_, err := idx.Refresh(ctx, TargetCatalog)
		return err
	})
	wg.Wait()
}
