package agentdex

import "errors"

// ErrCatalogUnavailable is returned when the agent catalog cannot be loaded:
// no network and no previously resolved version to fall back on.
var ErrCatalogUnavailable = errors.New("agent catalog unavailable")

// ErrAgentUnknown is returned by DetectOne and ResolveModel when the id names no
// catalog agent. It is the only "not a catalog agent" signal; a known agent whose
// binary is not installed is a normal result, not this error.
var ErrAgentUnknown = errors.New("unknown agent id")

// ErrModelAmbiguous is returned by ResolveModel when a fuzzy query matches more
// than one model. The error message carries the candidate ids.
var ErrModelAmbiguous = errors.New("model query matched multiple models")

// ErrModelNotFound is returned by ResolveModel when a fuzzy query matches no
// model in the agent's provider set.
var ErrModelNotFound = errors.New("model query matched no models")

// ErrProvidersRequired is returned when provider-related enrichment is demanded
// for a provider-agnostic agent and no caller providers were supplied. DetectOne
// returns it when a models.dev client is attached without WithProviders;
// ResolveModel returns it for an empty providers argument on an agnostic agent.
// Multi-agent Detect soft-skips enrichment instead of failing.
var ErrProvidersRequired = errors.New("providers required for agnostic agent")

// ErrUnknownProvider is returned when a caller-supplied provider id is not a
// models.dev provider. Catalog provider lists on home-provider agents are not
// validated this way; absent catalog providers remain a coverage fact.
var ErrUnknownProvider = errors.New("unknown provider id")
