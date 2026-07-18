package modelsdev

import (
	"errors"
	"testing"
)

func TestMergeAttachesAgnosticDataOnRealKeys(t *testing.T) {
	cat := &Catalog{
		Models: map[string]Model{
			"anthropic/claude-x": {ID: "anthropic/claude-x", Benchmarks: []Benchmark{{Name: "SWE-Bench"}}, Weights: []Weight{{Label: "HF"}}},
			"xai/grok-4":         {ID: "xai/grok-4", Benchmarks: []Benchmark{{Name: "GPQA"}}},
		},
		Providers: map[string]Provider{
			"anthropic": {ID: "anthropic", Models: map[string]Model{"claude-x": {ID: "claude-x", Limit: Limit{Context: 1}}}},
			"xai":       {ID: "xai", Models: map[string]Model{"grok-4": {ID: "grok-4", Limit: Limit{Context: 1}}}},
			// An aggregator re-exposes xai's model under a path-bearing key. No
			// agnostic id decomposes to (requesty, "xai/grok-4"), so it gets nothing.
			"requesty": {ID: "requesty", Models: map[string]Model{"xai/grok-4": {ID: "xai/grok-4", Limit: Limit{Context: 1}}}},
		},
	}

	merge(cat)

	firstParty := cat.Providers["anthropic"].Models["claude-x"]
	if len(firstParty.Benchmarks) != 1 || len(firstParty.Weights) != 1 {
		t.Errorf("first-party model did not receive agnostic benchmarks/weights: %+v", firstParty)
	}
	if firstParty.ID != "claude-x" {
		t.Errorf("first-party Model.ID rewritten: got %q, want short %q", firstParty.ID, "claude-x")
	}

	if xai := cat.Providers["xai"].Models["grok-4"]; len(xai.Benchmarks) != 1 {
		t.Errorf("first-party xai model did not receive benchmarks: %+v", xai)
	}

	aggregator := cat.Providers["requesty"].Models["xai/grok-4"]
	if len(aggregator.Benchmarks) != 0 || len(aggregator.Weights) != 0 {
		t.Errorf("aggregator model received benchmarks it has no agnostic id for: %+v", aggregator)
	}
	if aggregator.ID != "xai/grok-4" {
		t.Errorf("aggregator Model.ID changed: got %q", aggregator.ID)
	}

	// The agnostic map keeps its own source ids untouched.
	if cat.Models["anthropic/claude-x"].ID != "anthropic/claude-x" {
		t.Errorf("agnostic Model.ID changed: got %q", cat.Models["anthropic/claude-x"].ID)
	}
}

func TestMergeIgnoresNonDecomposableIDs(t *testing.T) {
	// A bare id with no slash and a provider with no matching model must not panic
	// or mint anything.
	cat := &Catalog{
		Models: map[string]Model{
			"noslash":          {ID: "noslash", Benchmarks: []Benchmark{{Name: "X"}}},
			"unknown/model":    {ID: "unknown/model", Benchmarks: []Benchmark{{Name: "Y"}}},
			"anthropic/absent": {ID: "anthropic/absent", Benchmarks: []Benchmark{{Name: "Z"}}},
		},
		Providers: map[string]Provider{
			"anthropic": {ID: "anthropic", Models: map[string]Model{"present": {ID: "present", Limit: Limit{Context: 1}}}},
		},
	}

	merge(cat)

	if got := cat.Providers["anthropic"].Models["present"]; len(got.Benchmarks) != 0 {
		t.Errorf("model with no agnostic id received benchmarks: %+v", got)
	}
}

func TestValidateTopLevel(t *testing.T) {
	tests := []struct {
		name    string
		cat     Catalog
		wantErr bool
	}{
		{"both present", Catalog{Models: map[string]Model{"a/b": {}}, Providers: map[string]Provider{"a": {}}}, false},
		{"empty models", Catalog{Models: map[string]Model{}, Providers: map[string]Provider{"a": {}}}, true},
		{"empty providers", Catalog{Models: map[string]Model{"a/b": {}}, Providers: map[string]Provider{}}, true},
		{"both nil", Catalog{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTopLevel(&tt.cat)
			if tt.wantErr && !errors.Is(err, ErrModelsSchema) {
				t.Errorf("got %v, want ErrModelsSchema", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("got %v, want nil", err)
			}
		})
	}
}

func TestValidateProvider(t *testing.T) {
	good := Limit{Context: 200000, Output: 64000}
	tests := []struct {
		name    string
		model   Model
		wantErr bool
	}{
		{"well formed", Model{ID: "m", Limit: good}, false},
		{"empty id", Model{ID: "", Limit: good}, true},
		// Media-generation models legitimately carry a zero limit; upstream serves
		// limit {context:0, output:0} for them. gpt-image-1.5 keeps cost data,
		// openai-gpt-image-2 has none — neither is malformed.
		{"limitless image model with cost", Model{ID: "gpt-image-1.5", Limit: Limit{}, Cost: &Cost{Input: 5, Output: 32}}, false},
		{"limitless image model no cost", Model{ID: "openai-gpt-image-2", Limit: Limit{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Provider{ID: "p", Models: map[string]Model{"k": tt.model}}
			err := validateProvider(p)
			if tt.wantErr && !errors.Is(err, ErrModelsSchema) {
				t.Errorf("got %v, want ErrModelsSchema", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("got %v, want nil", err)
			}
		})
	}
}
