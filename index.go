package agentdex

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

// AgentService browses and fetches agents joined with detection and enrichment.
type AgentService struct{ core *core }

// ProviderService browses and fetches models.dev providers.
type ProviderService struct{ core *core }

// ModelService browses and fetches models across models.dev providers.
type ModelService struct{ core *core }
