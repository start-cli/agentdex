package catalog_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/start-cli/agentdex/internal/catalog"
	"github.com/start-cli/agentdex/internal/catalogtest"
)

const (
	mainPath = "github.com/start-cli/agentdex/catalog@v1"
	forkPath = "example.com/fork/catalog@v2"
)

// fakeClock is a settable clock so TTL behaviour is driven from inputs.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestLoadValidFixture(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	stub := catalogtest.Serve("v1.0.0", dir)
	loader := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
	)

	res, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res.Version != "v1.0.0" {
		t.Errorf("Version = %q, want v1.0.0", res.Version)
	}
	if res.Stale {
		t.Error("Stale = true, want false on a fresh resolve")
	}

	got := res.Catalog.Agents
	for id, a := range got {
		if a.ID != id {
			t.Errorf("agent %q has ID %q; ID must equal its map key", id, a.ID)
		}
	}
	for _, want := range []string{"alpha-cli", "beta-tool", "gamma-agent"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing agent %q in %v", want, keys(got))
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d agents, want 3", len(got))
	}

	// Spot-check optional-field decoding across entries.
	if got["beta-tool"].Skills != nil {
		t.Error("beta-tool should have no skills")
	}
	if got["beta-tool"].Config.Local != "" {
		t.Errorf("beta-tool local config = %q, want empty", got["beta-tool"].Config.Local)
	}
	if got["gamma-agent"].Version != nil {
		t.Error("gamma-agent should have no version probe")
	}
	if want := []string{"google", "openai"}; !equal(got["gamma-agent"].Provider, want) {
		t.Errorf("gamma-agent providers = %v, want %v", got["gamma-agent"].Provider, want)
	}
}

func TestLoadSchemaViolationFails(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-invalid")
	stub := catalogtest.Serve("v1.0.0", dir)
	loader := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
	)

	res, err := loader.Load(context.Background())
	if err == nil {
		t.Fatalf("Load succeeded on schema-violating fixture; got %+v", res)
	}
	if !errors.Is(err, catalog.ErrInvalidCatalog) {
		t.Errorf("error = %v, want ErrInvalidCatalog", err)
	}
	if res != nil {
		t.Errorf("expected no partial catalog, got %+v", res)
	}
}

func TestWithinTTLUsesCacheWithoutResolving(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	stub := catalogtest.Serve("v1.0.0", dir)
	loader := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
		catalog.WithTTL(24*time.Hour),
		catalog.WithClock(clock.Now),
	)

	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if n := stub.ResolveCalls(mainPath); n != 1 {
		t.Fatalf("after first load, resolve calls = %d, want 1", n)
	}

	clock.advance(12 * time.Hour) // still within TTL
	res, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if n := stub.ResolveCalls(mainPath); n != 1 {
		t.Errorf("within-TTL load re-resolved: resolve calls = %d, want 1", n)
	}
	if res.Stale {
		t.Error("within-TTL load reported stale")
	}
}

func TestStaleResolveKeepsLastResolved(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	stub := catalogtest.Serve("v1.0.0", dir)
	loader := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
		catalog.WithTTL(24*time.Hour),
		catalog.WithClock(clock.Now),
	)

	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("first Load: %v", err)
	}

	// TTL expires and re-resolution now fails (network down).
	clock.advance(25 * time.Hour)
	stub.OnResolve = func(context.Context, string) (string, error) {
		return "", errors.New("network unreachable")
	}

	res, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("expected keep-last-resolved, got error: %v", err)
	}
	if res.Version != "v1.0.0" {
		t.Errorf("Version = %q, want last resolved v1.0.0", res.Version)
	}
	if !res.Stale {
		t.Error("Stale = false, want true after failed re-resolution")
	}
	if n := stub.ResolveCalls(mainPath); n != 2 {
		t.Errorf("resolve calls = %d, want 2 (one fresh, one failed re-resolve)", n)
	}
}

func TestRefreshedResolveUpdatesCache(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	stub := catalogtest.Serve("v1.0.0", dir)
	loader := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
		catalog.WithTTL(24*time.Hour),
		catalog.WithClock(clock.Now),
	)

	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("first Load: %v", err)
	}

	clock.advance(25 * time.Hour)
	stub.OnResolve = func(context.Context, string) (string, error) { return "v1.1.0", nil }

	res, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("re-resolve Load: %v", err)
	}
	if res.Version != "v1.1.0" {
		t.Errorf("Version = %q, want v1.1.0 after refresh", res.Version)
	}
	if res.Stale {
		t.Error("Stale = true on a successful re-resolve")
	}

	// The refreshed version is now cached: a subsequent within-TTL load reuses it.
	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("post-refresh Load: %v", err)
	}
	if n := stub.ResolveCalls(mainPath); n != 2 {
		t.Errorf("resolve calls = %d, want 2 (cache should serve the refreshed version)", n)
	}
}

func TestFirstRunOfflineFails(t *testing.T) {
	stub := &catalogtest.StubRegistry{
		OnResolve: func(context.Context, string) (string, error) {
			return "", errors.New("network unreachable")
		},
		OnFetch: func(context.Context, string) (string, error) {
			t.Error("Fetch must not be reached when resolution fails on a first run")
			return "", nil
		},
	}
	loader := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(t.TempDir()),
	)

	_, err := loader.Load(context.Background())
	if !errors.Is(err, catalog.ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable", err)
	}
}

func TestResolutionsAreIndependentPerModulePath(t *testing.T) {
	dir := catalogtest.FixtureDir(t, "catalog-valid")
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	stub := &catalogtest.StubRegistry{
		OnResolve: func(_ context.Context, modulePath string) (string, error) {
			switch modulePath {
			case mainPath:
				return "v1.0.0", nil
			case forkPath:
				return "v2.3.0", nil
			default:
				return "", errors.New("unexpected module path: " + modulePath)
			}
		},
		OnFetch: func(context.Context, string) (string, error) { return dir, nil },
	}
	cacheDir := t.TempDir()

	main := catalog.New(stub,
		catalog.WithModulePath(mainPath),
		catalog.WithCacheDir(cacheDir),
		catalog.WithClock(clock.Now),
	)
	fork := catalog.New(stub,
		catalog.WithModulePath(forkPath),
		catalog.WithCacheDir(cacheDir),
		catalog.WithClock(clock.Now),
	)

	mainRes, err := main.Load(context.Background())
	if err != nil {
		t.Fatalf("main Load: %v", err)
	}
	forkRes, err := fork.Load(context.Background())
	if err != nil {
		t.Fatalf("fork Load: %v", err)
	}

	if mainRes.Version != "v1.0.0" {
		t.Errorf("main version = %q, want v1.0.0", mainRes.Version)
	}
	if forkRes.Version != "v2.3.0" {
		t.Errorf("fork version = %q, want v2.3.0 (fork must not be served main's resolution)", forkRes.Version)
	}

	// Reloading main within TTL must still see v1.0.0 from its own cache entry,
	// untouched by the fork resolution, and must not re-resolve.
	reMain, err := main.Load(context.Background())
	if err != nil {
		t.Fatalf("main reload: %v", err)
	}
	if reMain.Version != "v1.0.0" {
		t.Errorf("main reload version = %q, want v1.0.0", reMain.Version)
	}
	if n := stub.ResolveCalls(mainPath); n != 1 {
		t.Errorf("main resolve calls = %d, want 1 (second load served from cache)", n)
	}
	if n := stub.ResolveCalls(forkPath); n != 1 {
		t.Errorf("fork resolve calls = %d, want 1", n)
	}
}

func keys(m map[string]catalog.KnownAgent) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
