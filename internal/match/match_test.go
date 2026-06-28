package match

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	items := []Item{
		{ID: "claude-sonnet", Name: "Claude Sonnet"},
		{ID: "claude-opus", Name: "Claude Opus"},
		{ID: "gpt-5", Name: "GPT-5"},
	}

	tests := []struct {
		name        string
		query       string
		wantOutcome Outcome
		wantIndex   int
		wantCands   []string
	}{
		{"exact id", "claude-opus", Unique, 1, nil},
		{"exact name case-insensitive", "claude sonnet", Unique, 0, nil},
		{"unique substring", "sonnet", Unique, 0, nil},
		{"unique prefix", "gpt", Unique, 2, nil},
		{"ambiguous substring", "claude", Ambiguous, -1, []string{"claude-sonnet", "claude-opus"}},
		{"no match", "gemini", None, -1, nil},
		{"exact id wins over substring", "claude-sonnet", Unique, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome, idx, cands := Match(tt.query, items)
			if outcome != tt.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if idx != tt.wantIndex {
				t.Errorf("index = %d, want %d", idx, tt.wantIndex)
			}
			if !reflect.DeepEqual(cands, tt.wantCands) {
				t.Errorf("candidates = %v, want %v", cands, tt.wantCands)
			}
		})
	}
}

// TestMatchDuplicateExact covers two providers exposing a model with the same
// short id and the same display name. Every stage applies the none/one/many rule,
// so an exact id or name that matches more than one candidate is Ambiguous rather
// than silently resolving to the first.
func TestMatchDuplicateExact(t *testing.T) {
	items := []Item{
		{ID: "claude-opus", Name: "Claude Opus"}, // anthropic
		{ID: "claude-opus", Name: "Claude Opus"}, // a reseller exposing the same id/name
		{ID: "gpt-5", Name: "GPT-5"},
	}

	tests := []struct {
		name        string
		query       string
		wantOutcome Outcome
		wantIndex   int
		wantCands   []string
	}{
		{"duplicate exact id", "claude-opus", Ambiguous, -1, []string{"claude-opus", "claude-opus"}},
		{"duplicate exact name", "claude opus", Ambiguous, -1, []string{"claude-opus", "claude-opus"}},
		{"unique exact id still resolves", "gpt-5", Unique, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcome, idx, cands := Match(tt.query, items)
			if outcome != tt.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if idx != tt.wantIndex {
				t.Errorf("index = %d, want %d", idx, tt.wantIndex)
			}
			if !reflect.DeepEqual(cands, tt.wantCands) {
				t.Errorf("candidates = %v, want %v", cands, tt.wantCands)
			}
		})
	}
}
