package modelsdev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// smallCatalog builds a minimal but well-formed catalog: one first-party
// provider whose model has a matching agnostic entry carrying benchmarks.
func smallCatalog() Catalog {
	return Catalog{
		Models: map[string]Model{
			"anthropic/claude-x": {ID: "anthropic/claude-x", Benchmarks: []Benchmark{{Name: "SWE-Bench"}}},
		},
		Providers: map[string]Provider{
			"anthropic": {ID: "anthropic", Env: []string{"ANTHROPIC_API_KEY"}, Models: map[string]Model{
				"claude-x": {ID: "claude-x", Limit: Limit{Context: 200000, Output: 64000}},
			}},
		},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

// serveBytes starts a server returning the given body for every request and
// records how many requests it received.
func serveBytes(t *testing.T, body []byte) (url string, requests *atomic.Int64) {
	t.Helper()
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &count
}

func TestCatalogMergesRealData(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "catalog.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	url, _ := serveBytes(t, body)
	c := New(WithURL(url), WithCacheDir(t.TempDir()))

	cat, err := c.Catalog(context.Background())
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}

	// A first-party model receives benchmarks by decomposing its real agnostic id.
	kimi, ok := cat.Providers["moonshotai"].Models["kimi-k2-thinking"]
	if !ok {
		t.Fatal("expected moonshotai/kimi-k2-thinking in fixture")
	}
	if len(kimi.Benchmarks) == 0 {
		t.Error("first-party model did not receive agnostic benchmarks")
	}
	if kimi.ID != "kimi-k2-thinking" {
		t.Errorf("provider Model.ID rewritten: got %q, want short id", kimi.ID)
	}

	// An aggregator model under a path-bearing key has no agnostic id decomposing
	// to it and is returned without agnostic benchmarks.
	agg, ok := cat.Providers["requesty"].Models["xai/grok-4"]
	if !ok {
		t.Fatal("expected aggregator requesty/xai/grok-4 in fixture")
	}
	if len(agg.Benchmarks) != 0 {
		t.Errorf("aggregator model received benchmarks: %+v", agg)
	}
	if agg.ID == "requesty/xai/grok-4" {
		t.Error("aggregator Model.ID is a minted composite")
	}

	// Tiered pricing decodes its nested dimension and threshold.
	gemini := cat.Providers["google"].Models["gemini-2.5-pro"]
	if gemini.Cost == nil || len(gemini.Cost.Tiers) == 0 {
		t.Fatal("expected google/gemini-2.5-pro to carry tiered pricing")
	}
	if tier := gemini.Cost.Tiers[0].Tier; tier.Type != "context" || tier.Size != 200000 {
		t.Errorf("tier dimension not decoded: got %+v", tier)
	}
	// The parallel over-200k pricing block decodes into its nested *Cost.
	if gemini.Cost.ContextOver200K == nil {
		t.Error("expected google/gemini-2.5-pro to carry context_over_200k pricing")
	} else if got := gemini.Cost.ContextOver200K.Input; got != 2.5 {
		t.Errorf("context_over_200k input not decoded: got %v, want 2.5", got)
	}

	// The join rate is partial: some first-party models attach, but far from all
	// provider models do (aggregators carry none).
	var withBench, total int
	for _, p := range cat.Providers {
		for _, m := range p.Models {
			total++
			if len(m.Benchmarks) > 0 {
				withBench++
			}
		}
	}
	if withBench == 0 {
		t.Error("expected some provider models to attach benchmarks")
	}
	if withBench >= total {
		t.Errorf("expected a partial join, got %d/%d", withBench, total)
	}
}

func TestSingleFlightAndMemoisation(t *testing.T) {
	url, requests := serveBytes(t, mustJSON(t, smallCatalog()))
	c := New(WithURL(url), WithCacheDir(t.TempDir()))

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				_, _ = c.Catalog(context.Background())
			case 1:
				_, _, _ = c.Provider(context.Background(), "anthropic")
			default:
				_, _ = c.Models(context.Background(), "anthropic")
			}
		}(i)
	}
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Errorf("expected exactly one upstream fetch, got %d", got)
	}

	// A subsequent call still does not refetch.
	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("memoised Client refetched: %d requests", got)
	}
}

func TestLeaderCancellationDoesNotPoisonWaiters(t *testing.T) {
	// The handler holds every response until release fires, so the first fetch
	// is reliably in flight while the test arranges a waiter and cancels the
	// leader. release is guarded by a Once so the test body and the cleanup can
	// both call it without double-closing.
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var count atomic.Int64
	body := mustJSON(t, smallCatalog())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		<-release
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock) // unblock any handler still parked on an early exit

	c := New(WithURL(srv.URL), WithCacheDir(t.TempDir()))

	// The leader starts the shared fetch and parks in the handler.
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	go func() { _, _ = c.Catalog(leaderCtx) }()
	waitFor(t, func() bool { return count.Load() == 1 }, "leader fetch to reach the server")

	// A second caller with a live context joins as a waiter on the same fetch.
	type result struct {
		cat *Catalog
		err error
	}
	waiter := make(chan result, 1)
	go func() {
		cat, err := c.Catalog(context.Background())
		waiter <- result{cat, err}
	}()

	// Cancel the leader while the shared fetch is still parked, then let the
	// detached fetch complete. The waiter's own context never cancelled.
	cancelLeader()
	unblock()

	got := <-waiter
	if got.err != nil {
		t.Fatalf("live-context waiter inherited the leader's cancellation: %v", got.err)
	}
	if got.cat == nil || got.cat.Providers["anthropic"].ID != "anthropic" {
		t.Errorf("waiter did not receive the merged catalog: %+v", got.cat)
	}
	// The leader's cancellation must not have spawned a second upstream fetch.
	if n := count.Load(); n != 1 {
		t.Errorf("expected one shared fetch despite leader cancellation, got %d", n)
	}
}

// waitFor polls cond until it holds or a short deadline elapses, failing the test
// on timeout. It keeps timing-dependent tests from hanging the suite.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCacheWithinTTLReadsFile(t *testing.T) {
	dir := t.TempDir()
	url, requests := serveBytes(t, mustJSON(t, smallCatalog()))

	if _, err := New(WithURL(url), WithCacheDir(dir)).Catalog(context.Background()); err != nil {
		t.Fatalf("first Catalog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
		t.Fatalf("fresh fetch did not write cache file: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected one fetch, got %d", got)
	}

	// A freshly constructed Client over the same cache dir, within TTL, reads the
	// file and does not hit the network.
	if _, err := New(WithURL(url), WithCacheDir(dir), WithTTL(time.Hour)).Catalog(context.Background()); err != nil {
		t.Fatalf("second Catalog: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("within-TTL call refetched: %d requests", got)
	}
}

func TestCacheExpiryRefetches(t *testing.T) {
	dir := t.TempDir()
	url, requests := serveBytes(t, mustJSON(t, smallCatalog()))
	if _, err := New(WithURL(url), WithCacheDir(dir)).Catalog(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// A clock advanced past the TTL makes the otherwise-fresh file expired, so a
	// reachable endpoint is refetched rather than read from cache.
	c := New(WithURL(url), WithCacheDir(dir), WithTTL(time.Minute))
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatalf("Catalog after expiry: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("expected a refetch after TTL expiry, got %d requests", got)
	}
}

func TestStaleServedOnFetchFailure(t *testing.T) {
	dir := t.TempDir()
	good, _ := serveBytes(t, mustJSON(t, smallCatalog()))
	if _, err := New(WithURL(good), WithCacheDir(dir)).Catalog(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// TTL zero forces a refetch attempt; the failing endpoint makes it fall back
	// to the stale file. The endpoint counts attempts so we can assert the
	// stale-served result is memoised and not re-fetched.
	var attempts atomic.Int64
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	c := New(WithURL(failing.URL), WithCacheDir(dir), WithTTL(0))
	cat, err := c.Catalog(context.Background())
	if err != nil {
		t.Fatalf("expected stale copy served, got error: %v", err)
	}
	if _, ok := cat.Providers["anthropic"]; !ok {
		t.Error("stale catalog missing expected provider")
	}

	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatalf("second Catalog: %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("stale-served result re-fetched: %d upstream attempts", got)
	}
}

func TestForceRefreshFailsRatherThanServeStale(t *testing.T) {
	// The honest counterpart to TestStaleServedOnFetchFailure: with WithForceRefresh
	// a fetch failure is reported even when a cache exists, so an explicit refresh
	// learns the network was unreachable instead of silently serving stale bytes.
	dir := t.TempDir()
	good, _ := serveBytes(t, mustJSON(t, smallCatalog()))
	if _, err := New(WithURL(good), WithCacheDir(dir)).Catalog(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	c := New(WithURL(failing.URL), WithCacheDir(dir), WithForceRefresh())
	if _, err := c.Catalog(context.Background()); err == nil {
		t.Fatal("force refresh must report the fetch failure, not serve the stale cache")
	}
}

func TestForceRefreshUpdatesCacheOnSuccess(t *testing.T) {
	// A successful force refresh writes the fetched bytes to the cache, so a later
	// ordinary client serves the refreshed data offline.
	dir := t.TempDir()
	url, _ := serveBytes(t, mustJSON(t, smallCatalog()))
	if _, err := New(WithURL(url), WithCacheDir(dir), WithForceRefresh()).Catalog(context.Background()); err != nil {
		t.Fatalf("force refresh: %v", err)
	}

	offline := New(WithURL("http://127.0.0.1:0"), WithCacheDir(dir), WithTTL(time.Hour))
	cat, err := offline.Catalog(context.Background())
	if err != nil {
		t.Fatalf("expected cached data served offline: %v", err)
	}
	if _, ok := cat.Providers["anthropic"]; !ok {
		t.Error("refreshed cache missing expected provider")
	}
}

func TestCorruptCacheNotServedAsStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cacheFileName), []byte("{not catalog json"), 0o644); err != nil {
		t.Fatalf("seed corrupt cache: %v", err)
	}

	// A within-TTL but corrupt cache is unusable as either fresh or stale. With a
	// failing endpoint the only fallback would be the corrupt file, which must not
	// be served; the fetch error surfaces instead.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	if _, err := New(WithURL(failing.URL), WithCacheDir(dir), WithTTL(time.Hour)).Catalog(context.Background()); err == nil {
		t.Fatal("expected error: a corrupt cache must not be served as stale")
	}

	// A reachable endpoint overwrites the corrupt file and returns a fresh catalog.
	good, _ := serveBytes(t, mustJSON(t, smallCatalog()))
	cat, err := New(WithURL(good), WithCacheDir(dir), WithTTL(time.Hour)).Catalog(context.Background())
	if err != nil {
		t.Fatalf("fetch over corrupt cache: %v", err)
	}
	if _, ok := cat.Providers["anthropic"]; !ok {
		t.Error("expected a fresh catalog after the corrupt cache was replaced")
	}
}

func TestFirstFetchFailureIsRetryable(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var count atomic.Int64
	body := mustJSON(t, smallCatalog())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithURL(srv.URL), WithCacheDir(t.TempDir()))

	if _, err := c.Catalog(context.Background()); err == nil {
		t.Fatal("expected first fetch to fail")
	}

	fail.Store(false)
	cat, err := c.Catalog(context.Background())
	if err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if _, ok := cat.Providers["anthropic"]; !ok {
		t.Error("retried catalog missing expected provider")
	}
	if got := count.Load(); got != 2 {
		t.Errorf("expected the failure not to be memoised (2 fetches), got %d", got)
	}
}

func TestTopLevelSchemaError(t *testing.T) {
	empty := mustJSON(t, Catalog{Models: map[string]Model{}, Providers: map[string]Provider{}})

	t.Run("no cache surfaces error", func(t *testing.T) {
		url, _ := serveBytes(t, empty)
		c := New(WithURL(url), WithCacheDir(t.TempDir()))
		_, err := c.Catalog(context.Background())
		if !errors.Is(err, ErrModelsSchema) {
			t.Errorf("got %v, want ErrModelsSchema", err)
		}
	})

	t.Run("serves stale when cache present", func(t *testing.T) {
		dir := t.TempDir()
		good, _ := serveBytes(t, mustJSON(t, smallCatalog()))
		if _, err := New(WithURL(good), WithCacheDir(dir)).Catalog(context.Background()); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		url, _ := serveBytes(t, empty)
		cat, err := New(WithURL(url), WithCacheDir(dir), WithTTL(0)).Catalog(context.Background())
		if err != nil {
			t.Fatalf("expected stale served, got %v", err)
		}
		if _, ok := cat.Providers["anthropic"]; !ok {
			t.Error("stale catalog missing expected provider")
		}
	})
}

func TestPerRequestedProviderSchemaError(t *testing.T) {
	// A malformed model (zero Limit) sits in "broken"; "anthropic" is clean.
	cat := smallCatalog()
	cat.Providers["broken"] = Provider{ID: "broken", Models: map[string]Model{
		"bad": {ID: "bad"}, // zero Limit
	}}
	url, _ := serveBytes(t, mustJSON(t, cat))
	c := New(WithURL(url), WithCacheDir(t.TempDir()))

	// Catalog and an unrequested-provider request are unaffected.
	if _, err := c.Catalog(context.Background()); err != nil {
		t.Fatalf("Catalog must not validate per-model: %v", err)
	}
	if _, ok, err := c.Provider(context.Background(), "anthropic"); err != nil || !ok {
		t.Fatalf("clean provider: ok=%v err=%v", ok, err)
	}
	if _, err := c.Models(context.Background(), "anthropic"); err != nil {
		t.Fatalf("clean provider models: %v", err)
	}

	// Requesting the broken provider raises ErrModelsSchema from the accessor, and
	// still reports found=true: existence is independent of the schema error, so a
	// caller branching on found alone cannot mistake drift for an absent provider.
	if p, ok, err := c.Provider(context.Background(), "broken"); !errors.Is(err, ErrModelsSchema) {
		t.Errorf("Provider(broken): got %v, want ErrModelsSchema", err)
	} else if !ok || p.ID != "broken" {
		t.Errorf("Provider(broken): got found=%v id=%q, want found=true id=broken", ok, p.ID)
	}
	if _, err := c.Models(context.Background(), "broken"); !errors.Is(err, ErrModelsSchema) {
		t.Errorf("Models(broken): got %v, want ErrModelsSchema", err)
	}
}

func TestModelsDedupsRepeatedProviders(t *testing.T) {
	url, _ := serveBytes(t, mustJSON(t, smallCatalog()))
	c := New(WithURL(url), WithCacheDir(t.TempDir()))

	once, err := c.Models(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(once) == 0 {
		t.Fatal("expected anthropic to contribute at least one model")
	}
	twice, err := c.Models(context.Background(), "anthropic", "anthropic")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(twice) != len(once) {
		t.Errorf("repeated provider id duplicated models: got %d, want %d", len(twice), len(once))
	}
}

func TestHTTPClientTimeoutDefaultAndOverride(t *testing.T) {
	if got := New().httpClient.Timeout; got != DefaultHTTPTimeout {
		t.Errorf("default client timeout: got %v, want %v", got, DefaultHTTPTimeout)
	}
	custom := &http.Client{Timeout: 5 * time.Second}
	if got := New(WithHTTPClient(custom)).httpClient; got != custom {
		t.Errorf("WithHTTPClient did not override the client: got %p, want %p", got, custom)
	}
}

func TestProviderNotFound(t *testing.T) {
	url, _ := serveBytes(t, mustJSON(t, smallCatalog()))
	c := New(WithURL(url), WithCacheDir(t.TempDir()))
	p, ok, err := c.Provider(context.Background(), "nonexistent")
	if err != nil || ok {
		t.Errorf("unknown provider: got ok=%v err=%v, want false,nil", ok, err)
	}
	if p.ID != "" {
		t.Errorf("unknown provider returned non-zero value: %+v", p)
	}
}
