package agentdex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/start-cli/agentdex/internal/catalog"
	"github.com/start-cli/agentdex/modelsdev"
)

// Get returns detection detail for one agent, selected exactly by its catalog id.
// It joins the catalog facts with what detection found and, from EnrichProviders
// upward, the resolved provider set and models.dev enrichment; it never fails on a
// coverage verdict (R5) and reports a not-installed or agnostic-without-providers
// agent as data plus a warning rather than an error (R4, R8). Warnings ride on the
// error return too, so a stale catalog or accumulated warning survives a failure
// (R6).
func (s AgentService) Get(ctx context.Context, id string, q AgentGetQuery) (AgentDetail, error) {
	c := s.core
	cat, stale, err := c.resolveCatalog(ctx)
	if err != nil {
		return AgentDetail{}, err
	}
	var warnings []Warning
	if stale {
		warnings = append(warnings, staleWarning())
	}

	ka, ok := cat.Agents[id]
	if !ok {
		return AgentDetail{Warnings: warnings}, errf(ErrAgentUnknown, "no agent %q", id)
	}

	caller := dedupeIDs(q.Providers)
	// A home-provider agent given an explicit set contradicts catalog data in hand,
	// so it is rejected at every level, before any level-dependent resolution (R8).
	if !ka.Agnostic && len(caller) > 0 {
		return AgentDetail{Warnings: warnings}, errf(ErrProvidersNotAllowed, "agent %q has catalog providers", id)
	}

	detail := AgentDetail{Agent: c.detect(ctx, ka)}
	if !detail.Detection.Found {
		warnings = append(warnings, notInstalledWarning(id))
	}

	// EnrichNone resolves nothing provider-related for any agent, so the agnostic
	// case does not arise and no models.dev round-trip is made (R4).
	if q.Enrich == EnrichNone {
		detail.Enrichment = EnrichNotRequested
		detail.Warnings = warnings
		return detail, nil
	}

	// Agnostic without a provider set is decided from catalog data alone: outside
	// facts only, not-applicable, guidance warning, no models.dev at any level (R8, R12).
	if ka.Agnostic && len(caller) == 0 {
		detail.Enrichment = EnrichNotApplicable
		warnings = append(warnings, providersRequiredWarning(id))
		detail.Warnings = warnings
		return detail, nil
	}

	providers := ka.Provider
	if ka.Agnostic {
		providers = caller
	}
	detail.Providers = providers

	mc := c.modelsClient()

	// Agnostic caller ids are validated against models.dev before they are reported;
	// an unknown id is rejected, a drift or outage degrades rather than rejects (R8).
	validation := provOK
	if ka.Agnostic {
		var verr error
		validation, verr = c.validateProviders(ctx, mc, providers)
		if validation == provUnknown {
			detail.Warnings = warnings
			return detail, verr
		}
	}

	if q.Enrich == EnrichProviders {
		switch validation {
		case provUnreachable:
			detail.Enrichment = EnrichDegraded
			warnings = append(warnings, modelsUnreachableGetWarning())
		case provSchema:
			detail.Enrichment = EnrichDegraded
		default:
			detail.Enrichment = EnrichApplied
		}
		detail.Warnings = warnings
		return detail, nil
	}

	// EnrichCount and EnrichFull: probe coverage and enrich from the present
	// providers. Coverage is the verdict on this agent's provider set as data (R5).
	wantModels := q.Enrich == EnrichFull
	cov := c.probeCoverage(ctx, mc, providers, wantModels)
	detail.Coverage = cov.cov
	detail.Enrichment = cov.state
	if cov.state == EnrichDegraded {
		// Unreachable is a warning; recognisable drift is carried by the coverage
		// verdict and Coverage.Err, which the caller maps to a failure (R6).
		if cov.cov.Status == CoverageUnreachable {
			warnings = append(warnings, modelsUnreachableGetWarning())
		}
		detail.Warnings = warnings
		return detail, nil
	}
	detail.ProviderEnv = cov.providerEnv
	detail.ModelCount = len(cov.models)
	if wantModels {
		sortModelsNewest(cov.models)
		detail.Models = cov.models
	}
	if cov.cov.Status == CoverageSomePresent {
		warnings = append(warnings, someProvidersAbsentWarning(cov.cov.Absent))
	}
	detail.Warnings = warnings
	return detail, nil
}

// List browses the catalog with local detection and, from EnrichProviders upward,
// the resolved provider set and models.dev enrichment. It fans detection out
// across catalog entries concurrently (R12), narrows by Installed and Filter,
// probes no per-agent coverage (R4), and returns the agents in the library's
// default order, by id (R14). AgentQuery.Providers is validated once at the
// boundary at every level; an unknown id fails the whole listing (R8).
func (s AgentService) List(ctx context.Context, q AgentQuery) (Result[Agent], error) {
	c := s.core
	cat, stale, err := c.resolveCatalog(ctx)
	if err != nil {
		return Result[Agent]{}, err
	}
	var warnings []Warning
	if stale {
		warnings = append(warnings, staleWarning())
	}

	providers := dedupeIDs(q.Providers)
	needModels := q.Enrich >= EnrichCount

	var mc *modelsdev.Client
	if needModels || len(providers) > 0 {
		mc = c.modelsClient()
	}

	degrade := degradeNone
	var degradeErr error

	// Listing-wide validation at the boundary, every level: an unknown id fails
	// regardless of which agents the query returns; a drift or outage degrades (R8).
	if len(providers) > 0 {
		kind, verr := c.validateProviders(ctx, mc, providers)
		switch kind {
		case provUnknown:
			return Result[Agent]{Warnings: warnings}, verr
		case provSchema:
			degrade, degradeErr = degradeSchema, verr
		case provUnreachable:
			degrade, degradeErr = degradeUnreachable, verr
		}
	}

	// Reachability probe for the model counts. Gross and per-model drift are caught
	// during per-agent enrichment; only a non-schema outage is decided here.
	if needModels && degrade == degradeNone {
		if _, cerr := mc.Catalog(ctx); cerr != nil && !errors.Is(cerr, modelsdev.ErrModelsSchema) {
			degrade, degradeErr = degradeUnreachable, cerr
		}
	}

	agents, err := c.detectAll(ctx, cat)
	if err != nil {
		return Result[Agent]{Warnings: warnings}, err
	}
	if q.Installed {
		agents = keepInstalled(agents)
	}

	switch {
	case q.Enrich == EnrichNone:
		for i := range agents {
			agents[i].Enrichment = EnrichNotRequested
		}
	case q.Enrich == EnrichProviders:
		for i := range agents {
			c.resolveListProviders(&agents[i], providers, degrade)
		}
	case degrade != degradeNone:
		for i := range agents {
			c.degradeListAgent(&agents[i], providers)
		}
		warnings = append(warnings, listDegradeWarning(degrade, degradeErr))
	default:
		wantModels := q.Enrich == EnrichFull
		for i := range agents {
			if serr := c.enrichListAgent(ctx, mc, &agents[i], providers, wantModels); serr != nil && degrade == degradeNone {
				if errors.Is(serr, modelsdev.ErrModelsSchema) {
					degrade, degradeErr = degradeSchema, serr
				} else {
					degrade, degradeErr = degradeUnreachable, serr
				}
			}
		}
		if degrade != degradeNone {
			for i := range agents {
				c.degradeListAgent(&agents[i], providers)
			}
			warnings = append(warnings, listDegradeWarning(degrade, degradeErr))
		}
	}

	if q.Filter != "" {
		agents = filterAgents(agents, q.Filter)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })
	return Result[Agent]{Items: agents, Warnings: warnings}, nil
}

// degradeMode records why a listing could not attach model data: a models.dev
// outage or recognisable schema drift. It selects the list-level degrade warning.
type degradeMode int

const (
	degradeNone degradeMode = iota
	degradeUnreachable
	degradeSchema
)

// provValidation classifies a provider-id validation against models.dev.
type provValidation int

const (
	provOK provValidation = iota
	provUnknown
	provUnreachable
	provSchema
)

// validateProviders checks each id against models.dev. A genuine absent id is an
// unknown caller provider (ErrUnknownProvider); a drift or an outage is not a
// rejection but a condition the caller degrades on (R8).
func (c *core) validateProviders(ctx context.Context, mc *modelsdev.Client, ids []string) (provValidation, error) {
	for _, pid := range ids {
		_, found, err := mc.Provider(ctx, pid)
		switch {
		case errors.Is(err, modelsdev.ErrModelsSchema):
			return provSchema, err
		case err != nil:
			return provUnreachable, err
		case !found:
			return provUnknown, errf(ErrUnknownProvider, "unknown provider id: %q", pid)
		}
	}
	return provOK, nil
}

// covResult is one agent's coverage verdict plus the data enrichment gathered in
// the same pass: provider-env presence and the present providers' models.
type covResult struct {
	cov         ProviderCoverage
	providerEnv map[string]bool
	models      []modelsdev.Model
	state       EnrichmentState
}

// probeCoverage probes each provider once, composing the coverage verdict and, on
// a reachable and parseable models.dev, the provider-env presence and the present
// providers' models. A drift or an outage short-circuits to the fault verdict with
// EnrichDegraded; otherwise the verdict is a positive one and the enrichment is
// applied. There is no empty-set case: a home-provider set is non-empty by schema
// and an agnostic empty set never reaches here (R5).
func (c *core) probeCoverage(ctx context.Context, mc *modelsdev.Client, providers []string, wantModels bool) covResult {
	var present, absent []string
	var providerEnv map[string]bool
	for _, pid := range providers {
		p, found, err := mc.Provider(ctx, pid)
		switch {
		case errors.Is(err, modelsdev.ErrModelsSchema):
			return covResult{cov: ProviderCoverage{Status: CoverageSchemaDrift, Err: err}, state: EnrichDegraded}
		case err != nil:
			return covResult{cov: ProviderCoverage{Status: CoverageUnreachable, Err: err}, state: EnrichDegraded}
		case !found:
			absent = append(absent, pid)
		default:
			present = append(present, pid)
			for _, env := range p.Env {
				if providerEnv == nil {
					providerEnv = map[string]bool{}
				}
				_, ok := c.envLookup(env)
				providerEnv[env] = ok
			}
		}
	}

	var models []modelsdev.Model
	if len(present) > 0 {
		m, merr := mc.Models(ctx, present...)
		if merr != nil {
			// present providers passed the per-provider check above, so a fault here
			// is genuine rather than an absent provider being skipped.
			if errors.Is(merr, modelsdev.ErrModelsSchema) {
				return covResult{cov: ProviderCoverage{Status: CoverageSchemaDrift, Err: merr}, state: EnrichDegraded}
			}
			return covResult{cov: ProviderCoverage{Status: CoverageUnreachable, Err: merr}, state: EnrichDegraded}
		}
		models = m
	}

	status := CoverageAllPresent
	switch {
	case len(present) == 0:
		status = CoverageNonePresent
	case len(absent) > 0:
		status = CoverageSomePresent
	}
	return covResult{
		cov:         ProviderCoverage{Present: present, Absent: absent, Status: status},
		providerEnv: providerEnv,
		models:      models,
		state:       EnrichApplied,
	}
}

// detectAll runs every catalog entry through detection concurrently, bounded by
// maxConcurrentDetections, and returns the agents unsorted. Detection carries no
// error of its own — a version probe failure is non-fatal — so the only error is a
// cancelled or expired context, honoured even on a run no per-agent step reports.
func (c *core) detectAll(ctx context.Context, cat *catalog.Catalog) ([]Agent, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConcurrentDetections)
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		agents = make([]Agent, 0, len(cat.Agents))
	)
	for _, ka := range cat.Agents {
		wg.Add(1)
		go func(ka catalog.KnownAgent) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			a := c.detect(ctx, ka)
			mu.Lock()
			agents = append(agents, a)
			mu.Unlock()
		}(ka)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return agents, nil
}

// resolveListProviders fills a listing row's provider set at EnrichProviders: the
// catalog list for a home-provider agent (offline), the listing-wide set for an
// agnostic row, or not-applicable for an agnostic row with no set (R4, R8).
func (c *core) resolveListProviders(a *Agent, listProviders []string, degrade degradeMode) {
	if a.Agnostic && len(listProviders) == 0 {
		a.Enrichment = EnrichNotApplicable
		return
	}
	if a.Agnostic {
		a.Providers = append([]string(nil), listProviders...)
		if degrade == degradeNone {
			a.Enrichment = EnrichApplied
		} else {
			a.Enrichment = EnrichDegraded
		}
		return
	}
	a.Providers = append([]string(nil), a.Provider...)
	a.Enrichment = EnrichApplied
}

// enrichListAgent fills a listing row from models.dev at EnrichCount and above:
// the resolved provider set, provider-env presence, model count, and — when
// wantModels — the newest-first model list. A listing probes no coverage, so an
// absent provider is skipped rather than reported (R4). Any models.dev fault is
// returned for the caller to degrade the whole listing on.
func (c *core) enrichListAgent(ctx context.Context, mc *modelsdev.Client, a *Agent, listProviders []string, wantModels bool) error {
	if a.Agnostic && len(listProviders) == 0 {
		a.Enrichment = EnrichNotApplicable
		return nil
	}
	var set []string
	if a.Agnostic {
		set = append([]string(nil), listProviders...)
	} else {
		set = append([]string(nil), a.Provider...)
	}
	a.Providers = set

	var providerEnv map[string]bool
	for _, pid := range set {
		p, found, err := mc.Provider(ctx, pid)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		for _, env := range p.Env {
			if providerEnv == nil {
				providerEnv = map[string]bool{}
			}
			_, ok := c.envLookup(env)
			providerEnv[env] = ok
		}
	}
	models, err := mc.Models(ctx, set...)
	if err != nil {
		return err
	}
	sortModelsNewest(models)
	a.ProviderEnv = providerEnv
	a.ModelCount = len(models)
	if wantModels {
		a.Models = models
	}
	a.Enrichment = EnrichApplied
	return nil
}

// degradeListAgent marks a model-backed listing row as degraded: the provider set
// is still resolved, but no count, provider-env, or models could be filled. An
// agnostic row with no set is not-applicable rather than degraded (R4).
func (c *core) degradeListAgent(a *Agent, listProviders []string) {
	if a.Agnostic && len(listProviders) == 0 {
		a.Enrichment = EnrichNotApplicable
		return
	}
	if a.Agnostic {
		a.Providers = append([]string(nil), listProviders...)
	} else {
		a.Providers = append([]string(nil), a.Provider...)
	}
	a.Enrichment = EnrichDegraded
	a.ModelCount = 0
	a.ProviderEnv = nil
	a.Models = nil
}

// keepInstalled narrows a listing to the agents whose binary was detected.
func keepInstalled(agents []Agent) []Agent {
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		if a.Detection.Found {
			out = append(out, a)
		}
	}
	return out
}

// filterAgents narrows a listing to agents whose id or name contains the filter,
// case-insensitively.
func filterAgents(agents []Agent, filter string) []Agent {
	needle := strings.ToLower(filter)
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		if matchesFilter(a.ID, a.Name, needle) {
			out = append(out, a)
		}
	}
	return out
}

// staleWarning is the single wording every catalog-resolving operation emits when
// the loaded catalog is a stale fallback, shared so it never drifts between
// surfaces (R6).
func staleWarning() Warning {
	return Warning{Kind: WarnStaleCatalog, Msg: "agentdex catalog is stale: re-resolution failed, using the last resolved version"}
}

func notInstalledWarning(id string) Warning {
	return Warning{Kind: WarnNotInstalled, Msg: fmt.Sprintf("agent %q is catalogued but not installed", id)}
}

func providersRequiredWarning(id string) Warning {
	return Warning{Kind: WarnProvidersRequired, Msg: fmt.Sprintf("%q is provider-agnostic", id)}
}

func modelsUnreachableGetWarning() Warning {
	return Warning{Kind: WarnModelsUnreachable, Msg: "models.dev is unreachable and not cached: model enrichment and provider-env omitted"}
}

func someProvidersAbsentWarning(absent []string) Warning {
	return Warning{Kind: WarnSomeProvidersAbsent, Msg: fmt.Sprintf("some providers are absent from models.dev: %s", strings.Join(absent, ", "))}
}

// listDegradeWarning selects the list-level wording for a models.dev shortfall:
// the unreachable count-omission or the schema-drift omission naming the fault (R6).
func listDegradeWarning(mode degradeMode, cause error) Warning {
	if mode == degradeSchema {
		return Warning{Kind: WarnModelsSchemaDrift, Msg: fmt.Sprintf("model counts omitted: %v", cause)}
	}
	return Warning{Kind: WarnModelsUnreachable, Msg: "model counts unavailable: models.dev is unreachable and not cached"}
}
