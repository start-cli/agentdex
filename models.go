package agentdex

import (
	"context"
	"log/slog"
	"strings"

	"github.com/start-cli/agentdex/modelsdev"
)

// List browses models across the scoped models.dev providers, narrowed by a
// case-insensitive substring over model id and name, and returns them in the
// library's default order, newest release first (R14). The scope comes from
// ModelQuery.Scope: an --agent resolves to that agent's provider set by the
// agnostic/home rules (R8), explicit Providers name a direct set, and an empty
// scope spans every provider models.dev knows. Each returned Model carries its
// provider and, when the composite is a key in the agnostic map, its canonical id
// (R9). Stale-catalog warnings from an agent scope ride the return, valid on the
// error path too (R6).
func (s ModelService) List(ctx context.Context, q ModelQuery) (Result[Model], error) {
	c := s.core
	mc := c.modelsClient()

	providers, warnings, err := c.resolveModelScope(ctx, mc, q.Scope)
	if err != nil {
		return Result[Model]{Warnings: warnings}, err
	}

	agnostic, err := mc.Catalog(ctx)
	if err != nil {
		return Result[Model]{Warnings: warnings}, mapModelsErr(err)
	}

	needle := strings.ToLower(q.Filter)
	var items []Model
	for _, pid := range providers {
		p, found, perr := mc.Provider(ctx, pid)
		if perr != nil {
			return Result[Model]{Warnings: warnings}, mapModelsErr(perr)
		}
		if !found {
			c.logger.LogAttrs(ctx, slog.LevelDebug, "model provider absent from models.dev", slog.String("provider", pid))
			continue
		}
		for _, key := range sortedKeys(p.Models) {
			m := p.Models[key]
			if needle != "" && !matchesFilter(m.ID, m.Name, needle) {
				continue
			}
			composite := pid + "/" + key
			items = append(items, Model{Model: m, Provider: pid, CanonicalID: canonicalID(agnostic, composite)})
		}
	}
	sortModels(items)
	return Result[Model]{Items: items, Warnings: warnings}, nil
}

// Get returns one model selected exactly by its composite provider-id/model-id. The
// composite splits on the first slash only: the prefix is the provider id, the whole
// remainder is the model key, which may itself contain slashes (R9). A value with no
// slash is ErrMalformedModelID; an unknown provider or model key is ErrNotFound; an
// outage is ErrModelsUnavailable and schema drift propagates (R7). It loads no agent
// catalog, so it carries no warnings channel (R6).
func (s ModelService) Get(ctx context.Context, composite string) (Model, error) {
	c := s.core
	pid, key, ok := strings.Cut(composite, "/")
	if !ok {
		return Model{}, errf(ErrMalformedModelID, "model id %q must be provider-id/model-id", composite)
	}

	mc := c.modelsClient()
	p, found, err := mc.Provider(ctx, pid)
	if err != nil {
		return Model{}, mapModelsErr(err)
	}
	if !found {
		return Model{}, errf(ErrNotFound, "no model %q: unknown provider %q", composite, pid)
	}
	m, ok := p.Models[key]
	if !ok {
		return Model{}, errf(ErrNotFound, "no model %q in provider %q", composite, pid)
	}

	agnostic, err := mc.Catalog(ctx)
	if err != nil {
		return Model{}, mapModelsErr(err)
	}
	canonical := canonicalID(agnostic, composite)
	c.logger.LogAttrs(ctx, slog.LevelDebug, "model resolved",
		slog.String("composite", composite), slog.String("provider", pid), slog.String("canonical", canonical))
	return Model{Model: m, Provider: pid, CanonicalID: canonical}, nil
}

// resolveModelScope resolves the provider set a listing spans from the scope,
// enforcing the agnostic/home rules and validating caller-supplied ids in every
// role they play (R8). A caller id models.dev does not know is ErrUnknownProvider; a
// models.dev outage is not a rejection — validation is skipped and the outage
// surfaces on the listing fetch as ErrModelsUnavailable — while recognisable schema
// drift propagates. An agent scope resolves the catalog, so a stale fallback rides
// the return, on the error path too (R6).
func (c *core) resolveModelScope(ctx context.Context, mc *modelsdev.Client, scope ModelScope) ([]string, []Warning, error) {
	caller := dedupeIDs(scope.Providers)

	if scope.Agent != "" {
		cat, stale, err := c.resolveCatalog(ctx)
		if err != nil {
			return nil, nil, err
		}
		var warnings []Warning
		if stale {
			warnings = append(warnings, staleWarning())
		}
		ka, ok := cat.Agents[scope.Agent]
		if !ok {
			return nil, warnings, errf(ErrAgentUnknown, "no agent %q", scope.Agent)
		}
		if ka.Agnostic {
			if len(caller) == 0 {
				return nil, warnings, errf(ErrProvidersRequired, "providers required for agnostic agent: %q is provider-agnostic", scope.Agent)
			}
			if verr := c.validateModelProviders(ctx, mc, caller); verr != nil {
				return nil, warnings, verr
			}
			c.logger.LogAttrs(ctx, slog.LevelDebug, "model scope resolved",
				slog.String("agent", scope.Agent), slog.Any("providers", caller))
			return caller, warnings, nil
		}
		if len(caller) > 0 {
			return nil, warnings, errf(ErrProvidersNotAllowed, "agent %q has catalog providers", scope.Agent)
		}
		set := append([]string(nil), ka.Provider...)
		c.logger.LogAttrs(ctx, slog.LevelDebug, "model scope resolved",
			slog.String("agent", scope.Agent), slog.Any("providers", set))
		return set, warnings, nil
	}

	if len(caller) > 0 {
		if verr := c.validateModelProviders(ctx, mc, caller); verr != nil {
			return nil, nil, verr
		}
		c.logger.LogAttrs(ctx, slog.LevelDebug, "model scope resolved", slog.Any("providers", caller))
		return caller, nil, nil
	}

	cat, err := mc.Catalog(ctx)
	if err != nil {
		return nil, nil, mapModelsErr(err)
	}
	ids := sortedKeys(cat.Providers)
	c.logger.LogAttrs(ctx, slog.LevelDebug, "model scope resolved", slog.Int("providers", len(ids)))
	return ids, nil, nil
}

// validateModelProviders rejects an unknown caller id and propagates recognisable
// schema drift, but treats an outage as a non-rejection: on an unreachable models.dev
// the ids stand and the listing fetch reports the outage instead (R8).
func (c *core) validateModelProviders(ctx context.Context, mc *modelsdev.Client, ids []string) error {
	switch kind, err := c.validateProviders(ctx, mc, ids); kind {
	case provUnknown, provSchema:
		return err
	default:
		return nil
	}
}

// canonicalID reports the composite when it names a key in the agnostic model map,
// else "" — the agnostic-catalog key a model carries when it has one (R9).
func canonicalID(agnostic *modelsdev.Catalog, composite string) string {
	if _, ok := agnostic.Models[composite]; ok {
		return composite
	}
	return ""
}
