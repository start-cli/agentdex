package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/start-cli/agentdex/modelsdev"
)

// pricedModelsServer serves one provider "acme" with three models whose combined
// price and release date deliberately do not correlate, so --order-by total and the
// default newest-first ordering produce visibly different orders.
func pricedModelsServer(t *testing.T) string {
	t.Helper()
	models := map[string]modelsdev.Model{
		"cheap":  {ID: "cheap", Name: "Cheap", ReleaseDate: "2023-01-01", Limit: modelsdev.Limit{Context: 128000, Output: 8192}, Cost: &modelsdev.Cost{Input: 1, Output: 1}},
		"mid":    {ID: "mid", Name: "Mid", ReleaseDate: "2025-03-01", Limit: modelsdev.Limit{Context: 128000, Output: 8192}, Cost: &modelsdev.Cost{Input: 3, Output: 3}},
		"pricey": {ID: "pricey", Name: "Pricey", ReleaseDate: "2024-01-01", Limit: modelsdev.Limit{Context: 128000, Output: 8192}, Cost: &modelsdev.Cost{Input: 5, Output: 5}},
	}
	cat := modelsdev.Catalog{
		Models: map[string]modelsdev.Model{"acme/cheap": {ID: "acme/cheap", Name: "Cheap", Limit: modelsdev.Limit{Context: 128000}}},
		Providers: map[string]modelsdev.Provider{
			"acme": {ID: "acme", Name: "Acme", Env: []string{"ACME_API_KEY"}, Models: models},
		},
	}
	data, err := json.Marshal(cat)
	if err != nil {
		t.Fatalf("marshal priced catalog: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// modelIDOrder returns the id of each row in the JSON data array, in order.
func modelIDOrder(t *testing.T, r result) []string {
	t.Helper()
	rows := r.envelope(t).Data.([]any)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i], _ = row.(map[string]any)["id"].(string)
	}
	return out
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestModelsListOrderByTotal(t *testing.T) {
	// --order-by total sorts by combined input+output price, ascending by default;
	// --reverse flips it. Neither matches the default newest-first order.
	newScenario(t, pricedModelsServer(t))

	asc := runCLI("--json", "models", "list", "--provider", "acme", "--order-by", "total")
	if asc.code != codeOK {
		t.Fatalf("models list --order-by total exit = %d, stderr=%q", asc.code, asc.stderr)
	}
	assertOrder(t, modelIDOrder(t, asc), []string{"cheap", "mid", "pricey"})

	desc := runCLI("--json", "models", "list", "--provider", "acme", "--order-by", "total", "--reverse")
	if desc.code != codeOK {
		t.Fatalf("models list --order-by total --reverse exit = %d, stderr=%q", desc.code, desc.stderr)
	}
	assertOrder(t, modelIDOrder(t, desc), []string{"pricey", "mid", "cheap"})
}

func TestModelsListDefaultNewestFirst(t *testing.T) {
	// With no --order-by the default remains newest release first.
	newScenario(t, pricedModelsServer(t))

	got := runCLI("--json", "models", "list", "--provider", "acme")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	assertOrder(t, modelIDOrder(t, got), []string{"mid", "pricey", "cheap"})
}

func TestModelsListReverseFlipsDefault(t *testing.T) {
	// --reverse with no --order-by flips the default newest-first order to oldest
	// first, exercising the default-key path rather than an explicit --order-by.
	newScenario(t, pricedModelsServer(t))

	got := runCLI("--json", "models", "list", "--provider", "acme", "--reverse")
	if got.code != codeOK {
		t.Fatalf("models list --reverse exit = %d, stderr=%q", got.code, got.stderr)
	}
	assertOrder(t, modelIDOrder(t, got), []string{"cheap", "pricey", "mid"})
}

func TestModelsListDefaultSurfacesReleasedColumn(t *testing.T) {
	// The default sort key (released) is pulled leftmost so the newest-first order is
	// legible, fixing the "looks unordered" report.
	newScenario(t, pricedModelsServer(t))

	got := runCLI("models", "list", "--provider", "acme")
	if got.code != codeOK {
		t.Fatalf("models list exit = %d, stderr=%q", got.code, got.stderr)
	}
	rel, id := strings.Index(got.stdout, "RELEASED"), strings.Index(got.stdout, "ID")
	if rel == -1 {
		t.Fatalf("models list default should surface the RELEASED column:\n%s", got.stdout)
	}
	if rel > id {
		t.Errorf("RELEASED should be the leftmost column:\n%s", got.stdout)
	}
}

func TestModelsListOrderByID(t *testing.T) {
	// --order-by id replaces the newest-first default with a plain ascending id sort.
	newScenario(t, pricedModelsServer(t))

	got := runCLI("--json", "models", "list", "--provider", "acme", "--order-by", "id")
	if got.code != codeOK {
		t.Fatalf("models list --order-by id exit = %d, stderr=%q", got.code, got.stderr)
	}
	assertOrder(t, modelIDOrder(t, got), []string{"cheap", "mid", "pricey"})
}

func TestModelsListUnknownOrderByIsUsage(t *testing.T) {
	newScenario(t, pricedModelsServer(t))

	got := runCLI("models", "list", "--provider", "acme", "--order-by", "bogus")
	if got.code != codeUsage {
		t.Fatalf("models list --order-by bogus exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "unknown field") {
		t.Errorf("expected unknown-field error, got %q", got.stderr)
	}
}

func TestModelsListFieldsAuthoritativeOverOrderColumn(t *testing.T) {
	// An explicit --fields selection is authoritative: the sort column is not injected
	// and columns are not reordered, though the rows still sort by --order-by.
	newScenario(t, pricedModelsServer(t))

	got := runCLI("models", "list", "--provider", "acme", "--fields", "id,name", "--order-by", "total")
	if got.code != codeOK {
		t.Fatalf("models list --fields --order-by exit = %d, stderr=%q", got.code, got.stderr)
	}
	if strings.Contains(got.stdout, "TOTAL") || strings.Contains(got.stdout, "RELEASED") {
		t.Errorf("--fields is authoritative; sort column must not be injected:\n%s", got.stdout)
	}
	// Rows still ordered by total ascending: cheap before pricey.
	if strings.Index(got.stdout, "cheap") > strings.Index(got.stdout, "pricey") {
		t.Errorf("rows should still order by total under --fields:\n%s", got.stdout)
	}
}

func TestAgentsListDefaultGroupsFoundFirst(t *testing.T) {
	// The default list places the not-found tail after detected agents; the default
	// id ordering holds within each group, so the missing delta-agent trails.
	newScenario(t, "", "alpha-cli", "beta-tool", "gamma-agent")

	got := runCLI("--json", "agents", "list")
	if got.code != codeOK {
		t.Fatalf("agents list exit = %d, stderr=%q", got.code, got.stderr)
	}
	order := modelIDOrder(t, got)
	if order[len(order)-1] != "delta-agent" {
		t.Errorf("missing delta-agent should trail the detected agents: %v", order)
	}
}

func TestAgentsListOrderByDropsFoundGrouping(t *testing.T) {
	// An explicit --order-by is a pure field sort with no found-first grouping, so the
	// missing delta-agent interleaves by id rather than trailing.
	newScenario(t, "", "alpha-cli", "beta-tool", "gamma-agent")

	got := runCLI("--json", "agents", "list", "--order-by", "id", "--reverse")
	if got.code != codeOK {
		t.Fatalf("agents list --order-by id --reverse exit = %d, stderr=%q", got.code, got.stderr)
	}
	assertOrder(t, modelIDOrder(t, got), []string{"gamma-agent", "delta-agent", "beta-tool", "alpha-cli"})
}

func TestProvidersListOrderByReverse(t *testing.T) {
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL)

	got := runCLI("--json", "providers", "list", "--order-by", "id", "--reverse")
	if got.code != codeOK {
		t.Fatalf("providers list --order-by id --reverse exit = %d, stderr=%q", got.code, got.stderr)
	}
	rows := got.envelope(t).Data.([]any)
	order := make([]string, len(rows))
	for i, row := range rows {
		order[i], _ = row.(map[string]any)["id"].(string)
	}
	assertOrder(t, order, []string{"openai", "google", "anthropic"})
}
