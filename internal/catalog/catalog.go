// Package catalog fetches the agentdex agent catalog from the CUE Central
// Registry, validates it by evaluating the fetched module against its bundled
// schema, caches the resolved module version, and decodes the catalog into an
// internal representation. The root package agentdex maps that representation
// into its public Catalog types; this package never imports the root package,
// keeping the dependency one-way.
package catalog

// Catalog is the loaded set of known agents in this package's internal
// representation, keyed by catalog id. The root package maps it into the public
// agentdex.Catalog.
type Catalog struct {
	Agents map[string]KnownAgent
}

// KnownAgent is one decoded catalog entry. ID is populated from the catalog map
// key by the loader; the schema's #KnownAgent has no id field.
type KnownAgent struct {
	ID          string
	Name        string
	Bin         string
	Description string
	Config      PathPair
	Skills      *PathPair
	Version     *VersionProbe
	Provider    []string
	Homepage    string
}

// PathPair is a catalog global/local directory pair before any expansion.
type PathPair struct {
	Global string
	Local  string
}

// VersionProbe describes how to read an agent's version from its binary.
type VersionProbe struct {
	Args    []string
	Pattern string
}
