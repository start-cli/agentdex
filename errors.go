package agentdex

import (
	"errors"
	"fmt"
)

// The exported sentinels an agentdex caller matches with errors.Is. Each names a
// distinct condition; the human-readable detail (the id or composite involved)
// rides on the wrapping error's message, which the library owns and the CLI emits
// verbatim, adding only a remedy clause that names one of its own flags (R7).
var (
	// ErrCatalogUnavailable is the cold-offline first call: the registry was
	// unreachable and no previously resolved catalog version remained to fall back
	// on. A catalog supplied by WithCatalogDir never raises it.
	ErrCatalogUnavailable = errors.New("agentdex catalog unavailable")

	// ErrCatalogInvalid is the catalog's analog of models.dev schema drift: the
	// module was obtained and then failed to evaluate against its bundled schema.
	// The fault is the data, not the network. It wraps the loader's CUE diagnostic.
	ErrCatalogInvalid = errors.New("agentdex catalog invalid")

	// ErrModelsUnavailable is a non-schema models.dev fetch failure (unreachable
	// and uncached) on a Providers or Models operation. Agent operations degrade
	// instead of returning it.
	ErrModelsUnavailable = errors.New("models.dev unavailable")

	// ErrAgentUnknown is an agent id absent from the catalog.
	ErrAgentUnknown = errors.New("unknown agent id")

	// ErrUnknownProvider is a caller-supplied provider id models.dev does not know.
	ErrUnknownProvider = errors.New("unknown provider id")

	// ErrProvidersRequired is a model listing scoped to an agnostic agent with no
	// provider set.
	ErrProvidersRequired = errors.New("providers required for agnostic agent")

	// ErrProvidersNotAllowed is a home-provider agent given an explicit provider
	// set, in a single-target operation where the set unambiguously targets it.
	ErrProvidersNotAllowed = errors.New("providers not allowed for home-provider agent")

	// ErrMalformedModelID is a model composite with no "/".
	ErrMalformedModelID = errors.New("malformed model id")

	// ErrNotFound is a provider or model exact-get miss.
	ErrNotFound = errors.New("not found")
)

// wrapped carries a fully-formed, library-owned message while unwrapping to a
// sentinel, so errors.Is resolves the condition and Error() is the exact string a
// caller emits. fmt.Errorf cannot do both: %w at either end splices the sentinel's
// own text into the message, which the R7 wording table forbids.
type wrapped struct {
	msg      string
	sentinel error
}

func (e *wrapped) Error() string { return e.msg }
func (e *wrapped) Unwrap() error { return e.sentinel }

// errf builds a wrapped error: Error() is the formatted message verbatim, and
// errors.Is(err, sentinel) holds.
func errf(sentinel error, format string, args ...any) error {
	return &wrapped{msg: fmt.Sprintf(format, args...), sentinel: sentinel}
}
