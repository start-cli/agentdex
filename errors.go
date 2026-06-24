package agentdex

import "errors"

// ErrCatalogUnavailable is returned when the agent catalog cannot be loaded:
// no network and no previously resolved version to fall back on. The remaining
// sentinel errors are defined alongside the detection engine (document 03).
var ErrCatalogUnavailable = errors.New("agent catalog unavailable")
