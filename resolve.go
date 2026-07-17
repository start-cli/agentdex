package agentdex

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/start-cli/agentdex/internal/match"
	"github.com/start-cli/agentdex/modelsdev"
)

// ResolveModel maps a fuzzy query (e.g. "sonnet") to a models.dev model within
// the given providers, applying the shared none/one/many rule: exact models.dev
// id, then exact name (case-insensitive), then a unique substring or prefix;
// ambiguity returns ErrModelAmbiguous with the candidate ids and no match
// returns ErrModelNotFound. An id absent from the catalog returns ErrAgentUnknown.
//
// providers is the search set. Home-provider callers pass the catalog provider
// list; agnostic callers pass the caller-supplied ids. An empty providers list
// for an agnostic agent is ErrProvidersRequired; for a home-provider agent an
// empty list falls back to the catalog provider list. Absent providers are
// skipped silently; callers that need unknown-id rejection should call
// ValidateCallerProviders first on caller-supplied sets.
//
// It returns the matched provider Model, the real models.dev provider id it
// resolved within, and the model's canonical (provider-agnostic) id. The
// canonical id is read, not minted: the agnostic map is probed under
// providerID + "/" + modelKey and the actual key found is returned, or "" when
// the agnostic map has no entry. The composite is only ever a lookup probe;
// Model.ID keeps its source-id meaning, so the library never surfaces an id that
// does not exist in models.dev. The provider id is returned so a caller holding
// only the Model — which carries no provider field — has provider context without
// parsing the opaque canonical id.
//
// mc must be non-nil: unlike WithModels, where a nil client means "attach
// nothing", ResolveModel needs a client to do anything, so passing nil is a
// programmer error and panics.
func (c *Catalog) ResolveModel(ctx context.Context, agentID, query string, mc *modelsdev.Client, providers []string) (m modelsdev.Model, providerID string, canonicalID string, err error) {
	if mc == nil {
		panic("agentdex: ResolveModel requires a non-nil *modelsdev.Client")
	}
	ka, ok := c.Agents[agentID]
	if !ok {
		return modelsdev.Model{}, "", "", ErrAgentUnknown
	}
	search := providers
	if len(search) == 0 {
		if ka.Agnostic {
			return modelsdev.Model{}, "", "", fmt.Errorf("%w: %q is provider-agnostic; supply providers (e.g. --provider)", ErrProvidersRequired, agentID)
		}
		search = ka.Provider
	}
	// A duplicated id would add every one of its models twice, turning a unique
	// query into a spurious ErrModelAmbiguous.
	search = dedupeIDs(search)

	type candidate struct {
		model      modelsdev.Model
		providerID string
		modelKey   string
	}
	var (
		cands []candidate
		items []match.Item
	)
	for _, pid := range search {
		p, found, perr := mc.Provider(ctx, pid)
		if perr != nil {
			return modelsdev.Model{}, "", "", perr
		}
		if !found {
			continue
		}
		for _, key := range sortedKeys(p.Models) {
			model := p.Models[key]
			cands = append(cands, candidate{model: model, providerID: pid, modelKey: key})
			items = append(items, match.Item{ID: model.ID, Name: model.Name})
		}
	}

	outcome, idx, ambiguous := match.Match(query, items)
	switch outcome {
	case match.None:
		return modelsdev.Model{}, "", "", fmt.Errorf("%w: %q", ErrModelNotFound, query)
	case match.Ambiguous:
		return modelsdev.Model{}, "", "", fmt.Errorf("%w: %q matched %s", ErrModelAmbiguous, query, strings.Join(ambiguous, ", "))
	}

	chosen := cands[idx]
	cat, err := mc.Catalog(ctx)
	if err != nil {
		return modelsdev.Model{}, "", "", err
	}
	composite := chosen.providerID + "/" + chosen.modelKey
	if _, ok := cat.Models[composite]; ok {
		canonicalID = composite
	}
	return chosen.model, chosen.providerID, canonicalID, nil
}

func sortedKeys(m map[string]modelsdev.Model) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
