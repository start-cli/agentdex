package agentdex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/start-cli/agentdex/internal/catalog"
	"github.com/start-cli/agentdex/modelsdev"
)

// Open constructs an *Index over the configured catalog source and models.dev
// client. It performs no network I/O: the agent catalog and the models.dev
// catalog are resolved lazily, once, on the first operation that needs each
// (R12). The returned Index is safe for concurrent use.
func Open(_ context.Context, opts ...Option) (*Index, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	c := newCore(o)
	return &Index{
		Agents:    AgentService{core: c},
		Providers: ProviderService{core: c},
		Models:    ModelService{core: c},
		core:      c,
	}, nil
}

// core holds the boundary-captured inputs and the lazily resolved catalog and
// models.dev client behind the two guards the concurrent detection fan-out and
// Refresh require (R12, R13). The captured inputs are immutable for the Index's
// lifetime; only the guarded resolved values change, under their own mutex.
type core struct {
	envLookup  func(string) (string, bool)
	home       string
	workingDir string
	searchDirs []string
	binPaths   map[string]string
	logger     *slog.Logger

	catalogDir    string
	catalogModule string
	catalogTTL    time.Duration
	catalogTTLSet bool
	cacheDir      string

	modelsURL    string
	modelsTTL    time.Duration
	modelsTTLSet bool
	httpClient   *http.Client

	catMu    sync.Mutex
	cat      *catalog.Catalog
	catStale bool

	mdMu sync.Mutex
	md   *modelsdev.Client
}

// newCore captures every nondeterministic boundary input once, at Open, so the
// per-operation logic downstream is a pure function of these values (R10). The
// environment lookup, working directory, and home directory default to the
// process only when the caller supplies no override.
func newCore(o *options) *core {
	lookup := o.envLookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	wd := o.workingDir
	if !o.workingDirSet {
		if got, err := os.Getwd(); err == nil {
			wd = got
		}
	}
	logger := o.logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &core{
		envLookup:     lookup,
		home:          resolveHome(lookup),
		workingDir:    wd,
		searchDirs:    o.searchDirs,
		binPaths:      o.binPaths,
		logger:        logger,
		catalogDir:    o.catalogDir,
		catalogModule: o.catalogModule,
		catalogTTL:    o.catalogTTL,
		catalogTTLSet: o.catalogTTLSet,
		cacheDir:      o.cacheDir,
		modelsURL:     o.modelsURL,
		modelsTTL:     o.modelsTTL,
		modelsTTLSet:  o.modelsTTLSet,
		httpClient:    o.httpClient,
	}
}

// resolveHome resolves the home directory for tilde expansion from the injected
// lookup's HOME, keeping the os.UserHomeDir fallback for the case where HOME is
// unset. Platforms are Linux, macOS, and WSL (Linux-native), so HOME is
// authoritative; no platform-specific user-dir helper is used.
func resolveHome(lookup func(string) (string, bool)) string {
	if h, ok := lookup("HOME"); ok && h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// resolveCatalog returns the loaded catalog and its stale flag, loading it once
// under the guard and publishing the result to every later reader. A failed load
// is not memoised: the next operation retries, so a caller recovers from a
// transient outage without reopening the Index (R12).
func (c *core) resolveCatalog(ctx context.Context) (*catalog.Catalog, bool, error) {
	c.catMu.Lock()
	defer c.catMu.Unlock()
	if c.cat != nil {
		return c.cat, c.catStale, nil
	}
	cat, stale, err := c.loadCatalog(ctx)
	if err != nil {
		return nil, false, err
	}
	c.cat = cat
	c.catStale = stale
	return cat, stale, nil
}

// loadCatalog loads the catalog from whichever source is configured. A directory
// source (WithCatalogDir) evaluates a local CUE module with no registry contact,
// so it is never stale; otherwise the registry loader resolves a version and may
// report the last resolved version as stale. Loader faults are mapped to the
// public sentinels (R7).
func (c *core) loadCatalog(ctx context.Context) (*catalog.Catalog, bool, error) {
	if c.catalogDir != "" {
		cat, err := catalog.LoadDir(c.catalogDir)
		if err != nil {
			return nil, false, mapCatalogErr(err)
		}
		c.logger.LogAttrs(ctx, slog.LevelDebug, "catalog resolved",
			slog.String("source", "dir"), slog.String("dir", c.catalogDir))
		return cat, false, nil
	}

	reg, err := catalog.NewRegistry()
	if err != nil {
		return nil, false, err
	}
	var lopts []catalog.Option
	if c.catalogModule != "" {
		lopts = append(lopts, catalog.WithModulePath(c.catalogModule))
	}
	if c.catalogTTLSet {
		lopts = append(lopts, catalog.WithTTL(c.catalogTTL))
	}
	if c.cacheDir != "" {
		lopts = append(lopts, catalog.WithCacheDir(c.cacheDir))
	}
	res, err := catalog.New(reg, lopts...).Load(ctx)
	if err != nil {
		return nil, false, mapCatalogErr(err)
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "catalog resolved",
		slog.String("source", "registry"),
		slog.String("version", res.Version),
		slog.Bool("stale", res.Stale))
	return res.Catalog, res.Stale, nil
}

// mapCatalogErr translates the loader's internal sentinels into the public ones:
// ErrCatalogUnavailable for a cold-offline load with no fallback, ErrCatalogInvalid
// for a module that loaded but failed schema evaluation. Both keep the loader's
// wrapped message — the CUE diagnostic naming the offending entry and field — so a
// caller reads it verbatim (R7). Any other error passes through unwrapped.
func mapCatalogErr(err error) error {
	switch {
	case errors.Is(err, catalog.ErrUnavailable):
		return fmt.Errorf("%w: %w", ErrCatalogUnavailable, err)
	case errors.Is(err, catalog.ErrInvalidCatalog):
		return fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	default:
		return err
	}
}

// mapModelsErr translates a models.dev fetch fault on a Providers or Models
// operation into the public sentinels: recognisable schema drift propagates
// unchanged so errors.Is(err, modelsdev.ErrModelsSchema) resolves it and the CLI
// maps it to a config fault, while any other fetch failure — unreachable and
// uncached — becomes ErrModelsUnavailable, the models.dev analog of
// ErrCatalogUnavailable (R7). Agent operations degrade rather than calling this.
func mapModelsErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, modelsdev.ErrModelsSchema):
		return err
	default:
		return fmt.Errorf("%w: %w", ErrModelsUnavailable, err)
	}
}

// modelsClient returns the shared models.dev client, constructing it once under
// the guard so the concurrent detection fan-out reaches one client and pays one
// fetch: the client single-flights and memoises within its own lifetime, so two
// clients would produce two independent fetches (R12, R13). Construction does no
// network I/O; the fetch happens on the client's first query.
func (c *core) modelsClient() *modelsdev.Client {
	c.mdMu.Lock()
	defer c.mdMu.Unlock()
	if c.md == nil {
		c.md = c.newModelsClient()
	}
	return c.md
}

// newModelsClient builds a models.dev client from the captured source settings.
// extra carries per-call client options (the force-refresh mode Refresh installs,
// R13); the base settings are applied first so extra can override them.
func (c *core) newModelsClient(extra ...modelsdev.ClientOption) *modelsdev.Client {
	var opts []modelsdev.ClientOption
	if c.modelsTTLSet {
		opts = append(opts, modelsdev.WithTTL(c.modelsTTL))
	}
	if c.modelsURL != "" {
		opts = append(opts, modelsdev.WithURL(c.modelsURL))
	}
	if c.cacheDir != "" {
		opts = append(opts, modelsdev.WithCacheDir(c.cacheDir))
	}
	if c.httpClient != nil {
		opts = append(opts, modelsdev.WithHTTPClient(c.httpClient))
	}
	opts = append(opts, extra...)
	return modelsdev.New(opts...)
}
