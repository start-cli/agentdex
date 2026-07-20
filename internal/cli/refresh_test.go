package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshAll(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "refresh", "all")
	if got.code != codeOK {
		t.Fatalf("refresh all exit = %d, stderr=%q", got.code, got.stderr)
	}
	refreshed := got.envelope(t).Data.(map[string]any)["refreshed"].([]any)
	if len(refreshed) != 2 {
		t.Errorf("refresh all should refresh catalog and models: %v", refreshed)
	}
}

func TestRefreshModelsTransientWithoutNetwork(t *testing.T) {
	newScenario(t, closedModelsServer(t), "alpha-cli")

	got := runCLI("refresh", "models.dev")
	if got.code != codeTransient {
		t.Fatalf("refresh models offline exit = %d, want 75; stderr=%q", got.code, got.stderr)
	}
}

func TestRefreshModelsWarmCacheOfflineIsTransient(t *testing.T) {
	// The case the zero-TTL approach missed: a populated cache plus an unreachable
	// network. A forced refresh must report failure rather than silently serving
	// the warm cache and claiming success.
	data := modelsCatalog([]string{"anthropic"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	newScenario(t, srv.URL, "alpha-cli")

	if got := runCLI("refresh", "models.dev"); got.code != codeOK {
		t.Fatalf("warm refresh exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}

	srv.Close() // go offline; the cache written above is now stale
	if got := runCLI("refresh", "models.dev"); got.code != codeTransient {
		t.Fatalf("offline refresh with warm cache exit = %d, want 75; stderr=%q", got.code, got.stderr)
	}
}

func TestRefreshCatalogWarmCacheOfflineIsTransient(t *testing.T) {
	// The catalog counterpart to the models honesty fix: once the version is
	// resolved, a forced refresh that can no longer reach the registry must report
	// transient rather than silently reusing the last resolved version.
	s := newScenario(t, "", "alpha-cli")

	if got := runCLI("refresh", "catalog"); got.code != codeOK {
		t.Fatalf("warm catalog refresh exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}

	s.closeRegistry() // re-resolution can no longer reach the registry
	if got := runCLI("refresh", "catalog"); got.code != codeTransient {
		t.Fatalf("offline catalog refresh exit = %d, want 75; stderr=%q", got.code, got.stderr)
	}
}

func TestRefreshDefaultTargetIsAll(t *testing.T) {
	// With no target argument refresh defaults to "all", refreshing both caches.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	got := runCLI("--json", "refresh")
	if got.code != codeOK {
		t.Fatalf("refresh (default) exit = %d, stderr=%q", got.code, got.stderr)
	}
	refreshed := got.envelope(t).Data.(map[string]any)["refreshed"].([]any)
	if len(refreshed) != 2 {
		t.Errorf("default refresh should refresh catalog and models: %v", refreshed)
	}
}

func TestRefreshUnknownTarget(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("refresh", "bogus")
	if got.code != codeUsage {
		t.Fatalf("refresh bad target exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
}

func TestRefreshCatalog(t *testing.T) {
	newScenario(t, "", "alpha-cli")

	got := runCLI("refresh", "catalog")
	if got.code != codeOK {
		t.Fatalf("refresh catalog exit = %d, stderr=%q", got.code, got.stderr)
	}
}
