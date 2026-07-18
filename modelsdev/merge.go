package modelsdev

import (
	"fmt"
	"strings"
)

// merge enriches provider models with the benchmarks and weights that upstream
// keeps only in the provider-agnostic map. It is driven agnostic-first: it
// iterates Catalog.Models, decomposes each real path-style id into its provider
// and model parts, and copies that entry's benchmarks and weights onto the
// matching provider model. Every key touched is therefore a real models.dev id —
// no composite id is constructed and Model.ID is never rewritten, so the merge
// cannot mint an id that does not exist upstream. First-party models decompose to
// their short provider key and receive the data; aggregator and proxy models,
// whose keys are already path-bearing, have no agnostic id decomposing to them
// and receive nothing, which is correct since they carry no benchmarks of their
// own. merge mutates cat in place.
func merge(cat *Catalog) {
	for id, agnostic := range cat.Models {
		providerID, modelKey, ok := strings.Cut(id, "/")
		if !ok || providerID == "" || modelKey == "" {
			continue
		}
		provider, ok := cat.Providers[providerID]
		if !ok {
			continue
		}
		model, ok := provider.Models[modelKey]
		if !ok {
			continue
		}
		model.Benchmarks = agnostic.Benchmarks
		model.Weights = agnostic.Weights
		provider.Models[modelKey] = model
	}
}

// validateTopLevel is the gross-drift guard applied on every fetch: both
// top-level maps must be non-empty. A violation means a wholesale schema change
// renamed or emptied the maps, decoding to a hollow catalog that would silently
// blank out enrichment.
func validateTopLevel(cat *Catalog) error {
	if len(cat.Models) == 0 || len(cat.Providers) == 0 {
		return fmt.Errorf("top-level models or providers map empty: %w", ErrModelsSchema)
	}
	return nil
}

// validateProvider applies the per-model required-field check, scoped to a
// single provider a caller actually requested. A model is malformed when its id
// is empty — the only per-model field upstream guarantees. A zero limit is not
// malformed: media-generation models (image, audio, video) legitimately carry no
// token limit and often no pricing, so a limit was never a real invariant to gate
// on. Gross drift that empties the model map is caught by validateTopLevel. The
// check runs only for requested providers, never across the full upstream
// catalog, so an unrelated provider's malformed model cannot break enrichment.
func validateProvider(p Provider) error {
	for key, m := range p.Models {
		if m.ID == "" {
			return fmt.Errorf("provider %q model %q malformed: %w", p.ID, key, ErrModelsSchema)
		}
	}
	return nil
}
