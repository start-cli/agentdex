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
