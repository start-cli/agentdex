package agentdex

import (
	"context"
	"errors"
	"fmt"

	"github.com/start-cli/agentdex/internal/catalog"
)

// loadCatalog drives the internal loader and maps its internal representation
// into the public Catalog. It is the seam document 03 wraps as the exported
// LoadCatalog (with Options and a preloaded-catalog fast path); the import
// direction stays one-way because internal/catalog never imports this package.
//
// stale reports that the loader reused the last resolved version after a failed
// re-resolution; the catalog is still usable and a caller may warn.
func loadCatalog(ctx context.Context, loader *catalog.Loader) (cat *Catalog, stale bool, err error) {
	res, err := loader.Load(ctx)
	if err != nil {
		if errors.Is(err, catalog.ErrUnavailable) {
			return nil, false, fmt.Errorf("%w: %w", ErrCatalogUnavailable, err)
		}
		return nil, false, err
	}
	return fromInternalCatalog(res.Catalog), res.Stale, nil
}

// fromInternalCatalog maps the loader's internal representation into the public
// Catalog. The field sets are identical by contract, so the mapping is
// mechanical; defining it here (rather than aliasing an internal type) keeps the
// public types owned by the public package, where Catalog can carry methods.
func fromInternalCatalog(ic *catalog.Catalog) *Catalog {
	agents := make(map[string]KnownAgent, len(ic.Agents))
	for id, a := range ic.Agents {
		ka := KnownAgent{
			ID:          a.ID,
			Name:        a.Name,
			Bin:         a.Bin,
			Description: a.Description,
			Config:      PathPair(a.Config),
			Provider:    a.Provider,
			Homepage:    a.Homepage,
		}
		if a.Skills != nil {
			pp := PathPair(*a.Skills)
			ka.Skills = &pp
		}
		if a.Version != nil {
			vp := VersionProbe(*a.Version)
			ka.Version = &vp
		}
		agents[id] = ka
	}
	return &Catalog{Agents: agents}
}
