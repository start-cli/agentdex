# 02 models.dev Subpackage

## Goal

Deliver the `modelsdev` public subpackage: a reusable client that fetches models.dev's static `catalog.json`, merges its provider and provider-agnostic maps into a single enriched model view, validates against gross schema drift, and caches with stale-on-failure semantics. This is the authoritative model and provider layer the detection engine enriches from; agentdex never duplicates models.dev data.

## Scope

In scope:

- The `modelsdev` package Go types mirroring the upstream `{ models, providers }` schema.
- A `Client` that fetches `catalog.json` over HTTPS, caches it, and serves stale on failure.
- The merge that enriches each provider model with benchmarks and weights from the provider-agnostic map.
- Two-tier schema validation: top-level shape on every fetch, per-model required fields only for requested providers.
- Provider and model accessors used by the detection engine and CLI.

Out of scope:

- `ResolveModel` fuzzy matching. It lives in the root package (document 03) and calls into this client; the matching rule is not built here.
- Detection, the agent catalog, provider-env reporting against the process environment (that reads `Provider.Env` but lives in document 03's engine), and any CLI surface.
- Reading user `config.cue`. The client takes URL, cache dir, and TTL as options; document 04 maps config to them.

## Current State

After document 01 the repo is a Go module (`github.com/start-cli/agentdex`, Go 1.25) with the catalog CUE module and loader in place, a repo `AGENTS.md`, and `.golangci.yml`. This package has no internal dependency on document 01's work; it is an independent leaf and can be built in parallel with it. It depends only on the standard library plus the org's standard CLI-adjacent deps already in the design.

The full design is `docs/agentdex-design.md`; the relevant slice is reproduced below.

## References

- `docs/agentdex-design.md` — sections: models.dev integration, Why catalog.json (the merge), Go types, Client, Provider env reporting, Caching.
- models.dev published catalog: https://models.dev/catalog.json — the static JSON this client fetches. Shape is `{ models, providers }`.
- models.dev schema source: `packages/core/src/schema.ts` (zod) in the models.dev GitHub repo — the authoritative shape the Go types mirror. Do not mirror the repo-root `models.json`, which is a vendored OpenRouter dump and is not the published schema.
- A downloaded `catalog.json` sits at the agentdex repo root (`catalog.json`) and can seed the test fixture and confirm the Go types against real data. It has been confirmed as the published `catalog.json` shape (top-level `models` and `providers` maps), not the OpenRouter dump.

## Requirements

1. Go types mirroring the published schema

   - Define `Catalog`, `Provider`, `Model`, `Cost`, `Tier`, `Modalities`, `Limit`, `Benchmark`, and `Weight` to the contract below (the contract lists the field sets that matter; complete `Modalities`, `Limit`, `Benchmark`, and `Weight` from the upstream schema).
   - `Catalog.Models` is the provider-agnostic map keyed by path-style id. `Catalog.Providers` is keyed by provider id; each `Provider.Models` is keyed by the short model id within that provider.
   - `Model.ID` holds its source id: path-style in the agnostic map, short within a provider map. Do not normalise it.

2. Client

   - `New(opts ...ClientOption) *Client` with options for the catalog URL (default `https://models.dev/catalog.json`), cache directory (default `$XDG_CACHE_HOME/agentdex/`), and TTL (default 24h).
   - `Catalog(ctx)` returns the merged `*Catalog`. The first call fetches, caches, and merges; later calls return the in-memory copy. `Provider` and `Models` are accessors over the same memoised catalog.
   - `Provider(ctx, id)` returns one provider and an ok bool.
   - `Models(ctx, providerIDs...)` returns the merged model list for the named providers.
   - The fetch, decode, and merge happen once per `Client` and are memoised in memory; the file cache and TTL govern that single fetch, not each call. A long-lived `Client` therefore never re-merges; a refresh is picked up by a freshly constructed `Client` (the CLI's `refresh` rewrites the cache file before the next run).
   - Methods are safe for concurrent use. The detection engine (document 03) calls into the client once per agent across concurrent goroutines, so the first fetch is single-flighted: concurrent callers share one fetch and one cache-file write rather than racing N requests, and the memoised catalog is read-only after that.
   - Cache `catalog.json` at `$XDG_CACHE_HOME/agentdex/catalog-modelsdev.json` for the TTL; on a fetch failure serve the stale cached copy. This is a plain-JSON stale cache, distinct from document 01's CUE version-resolution cache.

3. The merge

   - The provider model carries cost, limit, status, modalities, and capability flags. Benchmarks and weights live only in the provider-agnostic map. Merge agnostic-first: iterate the agnostic map, split each real path-style id on its single slash into its provider and model parts, and copy that entry's benchmarks and weights onto the matching provider model.
   - Join on real keys only. Never construct `providerID + "/" + modelKey` as a stored or surfaced value, and never write anything onto `Model.ID`. Both sides keep their source ids. Driving from the agnostic map guarantees every key touched is a real models.dev id, so the merge cannot mint an id that does not exist upstream.
   - Benchmarks and weights land only where a provider model has a real agnostic entry. First-party models decompose to their short provider key and receive them; aggregator and proxy models, whose keys are already path-bearing, have no agnostic id decomposing to them and receive nothing — correctly, as they carry no benchmarks of their own. A low attachment rate against the full upstream catalog is expected, not a defect.

4. Two-tier validation

   - After decode, validate only the top-level shape: `models` and `providers` both non-empty. A violation fails the fetch with `ErrModelsSchema` ("models.dev schema unrecognised") rather than returning a hollow catalog, and follows stale-on-failure: a cached copy is served if present, and only a first fetch with no cache surfaces the error.
   - Apply the per-model required-field check (`id` non-empty, `limit` present) only to providers a caller actually requests, inside `Provider` and `Models` — never across the full upstream catalog. A malformed model in an unrequested provider must not break enrichment; only a requested provider carrying a malformed model raises `ErrModelsSchema`, from the accessor that requested it.

5. Errors

   - Define `ErrModelsSchema`. The model-resolution sentinels (`ErrModelAmbiguous`, `ErrModelNotFound`) belong to the root package with `ResolveModel` (document 03) and are not defined here.

## Constraints

- Pure Go, `CGO_ENABLED=0`, Go 1.25. Standard library for HTTP, JSON, and filesystem; `net/http/httptest` for tests.
- This package is public and reusable by external consumers. It must not import `internal/` packages, the agent catalog, or the root package. It is a leaf.
- models.dev is an unversioned community JSON with no contract. Go's decoder silently ignores unknown fields and zero-fills renamed ones, so validation is the only signal of drift. Do not relax the two-tier validation into a no-op.
- Follow the repo `AGENTS.md` for style, platforms, and markdown conventions.

## Type contract

Types are illustrative; the field sets are the contract. Complete `Modalities`, `Limit`, `Benchmark`, and `Weight` from the upstream zod schema.

```go
package modelsdev

// Catalog is the merged result of fetching models.dev catalog.json. Distinct
// from the agentdex agent Catalog; package qualification keeps them unambiguous.
type Catalog struct {
    Models    map[string]Model    // provider-agnostic, keyed by path-style model id
    Providers map[string]Provider // keyed by provider id
}

type Provider struct {
    ID     string
    Name   string
    Doc    string
    NPM    string
    API    string
    Env    []string         // API-key env var names, e.g. ["ANTHROPIC_API_KEY"]
    Models map[string]Model // keyed by short model id within the provider
}

type Model struct {
    ID               string // source id: path-style in the agnostic map, short within a provider map
    Name             string
    Family           string
    Attachment       bool
    Reasoning        bool
    ToolCall         bool
    StructuredOutput bool
    Temperature      bool
    Knowledge        string // YYYY-MM or YYYY-MM-DD
    ReleaseDate      string
    LastUpdated      string
    Modalities       Modalities // input/output of text|audio|image|video|pdf
    OpenWeights      bool
    Limit            Limit      // context, input, output token counts
    Cost             *Cost      // USD per 1,000,000 tokens; nil if unknown
    Status           string     // alpha|beta|deprecated
    Benchmarks       []Benchmark
    Weights          []Weight
}

type Cost struct {
    Input, Output, Reasoning, CacheRead, CacheWrite float64
    InputAudio, OutputAudio                         float64
    ContextOver200K *Cost  // pricing once context exceeds 200k tokens; nil when flat
    Tiers           []Tier // tiered pricing; nil when flat
}

type Tier struct {
    Input, Output, CacheRead, CacheWrite float64
    Tier TierDimension // upstream nests the dimension under a "tier" object
}

// TierDimension is the nested "tier" object on each tiered-pricing entry: the
// dimension and the threshold at which the tier takes effect.
type TierDimension struct {
    Type string // tier dimension, e.g. "context"
    Size int    // threshold at which this tier takes effect, e.g. 200000
}
```

## Client contract

```go
type Client struct{ /* http client, cache dir, ttl, url */ }

func New(opts ...ClientOption) *Client
func (c *Client) Catalog(ctx context.Context) (*Catalog, error)              // fetch+cache+merge
func (c *Client) Provider(ctx context.Context, id string) (Provider, bool, error)
func (c *Client) Models(ctx context.Context, providerIDs ...string) ([]Model, error)
```

## Implementation Plan

1. Define the Go types from the upstream zod schema, confirming each field against the repo-root `catalog.json`. Complete the types the contract abbreviates (`Modalities`, `Limit`, `Benchmark`, `Weight`).
2. Implement `New` and the option set (URL, cache dir, TTL with defaults).
3. Implement fetch: HTTPS GET, JSON decode, top-level validation, and the stale-on-failure JSON cache at `catalog-modelsdev.json`.
4. Implement the merge agnostic-first: iterate the agnostic map, split each real path-style id into its provider and model parts, and copy benchmarks and weights onto the matching provider model without constructing a composite id or rewriting `Model.ID`.
5. Implement `Provider` and `Models`, applying the per-model required-field check only to the requested providers.
6. Tests against an `httptest` server serving a fixture `catalog.json` (seed it from the repo-root `catalog.json`): merge correctness, the expected partial join rate for first-party versus aggregator providers, cache TTL, stale-on-failure, top-level `ErrModelsSchema`, and per-requested-provider `ErrModelsSchema`.

## Implementation Guidance

- The merge is a join over real keys, not a key rewrite, and is driven from the agnostic side. Iterate the agnostic map and decompose each real path-style id into (provider, model); do not assemble a composite from the provider side. Across the full catalog most provider model ids are already slash-bearing (aggregators re-expose other providers' models under path-style keys) while first-party providers keep them short, so a provider-side composite would not consistently align with the agnostic keys and, worse, would mint ids that do not exist upstream. Decomposing real agnostic ids avoids both. Keep both source ids intact.
- Validation exists to make drift loud, not to fully validate the schema. Keep it coarse: top-level shape always, per-model checks only for requested providers. The URL override remains the way to pin a frozen mirror, so do not harden validation into something that rejects a deliberately pinned older snapshot of the real shape.
- Push the clock, network, and filesystem to the boundary so the merge and validation are testable from decoded inputs alone. `go.uber.org/goleak` for goroutine-leak checks is optional.

## Acceptance Criteria

- `go build ./...`, `go vet ./...`, and `golangci-lint run` are clean.
- `New().Catalog(ctx)` against an `httptest` server returns a merged catalog where first-party provider models carry benchmarks and weights attached from the agnostic map by decomposing real path-style ids, and `Model.ID` is unchanged on both sides; no composite id is constructed.
- A first-party provider model receives benchmarks; an aggregator/proxy provider model with a path-bearing key has no agnostic id decomposing to it and is returned without agnostic benchmarks, with no error.
- A response with an empty `models` or `providers` map yields `ErrModelsSchema` on a first fetch with no cache, and serves the stale cached copy when one exists.
- A malformed model (`id` empty or `limit` absent) in an unrequested provider does not error; the same malformation in a provider passed to `Provider` or `Models` raises `ErrModelsSchema` from that accessor.
- Cache behaviour: a fresh fetch writes `catalog-modelsdev.json`; a within-TTL call reads it; a network failure after expiry serves the stale file.
- Memoisation and concurrency: repeated and concurrent `Catalog`/`Provider`/`Models` calls on one `Client` trigger a single upstream fetch and a single cache-file write (verified by the `httptest` server request count under concurrent callers), and run race-free under `-race`.
- A model with tiered pricing decodes its tier dimension and threshold: `Cost.Tiers[i].Tier.Type` and `Cost.Tiers[i].Tier.Size` are populated from the nested upstream `tier` object, not left zero.
- The package imports no `internal/` package, the agent catalog, or the root package.
