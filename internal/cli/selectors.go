package cli

import (
	"context"
	"sort"

	"github.com/start-cli/agentdex"
	"github.com/start-cli/agentdex/internal/match"
	"github.com/start-cli/agentdex/modelsdev"
)

// catalogIDs returns the catalog's agent ids sorted, for selector candidate sets
// and the "valid ids" message on an unknown query.
func catalogIDs(cat *agentdex.Catalog) []string {
	ids := make([]string, 0, len(cat.Agents))
	for id := range cat.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// matchAgent resolves an agent query against the catalog with the shared
// none/one/many rule. On Unique it returns the matched id; on Ambiguous the
// candidate ids; on None neither.
func matchAgent(cat *agentdex.Catalog, query string) (match.Outcome, string, []string) {
	ids := catalogIDs(cat)
	items := make([]match.Item, len(ids))
	for i, id := range ids {
		items[i] = match.Item{ID: id, Name: cat.Agents[id].Name}
	}
	outcome, idx, candidates := match.Match(query, items)
	if outcome == match.Unique {
		return outcome, items[idx].ID, nil
	}
	return outcome, "", candidates
}

// matchProvider resolves a query against a reachable models.dev's providers by id
// and name, the only axis an uncatalogued agent query can resolve against. It
// loads the merged catalog through the client, so a models.dev outage surfaces as
// the returned error and the caller reports transient rather than asserting a
// verdict. On Unique it returns the matched provider.
func matchProvider(ctx context.Context, client *modelsdev.Client, query string) (match.Outcome, modelsdev.Provider, []string, error) {
	cat, err := client.Catalog(ctx)
	if err != nil {
		return match.None, modelsdev.Provider{}, nil, err
	}
	ids := make([]string, 0, len(cat.Providers))
	for id := range cat.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]match.Item, len(ids))
	for i, id := range ids {
		items[i] = match.Item{ID: id, Name: cat.Providers[id].Name}
	}
	outcome, idx, candidates := match.Match(query, items)
	if outcome == match.Unique {
		return outcome, cat.Providers[items[idx].ID], nil, nil
	}
	return outcome, modelsdev.Provider{}, candidates, nil
}
