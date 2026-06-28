package agentdex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// modelsCatalogJSON is a well-formed models.dev catalog: one provider with an
// API-key env var and two models, one of which has a matching agnostic entry.
const modelsCatalogJSON = `{
  "models": {
    "anthropic/claude-sonnet": {"id": "anthropic/claude-sonnet", "name": "Claude Sonnet", "limit": {"context": 200000, "output": 64000}}
  },
  "providers": {
    "anthropic": {
      "id": "anthropic",
      "env": ["ANTHROPIC_API_KEY"],
      "models": {
        "claude-sonnet": {"id": "claude-sonnet", "name": "Claude Sonnet", "limit": {"context": 200000, "output": 64000}},
        "claude-opus":   {"id": "claude-opus",   "name": "Claude Opus",   "limit": {"context": 200000, "output": 64000}}
      }
    }
  }
}`

// malformedModelsJSON has a well-formed top level but a malformed model (zero
// limit) in the anthropic provider, which the per-provider validating accessor
// rejects with ErrModelsSchema when anthropic is requested.
const malformedModelsJSON = `{
  "models": {
    "anthropic/claude-sonnet": {"id": "anthropic/claude-sonnet", "name": "Claude Sonnet", "limit": {"context": 200000}}
  },
  "providers": {
    "anthropic": {
      "id": "anthropic",
      "env": ["ANTHROPIC_API_KEY"],
      "models": {
        "broken": {"id": "broken", "name": "Broken"}
      }
    }
  }
}`

// modelsClient serves body on every request from a fresh cache directory.
func modelsClient(t *testing.T, body string) *modelsdev.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return modelsdev.New(modelsdev.WithURL(srv.URL), modelsdev.WithCacheDir(t.TempDir()))
}

// unreachableClient points at a server that always 500s, with an empty cache, so
// the catalog cannot be loaded and enrichment must degrade.
func unreachableClient(t *testing.T) *modelsdev.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return modelsdev.New(modelsdev.WithURL(srv.URL), modelsdev.WithCacheDir(t.TempDir()))
}

func anthropicAgentCatalog() *Catalog {
	return &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Provider: []string{"anthropic"}},
	}}
}

func TestEnrichProviderEnvOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	mc := modelsClient(t, modelsCatalogJSON)

	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(anthropicAgentCatalog()), WithModels(mc))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if present, ok := a.ProviderEnv["ANTHROPIC_API_KEY"]; !ok || !present {
		t.Errorf("ProviderEnv[ANTHROPIC_API_KEY] = %v,%v, want true,true", present, ok)
	}
	if a.Models != nil {
		t.Errorf("Models = %v, want nil without EnrichModels", a.Models)
	}
}

func TestEnrichProviderEnvAbsentVar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Register restoration via Setenv, then clear so LookupEnv reports absent.
	t.Setenv("ANTHROPIC_API_KEY", "")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	mc := modelsClient(t, modelsCatalogJSON)

	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(anthropicAgentCatalog()), WithModels(mc))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if present := a.ProviderEnv["ANTHROPIC_API_KEY"]; present {
		t.Errorf("ProviderEnv[ANTHROPIC_API_KEY] = true, want false when unset")
	}
}

func TestEnrichModelsFilled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mc := modelsClient(t, modelsCatalogJSON)

	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(anthropicAgentCatalog()),
		WithModels(mc, EnrichModels()))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if len(a.Models) != 2 {
		t.Fatalf("Models len = %d, want 2", len(a.Models))
	}
	// Models returns sorted by id: claude-opus before claude-sonnet.
	if a.Models[0].ID != "claude-opus" || a.Models[1].ID != "claude-sonnet" {
		t.Errorf("Models order = %q,%q, want sorted", a.Models[0].ID, a.Models[1].ID)
	}
}

func TestDetectEnrichesFoundAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	dir := t.TempDir()
	bin := writeStub(t, dir, "agent", "agent 1.0.0")
	mc := modelsClient(t, modelsCatalogJSON)

	agents, err := Detect(context.Background(), WithCatalog(anthropicAgentCatalog()),
		WithBinPaths(map[string]string{"agent": bin}), WithModels(mc, EnrichModels()))
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	a := agents[0]
	if present, ok := a.ProviderEnv["ANTHROPIC_API_KEY"]; !ok || !present {
		t.Errorf("ProviderEnv[ANTHROPIC_API_KEY] = %v,%v, want true,true", present, ok)
	}
	if len(a.Models) != 2 {
		t.Errorf("Models len = %d, want 2 filled via Detect path", len(a.Models))
	}
}

func TestEnrichConsultedButProviderAbsentIsNonNilEmpty(t *testing.T) {
	// A reachable models.dev that lacks the agent's provider is "consulted, nothing
	// present": a non-nil empty ProviderEnv, distinct from the nil degrade case.
	t.Setenv("HOME", t.TempDir())
	mc := modelsClient(t, modelsCatalogJSON)
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Provider: []string{"absent-provider"}},
	}}

	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(cat), WithModels(mc))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if a.ProviderEnv == nil {
		t.Error("ProviderEnv = nil, want non-nil empty map once models.dev was consulted")
	}
	if len(a.ProviderEnv) != 0 {
		t.Errorf("ProviderEnv = %v, want empty", a.ProviderEnv)
	}
}

func TestEnrichNoProvidersLeavesProviderEnvNil(t *testing.T) {
	// An agent with no providers is never consulted against models.dev even with a
	// client attached, so ProviderEnv stays nil per its contract, distinct from the
	// non-nil empty "consulted, nothing present" case.
	t.Setenv("HOME", t.TempDir())
	mc := modelsClient(t, modelsCatalogJSON)
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}},
	}}

	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(cat), WithModels(mc))
	if err != nil {
		t.Fatalf("DetectOne: %v", err)
	}
	if a.ProviderEnv != nil {
		t.Errorf("ProviderEnv = %v, want nil for an agent with no providers", a.ProviderEnv)
	}
}

func TestEnrichDegradesWhenUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mc := unreachableClient(t)

	a, _, err := DetectOne(context.Background(), "agent", WithCatalog(anthropicAgentCatalog()),
		WithModels(mc, EnrichModels()))
	if err != nil {
		t.Fatalf("DetectOne should degrade, got error: %v", err)
	}
	if a.ProviderEnv != nil || a.Models != nil {
		t.Errorf("ProviderEnv=%v Models=%v, want both nil on unreachable models.dev", a.ProviderEnv, a.Models)
	}
}

func TestEnrichPropagatesContextCancellation(t *testing.T) {
	// A cancelled run must surface ctx.Err() rather than degrade to a silent
	// nil-enrichment success, so a caller cannot mistake an aborted run for a
	// clean models.dev outage. The server blocks so the only ready select arm is
	// the cancelled context.
	t.Setenv("HOME", t.TempDir())
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	mc := modelsdev.New(modelsdev.WithURL(srv.URL), modelsdev.WithCacheDir(t.TempDir()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := DetectOne(ctx, "agent", WithCatalog(anthropicAgentCatalog()), WithModels(mc))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled (cancellation must not degrade)", err)
	}
}

func TestEnrichSchemaDriftPropagates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Schema drift must propagate whether or not per-model enrichment was requested.
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"with enrich", []Option{WithModels(modelsClient(t, malformedModelsJSON), EnrichModels())}},
		{"provider-env only", []Option{WithModels(modelsClient(t, malformedModelsJSON))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]Option{WithCatalog(anthropicAgentCatalog())}, tc.opts...)
			_, _, err := DetectOne(context.Background(), "agent", opts...)
			if !errors.Is(err, modelsdev.ErrModelsSchema) {
				t.Errorf("err = %v, want ErrModelsSchema", err)
			}
		})
	}
}

func TestDetectSchemaDriftPropagates(t *testing.T) {
	// A found agent whose requested provider is malformed fails Detect too.
	dir := t.TempDir()
	bin := writeStub(t, dir, "agent", "agent 1.0.0")
	mc := modelsClient(t, malformedModelsJSON)

	_, err := Detect(context.Background(), WithCatalog(anthropicAgentCatalog()),
		WithBinPaths(map[string]string{"agent": bin}), WithModels(mc, EnrichModels()))
	if !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Errorf("err = %v, want ErrModelsSchema from Detect", err)
	}
}

func TestDetectFailsFastAcrossConcurrentAgents(t *testing.T) {
	// With several agents detected concurrently and one requesting a malformed
	// provider, Detect must fail the whole run rather than leak partial results:
	// the first non-degradable error cancels the siblings and surfaces alone.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	cat := &Catalog{Agents: map[string]KnownAgent{
		"alpha": {Name: "Alpha", Bin: "alpha", Config: PathPair{Global: "~/.a"}, Provider: []string{"anthropic"}},
		"beta":  {Name: "Beta", Bin: "beta", Config: PathPair{Global: "~/.b"}, Provider: []string{"anthropic"}},
		"gamma": {Name: "Gamma", Bin: "gamma", Config: PathPair{Global: "~/.g"}, Provider: []string{"anthropic"}},
	}}
	binPaths := map[string]string{
		"alpha": writeStub(t, dir, "alpha", "alpha 1.0.0"),
		"beta":  writeStub(t, dir, "beta", "beta 1.0.0"),
		"gamma": writeStub(t, dir, "gamma", "gamma 1.0.0"),
	}
	mc := modelsClient(t, malformedModelsJSON)

	agents, err := Detect(context.Background(), WithCatalog(cat),
		WithBinPaths(binPaths), WithModels(mc, EnrichModels()))
	if !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Errorf("err = %v, want ErrModelsSchema", err)
	}
	if agents != nil {
		t.Errorf("agents = %v, want nil (no partial results on fail-fast)", agents)
	}
}
