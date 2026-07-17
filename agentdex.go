// Package agentdex detects AI coding agents installed on the local machine and
// reports, for each known agent, where it lives, its version, its config and
// skills paths, its providers, provider-env presence, and optionally an enriched
// models.dev model list. It owns the outside of an agent — identity, location,
// paths, version, capability — and never reads an agent's internal config.
//
// The detection engine is data-driven: one generic engine walks the agent
// catalog and applies the same steps to every entry, so adding an agent is a
// catalog edit, not a code change.
package agentdex

import (
	"context"
	"time"

	"github.com/start-cli/agentdex/internal/catalog"
	"github.com/start-cli/agentdex/modelsdev"
)

// Option configures a detection run or a catalog load.
type Option func(*config)

// ModelsOption tunes the models.dev integration enabled by WithModels.
type ModelsOption func(*modelsConfig)

// config is the resolved option set the engine and catalog loader read from.
// User config and flags map entirely through options, so the engine keeps no
// direct dependency on internal/catalog beyond loader construction.
type config struct {
	preloaded       *Catalog
	catalogModule   string
	catalogTTL      time.Duration
	catalogTTLSet   bool
	cacheDir        string
	skipVersion     bool
	includeMissing  bool
	searchDirs      []string
	binPaths        map[string]string
	disabled        map[string]struct{}
	models          *modelsConfig
	callerProviders []string
}

// modelsConfig holds the attached models.dev client and whether per-model
// enrichment was requested. A nil *modelsConfig means no client was attached.
type modelsConfig struct {
	client *modelsdev.Client
	enrich bool
}

func newConfig(opts ...Option) *config {
	cfg := &config{
		binPaths: map[string]string{},
		disabled: map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// WithModels attaches a models.dev client, enabling provider-env reporting.
// Passing EnrichModels() additionally fills Agent.Models. The client is
// mandatory: a nil client attaches nothing, so enrichment can never be requested
// without a client to serve it.
func WithModels(c *modelsdev.Client, opts ...ModelsOption) Option {
	return func(cfg *config) {
		if c == nil {
			return
		}
		mc := &modelsConfig{client: c}
		for _, opt := range opts {
			opt(mc)
		}
		cfg.models = mc
	}
}

// EnrichModels, passed to WithModels, additionally fills Agent.Models with the
// agent's per-provider model list. Without it, WithModels attaches the client for
// provider-env reporting only.
func EnrichModels() ModelsOption {
	return func(mc *modelsConfig) { mc.enrich = true }
}

// WithProviders supplies models.dev provider ids for provider-agnostic agents.
// Home-provider agents ignore the list and use their catalog providers. An empty
// call is a no-op; duplicate ids are dropped. Combined with WithModels on
// DetectOne, missing providers for an agnostic agent yield ErrProvidersRequired;
// multi-agent Detect soft-skips enrichment for that agent instead.
func WithProviders(ids ...string) Option {
	return func(cfg *config) {
		if len(ids) == 0 {
			return
		}
		cfg.callerProviders = dedupeIDs(ids)
	}
}

// dedupeIDs drops duplicate ids preserving first-seen order, so a repeated
// caller-supplied provider cannot double model candidates downstream.
func dedupeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// WithSkipVersion runs detection fully exec-free: no binary is executed and
// Version is left empty.
func WithSkipVersion() Option {
	return func(cfg *config) { cfg.skipVersion = true }
}

// IncludeMissing makes Detect also return catalogued agents whose binary was
// not found, populated from the catalog with Found false and an empty Version,
// instead of omitting them. DetectOne is unaffected: it always answers for any
// catalogued id.
func IncludeMissing() Option {
	return func(cfg *config) { cfg.includeMissing = true }
}

// WithSearchDirs adds binary search locations consulted after PATH.
func WithSearchDirs(dirs ...string) Option {
	return func(cfg *config) { cfg.searchDirs = append(cfg.searchDirs, dirs...) }
}

// WithBinPaths overrides specific agents' binary paths by id. An override is a
// filesystem path, made absolute (relative values resolve against the working
// directory); it is not PATH-resolved. It wins over PATH and search dirs and is
// the binary used for the version exec.
func WithBinPaths(m map[string]string) Option {
	return func(cfg *config) {
		for id, path := range m {
			cfg.binPaths[id] = path
		}
	}
}

// WithDisabled skips the given catalog ids in Detect. It does not affect
// DetectOne, which answers for any catalogued id.
func WithDisabled(ids ...string) Option {
	return func(cfg *config) {
		for _, id := range ids {
			cfg.disabled[id] = struct{}{}
		}
	}
}

// WithCatalog uses a preloaded catalog instead of loading one from the registry.
// It bypasses the loader entirely, so WithCatalogModule, WithCatalogTTL, and
// WithCacheDir have no effect when it is supplied.
func WithCatalog(c *Catalog) Option {
	return func(cfg *config) { cfg.preloaded = c }
}

// WithCatalogModule overrides the catalog module path (config catalog.module).
// Ignored when WithCatalog supplies a preloaded catalog.
func WithCatalogModule(path string) Option {
	return func(cfg *config) { cfg.catalogModule = path }
}

// WithCatalogTTL sets the catalog version-resolution cache TTL. The caller
// resolves the effective duration and passes a concrete value.
func WithCatalogTTL(d time.Duration) Option {
	return func(cfg *config) {
		cfg.catalogTTL = d
		cfg.catalogTTLSet = true
	}
}

// WithCacheDir overrides the catalog version-resolution cache directory.
func WithCacheDir(dir string) Option {
	return func(cfg *config) { cfg.cacheDir = dir }
}

// Detect runs every catalog entry through the detection engine and returns the
// agents found, sorted by id. Not-installed agents are omitted unless
// IncludeMissing is supplied.
func Detect(ctx context.Context, opts ...Option) ([]Agent, error) {
	cfg := newConfig(opts...)
	cat, err := cfg.resolveCatalog(ctx)
	if err != nil {
		return nil, err
	}
	return detectAll(ctx, cat, cfg)
}

// DetectOne detects a single agent by catalog id. For any id in the catalog it
// returns a fully populated *Agent — config and skills resolved for both scopes,
// Found and the per-scope existence flags reflecting reality — whether or not the
// binary is installed; the bool mirrors Agent.Found. An id absent from the
// catalog returns ErrAgentUnknown. Unlike Detect, a known-but-not-installed agent
// is a normal result here, not an omission. WithDisabled does not apply: a
// targeted query answers for any catalogued id, disabled or not.
func DetectOne(ctx context.Context, id string, opts ...Option) (*Agent, bool, error) {
	cfg := newConfig(opts...)
	cat, err := cfg.resolveCatalog(ctx)
	if err != nil {
		return nil, false, err
	}
	ka, ok := cat.Agents[id]
	if !ok {
		return nil, false, ErrAgentUnknown
	}
	a, err := detectAgent(ctx, id, ka, cfg, newEnv(), detectMode{single: true})
	if err != nil {
		return nil, false, err
	}
	return a, a.Found, nil
}

// LoadCatalog fetches and loads the agent catalog (registry plus cache). The
// bool is the loader's stale flag: true when re-resolution failed after the TTL
// expired and the last resolved version was reused, in which case the catalog is
// still usable. A caller can warn on a stale catalog, then pass it back into
// Detect/DetectOne via WithCatalog. Detect and DetectOne do not surface
// staleness; this explicit load step is where a caller observes it.
func LoadCatalog(ctx context.Context, opts ...Option) (cat *Catalog, stale bool, err error) {
	cfg := newConfig(opts...)
	if cfg.preloaded != nil {
		return cfg.preloaded, false, nil
	}
	loader, err := cfg.newLoader()
	if err != nil {
		return nil, false, err
	}
	return loadCatalog(ctx, loader)
}

// resolveCatalog returns the preloaded catalog when one was supplied, otherwise
// loads one from the registry. Detect/DetectOne discard the loader's stale flag;
// staleness is surfaced only through the explicit LoadCatalog step.
func (cfg *config) resolveCatalog(ctx context.Context) (*Catalog, error) {
	if cfg.preloaded != nil {
		return cfg.preloaded, nil
	}
	loader, err := cfg.newLoader()
	if err != nil {
		return nil, err
	}
	cat, _, err := loadCatalog(ctx, loader)
	return cat, err
}

// newLoader constructs the production catalog loader, threading the catalog
// source options into the loader's own options. The registry is built only here,
// when no preloaded catalog is supplied.
func (cfg *config) newLoader() (*catalog.Loader, error) {
	reg, err := catalog.NewRegistry()
	if err != nil {
		return nil, err
	}
	var opts []catalog.Option
	if cfg.catalogModule != "" {
		opts = append(opts, catalog.WithModulePath(cfg.catalogModule))
	}
	if cfg.catalogTTLSet {
		opts = append(opts, catalog.WithTTL(cfg.catalogTTL))
	}
	if cfg.cacheDir != "" {
		opts = append(opts, catalog.WithCacheDir(cfg.cacheDir))
	}
	return catalog.New(reg, opts...), nil
}
