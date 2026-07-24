package agentdex

import (
	"context"
	"errors"
	"testing"

	"github.com/start-cli/agentdex/internal/modelsdevtest"
	"github.com/start-cli/agentdex/modelsdev"
)

// openProviders opens an Index configured only for models.dev: a provider surface
// resolves no agent catalog, so no catalog source is supplied. present names the
// env vars the injected lookup reports as set.
func openProviders(t *testing.T, url string, presentEnv ...string) *Index {
	t.Helper()
	idx, err := Open(context.Background(),
		WithModelsURL(url),
		WithCacheDir(t.TempDir()),
		WithEnvLookup(envFn(t.TempDir(), presentEnv...)),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return idx
}

func providerIDs(items []Provider) []string {
	ids := make([]string, len(items))
	for i, p := range items {
		ids[i] = p.ID
	}
	return ids
}

func TestProvidersListOrdersByIDAndReadsEnv(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"openai", "anthropic", "google"})
	idx := openProviders(t, srv.URL, "ANTHROPIC_API_KEY")

	res, err := idx.Providers.List(context.Background(), ProviderQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := providerIDs(res.Items), []string{"anthropic", "google", "openai"}; !equal(got, want) {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("a provider listing loads no catalog and must raise no warnings: %v", res.Warnings)
	}

	var anthropic Provider
	for _, p := range res.Items {
		if p.ID == "anthropic" {
			anthropic = p
		}
	}
	if got := anthropic.EnvPresent["ANTHROPIC_API_KEY"]; !got {
		t.Errorf("ANTHROPIC_API_KEY presence = false, want true (env is set)")
	}
	for _, p := range res.Items {
		if p.ID == "google" && p.EnvPresent["GOOGLE_API_KEY"] {
			t.Errorf("GOOGLE_API_KEY presence = true, want false (env unset)")
		}
	}
}

func TestProvidersListFilterMatchesNothing(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google"})
	idx := openProviders(t, srv.URL)

	res, err := idx.Providers.List(context.Background(), ProviderQuery{Filter: "zzzz"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Items) != 0 {
		t.Errorf("filter matching nothing = %v, want empty and no error", providerIDs(res.Items))
	}
}

func TestProvidersListFilterOnIDAndName(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google", "openai"})
	idx := openProviders(t, srv.URL)

	// "e" matches google and openai by id, and no others.
	res, err := idx.Providers.List(context.Background(), ProviderQuery{Filter: "E"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := providerIDs(res.Items), []string{"google", "openai"}; !equal(got, want) {
		t.Errorf("filtered ids = %v, want %v", got, want)
	}
}

func TestProvidersListUnreachableIsModelsUnavailable(t *testing.T) {
	idx := openProviders(t, modelsdevtest.Closed(t))

	_, err := idx.Providers.List(context.Background(), ProviderQuery{})
	if !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("List against a closed models.dev err = %v, want ErrModelsUnavailable", err)
	}
	if errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Errorf("an outage must not resolve as schema drift: %v", err)
	}
}

func TestProvidersListSchemaDriftPropagates(t *testing.T) {
	// An empty top-level providers map is gross structural drift caught on the
	// Catalog fetch, distinct from an outage so the CLI maps it to config not
	// transient.
	srv := modelsdevtest.Server(t, nil)
	idx := openProviders(t, srv.URL)

	_, err := idx.Providers.List(context.Background(), ProviderQuery{})
	if !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Fatalf("List against gross drift err = %v, want wrapping modelsdev.ErrModelsSchema", err)
	}
	if errors.Is(err, ErrModelsUnavailable) {
		t.Errorf("schema drift must not resolve as ErrModelsUnavailable: %v", err)
	}
}

func TestProvidersGetReportsEnvPresence(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL, "ANTHROPIC_API_KEY")

	p, err := idx.Providers.Get(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.ID != "anthropic" {
		t.Errorf("id = %q, want anthropic", p.ID)
	}
	if !p.EnvPresent["ANTHROPIC_API_KEY"] {
		t.Errorf("ANTHROPIC_API_KEY presence = false, want true")
	}
	if len(p.Models) == 0 {
		t.Errorf("provider get should carry the embedded models map")
	}
}

func TestProvidersGetUnknownIsNotFound(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	_, err := idx.Providers.Get(context.Background(), "no-such-provider")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown provider err = %v, want ErrNotFound", err)
	}
	if want := `no models.dev provider "no-such-provider"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestProvidersGetUnreachableIsModelsUnavailable(t *testing.T) {
	idx := openProviders(t, modelsdevtest.Closed(t))

	_, err := idx.Providers.Get(context.Background(), "anthropic")
	if !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("Get against a closed models.dev err = %v, want ErrModelsUnavailable", err)
	}
}

func TestProvidersGetSchemaDriftPropagates(t *testing.T) {
	// A reachable models.dev serving a malformed provider model is a per-provider
	// data fault surfaced on the exact fetch.
	srv := modelsdevtest.Server(t, nil, "anthropic")
	idx := openProviders(t, srv.URL)

	_, err := idx.Providers.Get(context.Background(), "anthropic")
	if !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Fatalf("Get against malformed provider err = %v, want wrapping modelsdev.ErrModelsSchema", err)
	}
}
