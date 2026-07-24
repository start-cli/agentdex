package catalog

// LoadDir loads, validates, and decodes an agent catalog from a local CUE module
// directory, bypassing the registry entirely. Validation is by evaluation against
// the schema.cue that travels with the module, the same step a fetched module goes
// through, so an entry the schema rejects fails with ErrInvalidCatalog. The
// directory source resolves no version and reaches no network, so it is never
// stale and never raises ErrUnavailable.
func LoadDir(dir string) (*Catalog, error) {
	return loadCatalogModule(dir)
}
