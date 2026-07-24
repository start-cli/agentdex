package agentdex

import "github.com/start-cli/agentdex/modelsdev"

// Result is the symmetric return of every List operation: the ordered items and
// any warnings the operation raised. Warnings are valid on the error return too,
// so a caller reads Warnings unconditionally and Items only when the error is nil.
type Result[T any] struct {
	Items    []T
	Warnings []Warning
}

// AgentQuery narrows and enriches an Agents.List. Filter is a case-insensitive
// substring over id and name; "" matches all. Installed narrows to agents whose
// binary is detected on this machine. Providers is the listing-wide enrichment
// set applied to provider-agnostic rows and validated at the boundary (R8). Enrich
// selects how much provider and models.dev data to attach (R4).
type AgentQuery struct {
	Filter    string
	Installed bool
	Providers []string
	Enrich    Enrich
}

// AgentGetQuery selects the enrichment level and the agnostic provider set for an
// Agents.Get.
type AgentGetQuery struct {
	Providers []string
	Enrich    Enrich
}

// ProviderQuery narrows a Providers.List by a case-insensitive substring over id
// and name.
type ProviderQuery struct {
	Filter string
}

// ModelQuery scopes and narrows a Models.List. Filter is a case-insensitive
// substring over model id and name.
type ModelQuery struct {
	Scope  ModelScope
	Filter string
}

// ModelScope selects the provider set a model listing spans. Agent scopes to a
// catalogued agent's providers ("" means not scoped by agent); Providers names
// explicit provider ids, and is also the enrichment set for an agnostic Agent.
type ModelScope struct {
	Agent     string
	Providers []string
}

// KnownAgent is one catalog entry slimmed to identity and capability: the static
// facts an agent is known by, with no resolved path or version. ID is the catalog
// map key, the single source of identity. Provider is the home-provider list,
// empty for an agnostic agent.
type KnownAgent struct {
	ID          string
	Name        string
	Bin         string
	Description string
	Homepage    string
	Provider    []string
	Agnostic    bool
}

// ResolvedPaths is a catalog directory pair after tilde, environment, and
// working-directory expansion, with existence recorded per scope. Local is "" when
// the catalog defines no local scope; the zero value of the whole struct means the
// agent has no such concept (Detection.Skills uses it when there are no skills).
type ResolvedPaths struct {
	Global       string
	GlobalExists bool
	Local        string
	LocalExists  bool
}

// Detection is what locating an agent found on this machine: its binary, version,
// and the resolved config and skills paths. Found gates only BinaryPath and
// Version; paths and providers resolve identically whether or not the binary is
// installed (R4).
type Detection struct {
	Found      bool
	BinaryPath string
	Version    string
	Config     ResolvedPaths
	Skills     ResolvedPaths
}

// Agent is the catalog's static facts joined with what detection found and, from
// EnrichProviders upward, the resolved provider set and models.dev data.
type Agent struct {
	KnownAgent
	Detection   Detection
	Providers   []string        // resolved provider ids the operation used; empty below EnrichProviders and when agnostic and unresolved
	ProviderEnv map[string]bool // API-key env var -> present; nil when models.dev was not consulted
	Enrichment  EnrichmentState
	ModelCount  int               // meaningful when Enrichment == EnrichApplied
	Models      []modelsdev.Model // populated when Enrich == EnrichFull; newest release first
}

// AgentDetail is the exact-fetch result: an Agent with the per-provider coverage
// verdict and the warnings this fetch raised (stale catalog, not-installed,
// coverage degrade, agnostic guidance).
type AgentDetail struct {
	Agent
	Coverage ProviderCoverage
	Warnings []Warning
}

// Provider is a models.dev provider with the presence of each of its API-key
// environment variables.
type Provider struct {
	modelsdev.Provider
	EnvPresent map[string]bool
}

// Model is a models.dev model with the provider it was resolved within and its
// agnostic-catalog key when it has one, else "".
type Model struct {
	modelsdev.Model
	Provider    string
	CanonicalID string
}

// Target selects which caches a Refresh forces.
type Target int

const (
	TargetCatalog Target = iota
	TargetModels
	TargetAll
)

// Refreshed reports which targets a Refresh actually re-resolved or refetched.
type Refreshed struct {
	Catalog bool
	Models  bool
}
