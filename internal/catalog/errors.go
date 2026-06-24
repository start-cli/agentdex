package catalog

import "errors"

var (
	// ErrUnavailable signals that the catalog could not be loaded at all: no
	// network and no previously resolved version to fall back on. The root
	// package maps this to agentdex.ErrCatalogUnavailable.
	ErrUnavailable = errors.New("catalog unavailable")

	// ErrInvalidCatalog signals that a fetched catalog module failed to load,
	// validate against its bundled schema, or decode. It is distinct from
	// ErrUnavailable: the data was reachable but does not satisfy the contract.
	ErrInvalidCatalog = errors.New("invalid catalog")
)
