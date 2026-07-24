// Package agentdex indexes three kinds of data and serves them through one
// coherent surface: the AI coding agents in a published catalog, the models.dev
// providers that power them, and the models those providers offer. It owns the
// outside of an agent — identity, location, paths, version, capability — and never
// reads an agent's internal configuration.
//
// # Opening an index
//
// Open constructs an *Index, the entry point and facade. It performs no network
// I/O; the agent catalog and the models.dev catalog are resolved lazily on the
// first operation that needs each, once, behind a guard, and the Index is safe for
// concurrent use. Options configure the catalog source, the caches, detection, and
// the boundary inputs (environment lookup and working directory); WithLogger opts
// the library into structured debug logging.
//
//	idx, err := agentdex.Open(ctx)
//	if err != nil { return err }
//	res, err := idx.Agents.List(ctx, agentdex.AgentQuery{Enrich: agentdex.EnrichCount})
//
// # Services
//
// The Index exposes three noun services as fields — Agents, Providers, Models —
// each with exactly two operations: a browse List returning a Result[T] of items
// and warnings, and an exact Get. Detection is a property of an agent, reported on
// Agent.Detection, not a top-level verb. The Index also carries the cache-level
// operations Refresh and CatalogStale.
//
// # Enrichment levels
//
// Enrich is the single demand axis for an agent operation, each level a superset
// of the one below:
//
//   - EnrichNone: catalog and detection facts only; silent and offline.
//   - EnrichProviders: adds the resolved provider set. Offline for a home-provider
//     agent (catalog data); contacts models.dev for an agnostic agent, to validate
//     the caller's provider ids.
//   - EnrichCount: adds ProviderEnv and ModelCount, and coverage on Agents.Get.
//     models.dev-backed for every agent a provider set was resolved for.
//   - EnrichFull: adds the full Models list, for the same fetch as EnrichCount.
//
// Installation status gates none of this: a catalogued agent whose binary is
// absent enriches exactly as an installed one, so a caller can ask what an agent
// offers before installing it. EnrichmentState records the outcome on each Agent —
// applied, not-requested, not-applicable (an agnostic agent with no providers), or
// degraded (models.dev could not fill it).
//
// # Warnings and errors
//
// Warnings are structured: each carries a Kind a caller branches on and a Msg it
// emits verbatim. Result and AgentDetail carry a Warnings slice, valid on the error
// return as well as the success return, so a warning raised before a failure still
// reaches the caller. Errors are the sentinels in this package, matched with
// errors.Is: ErrCatalogUnavailable, ErrCatalogInvalid, ErrModelsUnavailable,
// ErrAgentUnknown, ErrUnknownProvider, ErrProvidersRequired, ErrProvidersNotAllowed,
// ErrMalformedModelID, and ErrNotFound. Recognisable models.dev schema drift wraps
// modelsdev.ErrModelsSchema wherever it surfaces.
//
// The detection engine is data-driven: one generic path walks the agent catalog
// and applies the same steps to every entry, so adding an agent is a catalog edit,
// not a code change.
package agentdex
