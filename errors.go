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
