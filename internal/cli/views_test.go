package cli

import (
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

func TestCostTextFullPrecision(t *testing.T) {
	// Costs are per-1M-token USD prices; the text surface must not round cheap
	// prices down to a misleading "$0.00" while keeping round numbers clean.
	for _, tc := range []struct {
		name string
		cost *modelsdev.Cost
		kind costKind
		want string
	}{
		{"cheap input not rounded to zero", &modelsdev.Cost{Input: 0.075}, costInput, "$0.075"},
		{"sub-cent output kept", &modelsdev.Cost{Output: 0.0001}, costOutput, "$0.0001"},
		{"round number stays clean", &modelsdev.Cost{Input: 3}, costInput, "$3"},
		{"nil cost is a dash", nil, costInput, "-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := costText(tc.cost, tc.kind); got != tc.want {
				t.Errorf("costText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCostValueUnknownIsNil(t *testing.T) {
	if got := costValue(nil, costInput); got != nil {
		t.Errorf("costValue(nil) = %v, want nil so JSON pricing is null not zero", got)
	}
}

func TestSortModelsNewestFirst(t *testing.T) {
	// Newest release first, undated models last, ties broken by id.
	models := []modelsdev.Model{
		{ID: "undated"},
		{ID: "old", ReleaseDate: "2024-01-15"},
		{ID: "new-b", ReleaseDate: "2025-06-01"},
		{ID: "new-a", ReleaseDate: "2025-06-01"},
	}
	sortModelsNewest(models)
	want := []string{"new-a", "new-b", "old", "undated"}
	for i, id := range want {
		if models[i].ID != id {
			t.Fatalf("order[%d] = %q, want %q (full: %v)", i, models[i].ID, id, models)
		}
	}
}
