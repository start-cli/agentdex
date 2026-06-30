package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// DefaultURL is the published models.dev catalog fetched unless overridden.
const DefaultURL = "https://models.dev/catalog.json"

// DefaultTTL is how long a cached catalog.json is served before a refetch is
// attempted. On a failed refetch the stale copy is still served.
const DefaultTTL = 24 * time.Hour

// DefaultHTTPTimeout bounds a single catalog fetch end to end. It is a backstop
// independent of the caller's context: the shared single-flight fetch detaches
// from any one caller's cancellation, so without an overall timeout a stalled
// endpoint that never sends response headers would wedge the Client and leak the
// fetch goroutine permanently. WithHTTPClient overrides it.
const DefaultHTTPTimeout = 30 * time.Second

// maxResponseBytes caps the catalog.json read so a misbehaving or hostile
// endpoint cannot exhaust memory. The real catalog is a few megabytes.
const maxResponseBytes = 64 << 20

// Client fetches, caches, merges, and serves the models.dev catalog. The fetch,
// decode, and merge happen once per Client and the merged catalog is memoised in
// memory; the file cache and TTL govern that single fetch, not each call. A
// long-lived Client therefore never re-merges — a refresh is picked up by a
// freshly constructed Client. Methods are safe for concurrent use.
type Client struct {
	httpClient   *http.Client
	url          string
	cache        cache
	ttl          time.Duration
	forceRefresh bool
	now          func() time.Time

	mu       sync.Mutex
	catalog  *Catalog   // memoised once a usable result is obtained
	inflight *fetchCall // the single in-flight fetch, nil when none
}

// fetchCall is one shared fetch attempt. Concurrent callers that arrive while a
// fetch is in flight wait on done and read its result rather than racing their
// own requests.
type fetchCall struct {
	done    chan struct{}
	catalog *Catalog
	err     error
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithURL overrides the catalog URL, the way to pin a mirror or a frozen
// snapshot of the real shape.
func WithURL(url string) ClientOption {
	return func(c *Client) { c.url = url }
}

// WithCacheDir overrides the stale-cache directory.
func WithCacheDir(dir string) ClientOption {
	return func(c *Client) { c.cache = cache{dir: dir} }
}

// WithTTL overrides the cache TTL.
func WithTTL(ttl time.Duration) ClientOption {
	return func(c *Client) { c.ttl = ttl }
}

// WithForceRefresh makes the client fetch fresh bytes from the network on its next
// load, ignoring the cache TTL, and report a fetch or decode failure rather than
// falling back to a stale cache. It is the honest mode for an explicit refresh:
// the caller learns whether fresh data was actually fetched. A successful fetch
// still updates the on-disk cache.
func WithForceRefresh() ClientOption {
	return func(c *Client) { c.forceRefresh = true }
}

// WithHTTPClient overrides the package-owned HTTP client, replacing the default
// timeout backstop. A consumer that supplies a client without a timeout, against
// an endpoint that can stall, takes on responsibility for bounding the fetch
// through the request context.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// New constructs a Client with built-in defaults: the published catalog URL, the
// cache directory under $XDG_CACHE_HOME, a 24h TTL, and a package-owned HTTP
// client. Options override any of these.
func New(opts ...ClientOption) *Client {
	c := &Client{
		// A dedicated client rather than http.DefaultClient: as a reusable leaf,
		// this package neither reads nor mutates http.DefaultClient (it still shares
		// http.DefaultTransport's connection pool, which is benign). DefaultHTTPTimeout
		// is a backstop on top of request-context cancellation, so a leader with no
		// deadline cannot wedge the shared fetch forever.
		httpClient: &http.Client{Timeout: DefaultHTTPTimeout},
		url:        DefaultURL,
		cache:      cache{dir: defaultCacheDir()},
		ttl:        DefaultTTL,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Catalog returns the merged catalog. The first call fetches, caches, and merges;
// later calls return the memoised copy. The returned pointer is shared and must
// be treated as read-only.
func (c *Client) Catalog(ctx context.Context) (*Catalog, error) {
	return c.load(ctx)
}

// Provider returns one provider by id, whether it was found, and any error. The
// found bool reports existence only; it is independent of the error. A provider
// that exists but carries a malformed model is returned with found true and a
// non-nil ErrModelsSchema, so a caller that branches on found alone cannot
// silently swallow schema drift as an absent provider. The per-model
// required-field check is applied to that provider only. The returned Provider
// shares the memoised catalog's maps and slices, so it must be treated as
// read-only.
func (c *Client) Provider(ctx context.Context, id string) (Provider, bool, error) {
	cat, err := c.load(ctx)
	if err != nil {
		return Provider{}, false, err
	}
	p, ok := cat.Providers[id]
	if !ok {
		return Provider{}, false, nil
	}
	if err := validateProvider(p); err != nil {
		return p, true, err
	}
	return p, true, nil
}

// Models returns the merged models of the named providers, sorted by id. The
// per-model required-field check is applied only to those providers; a malformed
// model in any of them raises ErrModelsSchema. Unknown provider ids are skipped.
// The returned models alias the memoised catalog's slices, so they must be
// treated as read-only.
func (c *Client) Models(ctx context.Context, providerIDs ...string) ([]Model, error) {
	cat, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	var models []Model
	seen := make(map[string]struct{}, len(providerIDs))
	for _, id := range providerIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		p, ok := cat.Providers[id]
		if !ok {
			continue
		}
		if err := validateProvider(p); err != nil {
			return nil, err
		}
		for _, m := range p.Models {
			models = append(models, m)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

// load returns the memoised catalog, single-flighting the first fetch. Only a
// usable result — a fresh fetch or a stale copy served on failure — is memoised;
// a bare first-fetch failure with no cache is returned to the caller and not
// memoised, so a transient blip does not permanently poison a long-lived Client.
//
// The shared fetch runs detached from any single caller's cancellation: each
// caller waits by selecting on its own context, so a caller that cancels gives up
// waiting without aborting the work the other waiters depend on. The fetch keeps
// the leader's deadline as a bound, so a black-hole endpoint cannot leak it.
func (c *Client) load(ctx context.Context) (*Catalog, error) {
	c.mu.Lock()
	if c.catalog != nil {
		cat := c.catalog
		c.mu.Unlock()
		return cat, nil
	}
	call := c.inflight
	if call == nil {
		call = &fetchCall{done: make(chan struct{})}
		c.inflight = call
		c.mu.Unlock()
		go c.runFetch(ctx, call)
	} else {
		c.mu.Unlock()
	}

	select {
	case <-call.done:
		return c.afterInflight(call)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// runFetch performs the one shared fetch for a single-flight group and records
// the result on call. It detaches from the leader's cancellation so an unrelated
// caller cancelling does not abort the fetch, while preserving the leader's
// deadline so the shared work still has a time bound.
func (c *Client) runFetch(leaderCtx context.Context, call *fetchCall) {
	ctx := context.WithoutCancel(leaderCtx)
	if deadline, ok := leaderCtx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	cat, err := c.fetchMerge(ctx)

	c.mu.Lock()
	call.catalog, call.err = cat, err
	if err == nil {
		c.catalog = cat
	}
	c.inflight = nil
	close(call.done)
	c.mu.Unlock()
}

// afterInflight returns the result a waiter should see once the shared fetch it
// waited on has completed: the memoised catalog if the leader succeeded, or the
// shared error so the waiter can retry rather than inherit a cached failure.
func (c *Client) afterInflight(call *fetchCall) (*Catalog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catalog != nil {
		return c.catalog, nil
	}
	return call.catalog, call.err
}

// fetchMerge produces a usable catalog: a within-TTL cache hit, a fresh fetch, or
// a stale copy served when the fetch fails. It returns an error only on a fetch
// failure with no cache to fall back on. Under forceRefresh it skips both the
// within-TTL cache hit and the stale fallback, so the caller learns honestly
// whether fresh bytes were fetched. The returned catalog is merged in place.
func (c *Client) fetchMerge(ctx context.Context) (*Catalog, error) {
	cachedData, modTime, cached := c.cache.read()
	if !c.forceRefresh && cached && c.now().Sub(modTime) < c.ttl {
		if cat, err := decodeValidate(cachedData); err == nil {
			merge(cat)
			return cat, nil
		}
		// A corrupt within-TTL cache is unusable as either fresh or stale: fall
		// through to the network and do not serve it below.
		cached = false
	}

	data, fetchErr := c.get(ctx)
	if fetchErr == nil {
		cat, decErr := decodeValidate(data)
		if decErr == nil {
			_ = c.cache.write(data) // best-effort: a usable result is returned regardless
			merge(cat)
			return cat, nil
		}
		fetchErr = decErr
	}

	if !c.forceRefresh && cached {
		if cat, err := decodeValidate(cachedData); err == nil {
			merge(cat)
			return cat, nil
		}
	}
	return nil, fetchErr
}

// get performs the HTTPS GET and returns the response body bytes.
func (c *Client) get(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build models.dev request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models.dev catalog: unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read models.dev catalog: %w", err)
	}
	return data, nil
}

// decodeValidate decodes catalog bytes and applies the top-level gross-drift
// check. A decode failure or an empty top-level map yields an error; both are
// treated as fetch failures by the caller, so a cached copy is served when one
// exists.
func decodeValidate(data []byte) (*Catalog, error) {
	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("decode models.dev catalog: %w", err)
	}
	if err := validateTopLevel(&cat); err != nil {
		return nil, err
	}
	return &cat, nil
}
