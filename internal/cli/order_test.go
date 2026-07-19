package cli

import (
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// orderTestSet is a throwaway field set for exercising the ordering primitives
// directly, independent of any command's real schema.
var orderTestSet = newFieldSet(
	[]string{"id", "name", "context", "price", "flag", "tags"},
	nil,
).ordered("id", "price")

func orderRec(id string, kv map[string]any) *record {
	r := newRecord(orderTestSet)
	r.add("id", id, id)
	for k, v := range kv {
		r.add(k, v, "")
	}
	return r
}

func ids(recs []*record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = recordID(r)
	}
	return out
}

func TestOrderRecords(t *testing.T) {
	for _, tc := range []struct {
		name       string
		recs       []*record
		key        string
		descending bool
		want       []string
	}{
		{
			name: "string ascending",
			recs: []*record{
				orderRec("gamma", nil), orderRec("alpha", nil), orderRec("beta", nil),
			},
			key:  "id",
			want: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "string descending",
			recs: []*record{
				orderRec("alpha", nil), orderRec("gamma", nil), orderRec("beta", nil),
			},
			key:        "id",
			descending: true,
			want:       []string{"gamma", "beta", "alpha"},
		},
		{
			name: "numeric ascending, zero is a real value",
			recs: []*record{
				orderRec("a", map[string]any{"context": 200000}),
				orderRec("b", map[string]any{"context": 0}),
				orderRec("c", map[string]any{"context": 8000}),
			},
			key:  "context",
			want: []string{"b", "c", "a"},
		},
		{
			name: "missing values sink last regardless of direction",
			recs: []*record{
				orderRec("dated", map[string]any{"price": 5.0}),
				orderRec("undated", nil),
				orderRec("cheap", map[string]any{"price": 1.0}),
			},
			key:  "price",
			want: []string{"cheap", "dated", "undated"},
		},
		{
			name: "missing values still sink last when descending",
			recs: []*record{
				orderRec("dated", map[string]any{"price": 5.0}),
				orderRec("undated", nil),
				orderRec("cheap", map[string]any{"price": 1.0}),
			},
			key:        "price",
			descending: true,
			want:       []string{"dated", "cheap", "undated"},
		},
		{
			name: "empty string is missing and sinks last",
			recs: []*record{
				orderRec("a", map[string]any{"name": "zed"}),
				orderRec("b", map[string]any{"name": ""}),
				orderRec("c", map[string]any{"name": "amy"}),
			},
			key:  "name",
			want: []string{"c", "a", "b"},
		},
		{
			name: "bool orders false before true",
			recs: []*record{
				orderRec("on", map[string]any{"flag": true}),
				orderRec("off", map[string]any{"flag": false}),
			},
			key:  "flag",
			want: []string{"off", "on"},
		},
		{
			name: "slice orders by length",
			recs: []*record{
				orderRec("many", map[string]any{"tags": []string{"a", "b", "c"}}),
				orderRec("none", map[string]any{"tags": []string{}}),
				orderRec("one", map[string]any{"tags": []string{"a"}}),
			},
			key:  "tags",
			want: []string{"none", "one", "many"},
		},
		{
			name: "ties break by id",
			recs: []*record{
				orderRec("gamma", map[string]any{"price": 5.0}),
				orderRec("alpha", map[string]any{"price": 5.0}),
				orderRec("beta", map[string]any{"price": 5.0}),
			},
			key:  "price",
			want: []string{"alpha", "beta", "gamma"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orderRecords(tc.recs, tc.key, tc.descending)
			got := ids(tc.recs)
			for i, want := range tc.want {
				if got[i] != want {
					t.Fatalf("order = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestApplyOrderDefaultAndReverse(t *testing.T) {
	// The default key sorts by the field's natural direction; price is declared
	// descending, so the default is most-expensive first and --reverse flips it.
	build := func() []*record {
		return []*record{
			orderRec("a", map[string]any{"price": 1.0}),
			orderRec("b", map[string]any{"price": 9.0}),
			orderRec("c", map[string]any{"price": 5.0}),
		}
	}

	recs := build()
	key, err := applyOrder(recs, orderTestSet.ordered("price", "price"), "", false)
	if err != nil {
		t.Fatalf("applyOrder default: %v", err)
	}
	if key != "price" {
		t.Fatalf("default sort key = %q, want price", key)
	}
	if got := ids(recs); got[0] != "b" || got[2] != "a" {
		t.Errorf("default (descending price) order = %v, want b..a", got)
	}

	recs = build()
	if _, err := applyOrder(recs, orderTestSet.ordered("price", "price"), "", true); err != nil {
		t.Fatalf("applyOrder reverse: %v", err)
	}
	if got := ids(recs); got[0] != "a" || got[2] != "b" {
		t.Errorf("--reverse of descending price = %v, want a..b", got)
	}
}

func TestApplyOrderReverseFlipsAscending(t *testing.T) {
	// id is naturally ascending; --reverse makes it descending.
	recs := []*record{orderRec("a", nil), orderRec("b", nil), orderRec("c", nil)}
	if _, err := applyOrder(recs, orderTestSet, "id", true); err != nil {
		t.Fatalf("applyOrder: %v", err)
	}
	if got := ids(recs); got[0] != "c" || got[2] != "a" {
		t.Errorf("--reverse of ascending id = %v, want c..a", got)
	}
}

func TestApplyOrderUnknownKeyIsError(t *testing.T) {
	recs := []*record{orderRec("a", nil)}
	if _, err := applyOrder(recs, orderTestSet, "bogus", false); err == nil {
		t.Fatal("applyOrder with unknown key should error")
	}
}

func TestOrderColumns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cols    []string
		sortKey string
		want    []string
	}{
		{"prepends when absent", []string{"id", "name"}, "released", []string{"released", "id", "name"}},
		{"moves to front when present", []string{"id", "name", "context"}, "context", []string{"context", "id", "name"}},
		{"no change when already leftmost", []string{"id", "name"}, "id", []string{"id", "name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := orderColumns(tc.cols, tc.sortKey)
			if len(got) != len(tc.want) {
				t.Fatalf("orderColumns = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("orderColumns = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestTotalCost(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cost     *modelsdev.Cost
		wantVal  any
		wantText string
	}{
		{"sum of input and output", &modelsdev.Cost{Input: 3, Output: 15}, 18.0, "$18"},
		{"trims trailing zeros", &modelsdev.Cost{Input: 0.05, Output: 0.025}, 0.075, "$0.075"},
		{"unknown pricing is nil", nil, nil, "-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := totalValue(tc.cost); got != tc.wantVal {
				t.Errorf("totalValue = %v, want %v", got, tc.wantVal)
			}
			if got := totalText(tc.cost); got != tc.wantText {
				t.Errorf("totalText = %q, want %q", got, tc.wantText)
			}
		})
	}
}
