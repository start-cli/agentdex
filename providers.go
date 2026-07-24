package agentdex

import (
	"context"
	"log/slog"
	"strings"
)

// List browses the models.dev providers, narrowed by a case-insensitive substring
// over id and name, and returns them in the library's default order, by id (R14).
// It loads no agent catalog and raises no warnings; a models.dev outage is
// ErrModelsUnavailable and recognisable schema drift propagates wrapping
// modelsdev.ErrModelsSchema (R7, R12).
func (s ProviderService) List(ctx context.Context, q ProviderQuery) (Result[Provider], error) {
	c := s.core
	cat, err := c.modelsClient().Catalog(ctx)
	if err != nil {
		return Result[Provider]{}, mapModelsErr(err)
	}

	needle := strings.ToLower(q.Filter)
	ids := sortedKeys(cat.Providers)
	items := make([]Provider, 0, len(ids))
	for _, id := range ids {
		p := cat.Providers[id]
		if needle != "" && !matchesFilter(p.ID, p.Name, needle) {
			continue
		}
		items = append(items, Provider{Provider: p, EnvPresent: c.envPresence(p.Env)})
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "providers listed", slog.Int("count", len(items)))
	return Result[Provider]{Items: items}, nil
}

// Get returns one models.dev provider selected exactly by its id, with the presence
// of each of its API-key environment variables. An unknown id is ErrNotFound; an
// outage is ErrModelsUnavailable and schema drift propagates (R7). It loads no
// agent catalog, so it carries no warnings channel (R6).
func (s ProviderService) Get(ctx context.Context, id string) (Provider, error) {
	c := s.core
	p, found, err := c.modelsClient().Provider(ctx, id)
	if err != nil {
		return Provider{}, mapModelsErr(err)
	}
	if !found {
		return Provider{}, errf(ErrNotFound, "no models.dev provider %q", id)
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "provider resolved", slog.String("provider", id))
	return Provider{Provider: p, EnvPresent: c.envPresence(p.Env)}, nil
}

// envPresence reads whether each API-key variable is set, through the captured
// boundary lookup, taking only presence and never the value (R10). The map is
// non-nil even for a provider that declares no variables: a provider is always in
// hand when this is called, so its env presence is a resolved fact, not the
// "models.dev not consulted" nil an Agent's ProviderEnv carries.
func (c *core) envPresence(env []string) map[string]bool {
	present := make(map[string]bool, len(env))
	for _, name := range env {
		_, ok := c.envLookup(name)
		present[name] = ok
	}
	return present
}

// matchesFilter reports whether a case-insensitive needle occurs in an id or name.
// The needle is expected pre-lowered; an empty needle is handled by the caller.
func matchesFilter(id, name, needle string) bool {
	return strings.Contains(strings.ToLower(id), needle) || strings.Contains(strings.ToLower(name), needle)
}
