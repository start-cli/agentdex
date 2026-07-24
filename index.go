package agentdex

import "context"

// Index is the entry point and facade returned by Open. It exposes the three noun
// services as fields and carries the cache-level operations. It is safe for
// concurrent use: the lazy catalog and models.dev resolution behind the services
// happens once under a guard, and Refresh publishes replacement state under the
// same guard (R12, R13).
type Index struct {
	Agents    AgentService
	Providers ProviderService
	Models    ModelService

	core *core
}

// Refresh forces re-resolution or refetch of the requested targets past their
// caches and publishes the refreshed state on the Index, so the operations a caller
// makes next serve the fresh data (R13). TargetAll runs its targets in order —
// catalog, then models.dev — and stops at the first failure, returning that target's
// error with Refreshed reporting only the targets that completed before it; a target
// the failure leaves unattempted is neither refreshed nor failed. A target that
// fails to refresh leaves its existing state untouched, so a failed refresh never
// costs a caller a working index. A catalog supplied by WithCatalogDir has no version
// to re-resolve, so its target is reported not-refreshed with no error.
func (x *Index) Refresh(ctx context.Context, t Target) (Refreshed, error) {
	var refreshed Refreshed
	if t == TargetCatalog || t == TargetAll {
		did, err := x.core.refreshCatalog(ctx)
		if err != nil {
			return refreshed, err
		}
		refreshed.Catalog = did
	}
	if t == TargetModels || t == TargetAll {
		if err := x.core.refreshModels(ctx); err != nil {
			return refreshed, err
		}
		refreshed.Models = true
	}
	return refreshed, nil
}

// CatalogStale reports whether the loaded agent catalog is a stale fallback: a
// re-resolution that failed after the TTL expired and reused the last resolved
// version. It resolves the catalog lazily like any catalog-touching operation, so it
// takes a context and returns ErrCatalogUnavailable on a cold-offline first call
// rather than a misleading false (R2, R12). A catalog supplied by WithCatalogDir is
// never stale.
func (x *Index) CatalogStale(ctx context.Context) (bool, error) {
	_, stale, err := x.core.resolveCatalog(ctx)
	if err != nil {
		return false, err
	}
	return stale, nil
}

// AgentService browses and fetches agents joined with detection and enrichment.
type AgentService struct{ core *core }

// ProviderService browses and fetches models.dev providers.
type ProviderService struct{ core *core }

// ModelService browses and fetches models across models.dev providers.
type ModelService struct{ core *core }
