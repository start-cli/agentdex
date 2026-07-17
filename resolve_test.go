package agentdex

import (
	"context"
	"errors"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

func resolveCatalog() *Catalog {
	return &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Provider: []string{"anthropic"}},
	}}
}

func TestResolveModel(t *testing.T) {
	mc := modelsClient(t, modelsCatalogJSON)
	cat := resolveCatalog()

	tests := []struct {
		name          string
		query         string
		wantID        string // provider-local Model.ID
		wantProvider  string
		wantCanonical string
	}{
		{"exact id", "claude-sonnet", "claude-sonnet", "anthropic", "anthropic/claude-sonnet"},
		{"exact name", "Claude Opus", "claude-opus", "anthropic", ""},
		{"unique substring", "sonnet", "claude-sonnet", "anthropic", "anthropic/claude-sonnet"},
		{"no agnostic entry", "opus", "claude-opus", "anthropic", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, provider, canonical, err := cat.ResolveModel(context.Background(), "agent", tt.query, mc, nil)
			if err != nil {
				t.Fatalf("ResolveModel: %v", err)
			}
			if m.ID != tt.wantID {
				t.Errorf("Model.ID = %q, want %q (source id preserved)", m.ID, tt.wantID)
			}
			if provider != tt.wantProvider {
				t.Errorf("provider = %q, want %q", provider, tt.wantProvider)
			}
			if canonical != tt.wantCanonical {
				t.Errorf("canonical = %q, want %q", canonical, tt.wantCanonical)
			}
		})
	}
}

// multiProviderModelsJSON serves two providers, so a resolve across an agent with
// more than one provider can be exercised.
const multiProviderModelsJSON = `{
  "models": {
    "anthropic/claude-opus": {"id": "anthropic/claude-opus", "name": "Claude Opus", "limit": {"context": 200000, "output": 64000}}
  },
  "providers": {
    "anthropic": {
      "id": "anthropic",
      "env": ["ANTHROPIC_API_KEY"],
      "models": {
        "claude-opus": {"id": "claude-opus", "name": "Claude Opus", "limit": {"context": 200000, "output": 64000}}
      }
    },
    "openai": {
      "id": "openai",
      "env": ["OPENAI_API_KEY"],
      "models": {
        "gpt-5": {"id": "gpt-5", "name": "GPT-5", "limit": {"context": 400000, "output": 128000}}
      }
    }
  }
}`

func TestResolveModelAcrossProviders(t *testing.T) {
	mc := modelsClient(t, multiProviderModelsJSON)
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Provider: []string{"anthropic", "openai"}},
	}}

	// A query that matches a model in the second provider resolves to that
	// provider, confirming the resolver spans every provider the agent declares.
	m, provider, canonical, err := cat.ResolveModel(context.Background(), "agent", "gpt", mc, nil)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if m.ID != "gpt-5" || provider != "openai" {
		t.Errorf("got id=%q provider=%q, want gpt-5/openai", m.ID, provider)
	}
	if canonical != "" {
		t.Errorf("canonical = %q, want empty (gpt-5 has no agnostic entry)", canonical)
	}

	// A model in the first provider still resolves correctly alongside the second.
	m, provider, canonical, err = cat.ResolveModel(context.Background(), "agent", "opus", mc, nil)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if m.ID != "claude-opus" || provider != "anthropic" || canonical != "anthropic/claude-opus" {
		t.Errorf("got id=%q provider=%q canonical=%q, want claude-opus/anthropic/anthropic/claude-opus", m.ID, provider, canonical)
	}
}

func TestResolveModelAgnosticRequiresProviders(t *testing.T) {
	mc := modelsClient(t, modelsCatalogJSON)
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Agnostic: true},
	}}

	_, _, _, err := cat.ResolveModel(context.Background(), "agent", "sonnet", mc, nil)
	if !errors.Is(err, ErrProvidersRequired) {
		t.Errorf("err = %v, want ErrProvidersRequired for an agnostic agent with no providers", err)
	}
}

func TestResolveModelAgnosticWithCallerProviders(t *testing.T) {
	// An agnostic agent resolves within exactly the caller-supplied set, not a
	// catalog list it does not have.
	mc := modelsClient(t, multiProviderModelsJSON)
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Agnostic: true},
	}}

	m, provider, canonical, err := cat.ResolveModel(context.Background(), "agent", "gpt", mc, []string{"openai"})
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if m.ID != "gpt-5" || provider != "openai" || canonical != "" {
		t.Errorf("got id=%q provider=%q canonical=%q, want gpt-5/openai/\"\"", m.ID, provider, canonical)
	}

	// A model outside the caller-supplied set does not match: the set bounds the
	// search even though models.dev carries the model under another provider.
	_, _, _, err = cat.ResolveModel(context.Background(), "agent", "opus", mc, []string{"openai"})
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("err = %v, want ErrModelNotFound outside the caller-supplied set", err)
	}
}

func TestResolveModelDeduplicatesProviders(t *testing.T) {
	// A duplicated provider id must not double the candidate set: a unique query
	// stays unique rather than failing ErrModelAmbiguous against itself.
	mc := modelsClient(t, modelsCatalogJSON)
	cat := &Catalog{Agents: map[string]KnownAgent{
		"agent": {Name: "Agent", Bin: "absent", Config: PathPair{Global: "~/.a"}, Agnostic: true},
	}}

	m, provider, _, err := cat.ResolveModel(context.Background(), "agent", "sonnet", mc, []string{"anthropic", "anthropic"})
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if m.ID != "claude-sonnet" || provider != "anthropic" {
		t.Errorf("got id=%q provider=%q, want claude-sonnet/anthropic", m.ID, provider)
	}
}

func TestResolveModelAmbiguous(t *testing.T) {
	mc := modelsClient(t, modelsCatalogJSON)
	_, _, _, err := resolveCatalog().ResolveModel(context.Background(), "agent", "claude", mc, nil)
	if !errors.Is(err, ErrModelAmbiguous) {
		t.Errorf("err = %v, want ErrModelAmbiguous", err)
	}
}

func TestResolveModelNotFound(t *testing.T) {
	mc := modelsClient(t, modelsCatalogJSON)
	_, _, _, err := resolveCatalog().ResolveModel(context.Background(), "agent", "gemini", mc, nil)
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("err = %v, want ErrModelNotFound", err)
	}
}

func TestResolveModelUnknownAgent(t *testing.T) {
	mc := modelsClient(t, modelsCatalogJSON)
	_, _, _, err := resolveCatalog().ResolveModel(context.Background(), "nope", "sonnet", mc, nil)
	if !errors.Is(err, ErrAgentUnknown) {
		t.Errorf("err = %v, want ErrAgentUnknown", err)
	}
}

func TestResolveModelSchemaDrift(t *testing.T) {
	mc := modelsClient(t, malformedModelsJSON)
	_, _, _, err := resolveCatalog().ResolveModel(context.Background(), "agent", "broken", mc, nil)
	if !errors.Is(err, modelsdev.ErrModelsSchema) {
		t.Errorf("err = %v, want ErrModelsSchema", err)
	}
}
