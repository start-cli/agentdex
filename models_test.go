package agentdex

import (
	"context"
	"errors"
	"testing"

	"github.com/start-cli/agentdex/internal/catalogtest"
	"github.com/start-cli/agentdex/internal/modelsdevtest"
	"github.com/start-cli/agentdex/modelsdev"
)

// openModels opens an Index over both a directory catalog and a models.dev double,
// for the --agent-scoped model listings that resolve the agent catalog.
func openModels(t *testing.T, body, url string, presentEnv ...string) *Index {
	t.Helper()
	dir := catalogtest.WriteModule(t, body)
	idx, err := Open(context.Background(),
		WithCatalogDir(dir),
		WithCacheDir(t.TempDir()),
		WithModelsURL(url),
		WithEnvLookup(envFn(t.TempDir(), presentEnv...)),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return idx
}

func wrappedModelIDs(items []Model) []string {
	ids := make([]string, len(items))
	for i, m := range items {
		ids[i] = m.ID
	}
	return ids
}

func TestModelsListNoScopeSpansEveryProviderNewestFirst(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google", "openai"})
	idx := openProviders(t, srv.URL)

	res, err := idx.Models.List(context.Background(), ModelQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// anthropic 2025-06-01, openai 2025-01-01, google 2024-01-01.
	if got, want := wrappedModelIDs(res.Items), []string{"claude-sonnet", "openai-model", "google-model"}; !equal(got, want) {
		t.Fatalf("model order = %v, want newest-first %v", got, want)
	}
	var claude Model
	for _, m := range res.Items {
		if m.ID == "claude-sonnet" {
			claude = m
		}
	}
	if claude.Provider != "anthropic" {
		t.Errorf("claude-sonnet provider = %q, want anthropic", claude.Provider)
	}
	if want := "anthropic/claude-sonnet"; claude.CanonicalID != want {
		t.Errorf("canonical id = %q, want %q", claude.CanonicalID, want)
	}
	for _, m := range res.Items {
		if m.ID == "google-model" && m.CanonicalID != "" {
			t.Errorf("google-model canonical id = %q, want empty (not in agnostic map)", m.CanonicalID)
		}
	}
}

func TestModelsListProviderScopeValidates(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google"})
	idx := openProviders(t, srv.URL)

	res, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Providers: []string{"google"}}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := wrappedModelIDs(res.Items), []string{"google-model"}; !equal(got, want) {
		t.Errorf("scoped ids = %v, want %v", got, want)
	}
}

func TestModelsListUnknownProviderIsUnknownProvider(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	_, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Providers: []string{"nope"}}})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown --provider err = %v, want ErrUnknownProvider", err)
	}
}

func TestModelsListUnreachableDoesNotRejectIDs(t *testing.T) {
	// An outage is not an unknown-id verdict: the ids stand and the fetch reports
	// the outage as ErrModelsUnavailable, never ErrUnknownProvider (R8).
	idx := openProviders(t, modelsdevtest.Closed(t))

	_, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Providers: []string{"anthropic"}}})
	if !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("unreachable --provider err = %v, want ErrModelsUnavailable", err)
	}
	if errors.Is(err, ErrUnknownProvider) {
		t.Errorf("an outage must not reject ids as unknown: %v", err)
	}
}

func TestModelsListFilterNarrows(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google", "openai"})
	idx := openProviders(t, srv.URL)

	res, err := idx.Models.List(context.Background(), ModelQuery{Filter: "claude"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := wrappedModelIDs(res.Items), []string{"claude-sonnet"}; !equal(got, want) {
		t.Errorf("filtered ids = %v, want %v", got, want)
	}
}

func TestModelsListAgentHomeProvider(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google", "openai"})
	idx := openModels(t, testCatalog, srv.URL)

	res, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Agent: "gamma-agent"}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// gamma-agent's catalog providers are google+openai; anthropic is out of scope.
	if got, want := wrappedModelIDs(res.Items), []string{"openai-model", "google-model"}; !equal(got, want) {
		t.Errorf("agent-scoped ids = %v, want %v (openai newer than google)", got, want)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("a directory catalog is never stale, so no warning: %v", res.Warnings)
	}
}

func TestModelsListAgentHomeProviderRejectsProviderSet(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic", "google"})
	idx := openModels(t, testCatalog, srv.URL)

	_, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Agent: "alpha-cli", Providers: []string{"google"}}})
	if !errors.Is(err, ErrProvidersNotAllowed) {
		t.Fatalf("home-provider agent with --provider err = %v, want ErrProvidersNotAllowed", err)
	}
	if want := `agent "alpha-cli" has catalog providers`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestModelsListAgnosticAgentRequiresProviders(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"google"})
	idx := openModels(t, testCatalog, srv.URL)

	_, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Agent: "delta-agent"}})
	if !errors.Is(err, ErrProvidersRequired) {
		t.Fatalf("agnostic agent without --provider err = %v, want ErrProvidersRequired", err)
	}
	if want := `providers required for agnostic agent: "delta-agent" is provider-agnostic`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestModelsListAgnosticAgentWithProviders(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"google", "openai"})
	idx := openModels(t, testCatalog, srv.URL)

	res, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Agent: "delta-agent", Providers: []string{"google"}}})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := wrappedModelIDs(res.Items), []string{"google-model"}; !equal(got, want) {
		t.Errorf("agnostic-scoped ids = %v, want %v", got, want)
	}
}

func TestModelsListUnknownAgentIsAgentUnknown(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openModels(t, testCatalog, srv.URL)

	_, err := idx.Models.List(context.Background(), ModelQuery{Scope: ModelScope{Agent: "no-such-agent"}})
	if !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("unknown --agent err = %v, want ErrAgentUnknown", err)
	}
	if want := `no agent "no-such-agent"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestModelsGetComposite(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	m, err := idx.Models.Get(context.Background(), "anthropic/claude-sonnet")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", m.Provider)
	}
	if want := "anthropic/claude-sonnet"; m.CanonicalID != want {
		t.Errorf("canonical id = %q, want %q", m.CanonicalID, want)
	}
}

func TestModelsGetNonAgnosticHasNoCanonicalID(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"google"})
	idx := openProviders(t, srv.URL)

	m, err := idx.Models.Get(context.Background(), "google/google-model")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.CanonicalID != "" {
		t.Errorf("canonical id = %q, want empty (not in agnostic map)", m.CanonicalID)
	}
}

func TestModelsGetMalformedComposite(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	_, err := idx.Models.Get(context.Background(), "no-slash")
	if !errors.Is(err, ErrMalformedModelID) {
		t.Fatalf("no-slash composite err = %v, want ErrMalformedModelID", err)
	}
	if want := `model id "no-slash" must be provider-id/model-id`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestModelsGetUnknownProviderComposite(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	_, err := idx.Models.Get(context.Background(), "nope/some-model")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown-provider composite err = %v, want ErrNotFound", err)
	}
	if want := `no model "nope/some-model": unknown provider "nope"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestModelsGetUnknownModelKey(t *testing.T) {
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	_, err := idx.Models.Get(context.Background(), "anthropic/no-such-model")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown-key composite err = %v, want ErrNotFound", err)
	}
	if want := `no model "anthropic/no-such-model" in provider "anthropic"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestModelsGetSplitsOnFirstSlash(t *testing.T) {
	// A model key may contain slashes; the split takes the whole remainder as the
	// key. anthropic exists, so a multi-slash remainder resolves the provider and
	// then misses on the key — proving the split is first-slash, not last (R9).
	srv := modelsdevtest.Server(t, []string{"anthropic"})
	idx := openProviders(t, srv.URL)

	_, err := idx.Models.Get(context.Background(), "anthropic/family/model")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("multi-slash composite err = %v, want ErrNotFound", err)
	}
	if want := `no model "anthropic/family/model" in provider "anthropic"`; err.Error() != want {
		t.Errorf("message = %q, want %q (a last-slash split would name provider %q unknown)", err.Error(), want, "anthropic/family")
	}
}

func TestModelsGetSchemaDriftPropagates(t *testing.T) {
	srv := modelsdevtest.Server(t, nil, "anthropic")
	idx := openProviders(t, srv.URL)

	_, err := idx.Models.Get(context.Background(), "anthropic/claude-sonnet")
	if !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Fatalf("Get against malformed provider err = %v, want wrapping modelsdev.ErrModelsSchema", err)
	}
}
