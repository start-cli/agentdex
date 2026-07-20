# 02 Library API Redesign

## Goal

Replace the agentdex library's public API with a single coherent surface built
around the three data nouns the project actually serves — agents, providers, and
models — with detection expressed as a property of an agent rather than a
top-level verb. The result must read as if this surface were the original design:
no compatibility layer, no deprecated symbols, no trace of the API it replaces.

## Scope

In scope:

- A complete rewrite of the public API of the root Go package
  `github.com/start-cli/agentdex`.
- Relocation of domain policy that currently lives in `internal/cli` (model
  scope resolution, provider coverage rollup, composite model lookup,
  models-across-providers listing, enrichment degrade classification,
  not-found versus not-installed) into the library.
- Folding the `internal/config` option-mapping bridge into the new entry point.
- Rewriting `internal/cli` as a thin shell over the new surface.
- Updating repository documents so they describe only the new API.

Out of scope:

- The `modelsdev` package public surface. It stays a leaf that the new API
  composes over; its exported types, functions, and behaviour do not change.
- The CUE catalog module under `catalog/` and its schema.
- The detection engine's observable behaviour: version probing, path resolution,
  XDG handling, catalog version-resolution caching, and models.dev merge/caching
  all keep their current semantics. This project re-plumbs them behind a new
  public face; it does not change what they compute.
- The CLI's observable behaviour: command tree, flags, JSON envelope shape, exit
  codes, warning wording, ordering defaults, `--fields`, and empty-state
  messages remain as they are today.
- The `version` and `completion` commands. They carry no library policy and are
  unchanged, save for whatever the mechanical switch to the new entry point
  requires of the shared command wiring.

## Current State

The root package `agentdex` today exposes a detection-first API: `Detect`,
`DetectOne`, `LoadCatalog`, a `ResolveModel` method on `Catalog`,
`ValidateCallerProviders`, a family of `With*` functional options, the result
types `Agent`, `Catalog`, `KnownAgent`, `ResolvedPaths`, `PathPair`,
`VersionProbe`, `Option`, `ModelsOption`, and the sentinel errors
`ErrAgentUnknown`, `ErrCatalogUnavailable`, `ErrModelAmbiguous`,
`ErrModelNotFound`, `ErrProvidersRequired`, `ErrUnknownProvider`.

The detection mechanics live in the root package files `agentdex.go`, `engine.go`,
`probe.go`, `resolve.go`, `agent.go`, `version.go`, `catalog.go`. The catalog
loader, cache, registry access, and CUE decode live under `internal/catalog`.

`internal/config` owns `config.cue` loading and maps configuration plus global
flags into library options and a `modelsdev.Client`. Its `options.go` provides
`CatalogOptions`, `LibraryOptions`, `ModelsClient`, and `ForceRefreshModelsClient`.

`internal/cli` is a cobra tree with a noun/verb shape: `agents`, `providers`,
`models` each carry `list` and `get`, plus `refresh`, `version`, and `completion`.
Although the design intends the CLI to reimplement no library behaviour, several
pieces of domain policy currently live only in the CLI:

- `resolveModelsScope` (models.go): resolves the provider set a model listing
  spans from `--agent` and `--provider`, enforcing the agnostic-requires-provider
  and home-provider-rejects-provider rules and validating provider ids.
- `modelsGet` (models.go): splits the composite `provider-id/model-id` on the
  first slash, looks up the provider then the model, and computes the canonical
  agnostic id. The code comment notes this is composed in the CLI "rather than a
  library API".
- `modelsList` (models.go): iterates the scoped providers and their models,
  computes canonical ids, and applies the browse filter.
- `rollup` and `getCoverage` (agents.go): probe each of a detected agent's
  providers against models.dev and reduce to a per-agent verdict
  (all-present, some-present, none-present, no-providers, unreachable, schema).
- The list enrichment degrade policy (agents.go): model counts served from the
  warm cache, degraded to a warned zero when models.dev is unreachable and
  uncached, and re-detected without enrichment on schema drift.
- The not-installed handling (agents.go): a catalogued but undetected agent is
  reported at exit 0 with a warning, distinct from a catalog miss at exit 3.

The CLI's presentation machinery is `internal/cli`'s `record`/`fieldSet`
(envelope.go), `tabulate`/`render` (views.go, render.go), ordering (order.go),
the JSON `envelope` and `--fields` selection (envelope.go), the exit-code
taxonomy (exit.go), and `internal/tui` colour and table layout.

The exit-code taxonomy is: `0` ok, `1` failure, `2` usage, `3` not-found,
`4` permission, `5` conflict, `75` transient, `78` config.

The three field sets and their default orders are fixed and must be
reconstructable from the new library data:

- Agent fields: `id, name, version, bin, found, config_dir, config_local_dir,
  skills_dir, providers, homepage, provider_env, models`. Default columns:
  `id, name, version, providers, models, bin`. Default order: `id`.
- Model fields: `id, provider, name, family, context, input, output, total,
  reasoning, tool_call, attachment, released, canonical_id`. Default columns:
  `id, name, context, input, output`. Default order: `released`, descending.
- Provider fields: `id, name, env, present, models, doc, npm, api`. Default
  columns: `id, name, env, models`. Default order: `id`.

There is one existing project document, `01-theme-safe-terminal-path-colour.md`.
The repository has no standing library-API design document; `README.md` and
`AGENTS.md` are the API-describing documents, and both currently describe the
detection-first framing.

The behaviour that this project must preserve is encoded in the existing test
suites, which are the parity oracle for the rewrite:

- Root package (exercises the current public API, so these are rewritten against
  the new surface): `agentdex_test.go`, `catalog_test.go`, `enrich_test.go`,
  `resolve_test.go`, `version_test.go`.
- CLI end-to-end and behavioural tests (drive `NewRootCommand` with captured
  output; these encode the observable contract): `harness_test.go`,
  `agents_list_test.go`, `agents_get_test.go`, `agnostic_test.go`,
  `models_test.go`, `providers_test.go`, `refresh_test.go`, `order_test.go`,
  `order_cli_test.go`, `views_test.go`, `cli_test.go`, `root_test.go`.
- Unchanged-subsystem tests: `internal/catalog/*_test.go`,
  `internal/config/config_test.go`, `internal/match/match_test.go`,
  `internal/tui/table_test.go`, `modelsdev/client_test.go`,
  `modelsdev/merge_test.go`.

## Requirements

### R1 Remove the current public API in full

Delete every exported symbol listed in Current State from the public surface:
`Detect`, `DetectOne`, `LoadCatalog`, `Catalog.ResolveModel`,
`ValidateCallerProviders`, all current `With*` options, `Option`, `ModelsOption`,
and the current result types where they are not carried forward below. No
deprecation shim, alias, or forwarding wrapper may remain. After this project a
reader of the root package must see only the surface defined here.

The detection, probing, path-resolution, enrichment, and catalog-loading
mechanics are retained as unexported implementation behind the new services.
Relocate them to unexported package scope or to `internal/` packages so the root
package's exported surface is exactly the list in R2 through R13.

### R2 Entry point and facade

The single entry point is `Open`, returning an `*Index` that exposes the three
noun services as fields and carries the cache-level operations.

```go
func Open(ctx context.Context, opts ...Option) (*Index, error)

type Index struct {
    Agents    AgentService
    Providers ProviderService
    Models    ModelService
}

func (x *Index) Refresh(ctx context.Context, t Target) (Refreshed, error)
func (x *Index) CatalogStale() bool
```

Each service provides exactly two operations, a browse `List` and an exact
`Get`:

```go
// AgentService
List(ctx context.Context, q AgentQuery) (Result[Agent], error)
Get(ctx context.Context, id string, q AgentGetQuery) (AgentDetail, error)

// ProviderService
List(ctx context.Context, q ProviderQuery) (Result[Provider], error)
Get(ctx context.Context, id string) (Provider, error)

// ModelService
List(ctx context.Context, q ModelQuery) (Result[Model], error)
Get(ctx context.Context, composite string) (Model, error)
```

Whether the services are concrete structs or interfaces is an implementation
choice; they must be reachable as the named fields with the signatures above.

### R3 Query, result, and detail types

```go
type Result[T any] struct {
    Items    []T
    Warnings []Warning
}

type AgentQuery struct {
    Filter    string   // substring over id and name, case-insensitive; "" matches all
    Installed bool     // true narrows to agents detected on this machine
    Providers []string // enrichment provider set for provider-agnostic agents
    Enrich    Enrich   // how much models.dev data to attach
}

type AgentGetQuery struct {
    Providers []string
    Enrich    Enrich
}

type ProviderQuery struct {
    Filter string
}

type ModelQuery struct {
    Scope  ModelScope
    Filter string
}

type ModelScope struct {
    Agent     string   // "" means not scoped by agent
    Providers []string // explicit provider ids; also the enrichment set for an agnostic Agent
}
```

Result and detail data types:

```go
type Agent struct {
    KnownAgent                    // static catalog facts, embedded
    Detection   Detection
    ProviderEnv map[string]bool   // API-key env var -> present; nil when models.dev was not consulted
    Enrichment  EnrichmentState
    ModelCount  int               // meaningful when Enrichment == EnrichApplied
    Models      []modelsdev.Model // populated when Enrich == EnrichFull; newest release first
}

type AgentDetail struct {
    Agent
    Coverage ProviderCoverage
}

type Detection struct {
    Found      bool
    BinaryPath string
    Version    string
    Config     ResolvedPaths
    Skills     ResolvedPaths
}

type Provider struct {
    modelsdev.Provider                 // embedded; carries Models
    Env                map[string]bool // API-key env var -> present in the environment
}

type Model struct {
    modelsdev.Model
    Provider    string
    CanonicalID string // agnostic-catalog key when the model has one, else ""
}
```

`KnownAgent` is the embedded static-facts type on `Agent`, and it is slimmed to
identity and capability only: `ID`, `Name`, `Bin`, `Description`, `Homepage`,
`Provider`, `Agnostic`. It does not carry the raw catalog `PathPair` values or the
version-probe recipe. `Detection` owns the resolved paths (as `ResolvedPaths`) and
the resolved version string, so `Agent` exposes exactly one `Config` and one
`Skills`, both resolved, with no duplicate unexpanded pair alongside them.

`ResolvedPaths` is carried forward with its current fields. The catalog `PathPair`
type and the `VersionProbe` recipe become unexported: raw path pairs and version
probing are internal to detection and are not part of the public surface.

### R4 Enrichment level and enrichment state

`Enrich` selects how much models.dev data an agent operation attaches:

```go
type Enrich int
const (
    EnrichNone  Enrich = iota // no models.dev round-trip; catalog and detection facts only
    EnrichCount               // ModelCount only
    EnrichFull                // full Models list
)
```

`EnrichmentState` records the outcome of enrichment on each returned `Agent`,
replacing the nil/empty/null encodings the CLI uses today:

```go
type EnrichmentState int
const (
    EnrichNotRequested EnrichmentState = iota // Enrich was EnrichNone
    EnrichApplied                             // a real count/list was attached
    EnrichNotApplicable                       // agnostic agent with no providers supplied
    EnrichDegraded                            // models.dev unreachable and uncached; count is not a true zero
)
```

A caller distinguishes an agnostic agent shown without a provider set
(`EnrichNotApplicable`) from a real empty result and from a models.dev outage
(`EnrichDegraded`) by this field alone.

### R5 Provider coverage on agent detail

`Agents.Get` reports per-provider models.dev coverage as data. It does not fail
on a coverage verdict; the caller maps verdicts to policy (the CLI maps them to
exit codes and warnings).

```go
type ProviderCoverage struct {
    Present []string
    Absent  []string
    Status  CoverageStatus
}

type CoverageStatus int
const (
    CoverageAllPresent CoverageStatus = iota
    CoverageSomePresent
    CoverageNonePresent
    CoverageNoProviders
    CoverageUnreachable
    CoverageSchemaDrift
)
```

### R6 Structured warnings

Warnings are structured and carry their own human-readable message. The library
sets the message; a caller emits it verbatim.

```go
type Warning struct {
    Kind WarningKind
    Msg  string
}

type WarningKind int
const (
    WarnStaleCatalog WarningKind = iota
    WarnModelsUnreachable
    WarnModelsSchemaDrift
    WarnSomeProvidersAbsent
    WarnNotInstalled
    WarnProvidersRequired // agnostic agent reported without a provider set (guidance, not an error)
)
```

When the loaded catalog is a stale fallback (`CatalogStale()` is true), every
`Result` and every `Detail` the library returns must include a
`WarnStaleCatalog` warning. Enrichment failures (models.dev unreachable and
uncached, or recognisable schema drift) never fail a `List`; they degrade the
result and attach the matching warning. The message wording carried in `Warning.Msg`
is the library's to own, and each message is preserved verbatim from the string
the CLI emits for that condition today, so the retained CLI end-to-end tests pass
their warning assertions unchanged.

### R7 Error set

The exported sentinels are:

```go
var (
    ErrCatalogUnavailable = errors.New(...) // cold offline, no previously resolved catalog version
    ErrAgentUnknown       = errors.New(...) // agent id not in the catalog
    ErrUnknownProvider    = errors.New(...) // provider id not known to models.dev
    ErrProvidersRequired  = errors.New(...) // agnostic agent needs a provider set to enrich
    ErrProvidersNotAllowed = errors.New(...) // home-provider agent given an explicit provider set
    ErrMalformedModelID   = errors.New(...) // model composite has no "/"
    ErrNotFound           = errors.New(...) // provider or model exact-get miss
)
```

`ErrModelNotFound` and `ErrModelAmbiguous` from the current surface are removed;
model lookup is now the exact composite `Get` (R9), which uses `ErrNotFound` and
`ErrMalformedModelID`. Recognisable models.dev schema drift surfaces as the
propagated `modelsdev.ErrModelsSchema` from `Provider`/`Model` operations and as
`WarnModelsSchemaDrift` on degraded agent lists.

### R8 Scope resolution and agnostic rules in the library

The library owns the agnostic/home-provider rules; they must not live in the CLI.
Applied uniformly across agent and model operations:

- Home-provider agent, no explicit providers: enrich against the agent's catalog
  providers.
- Home-provider agent, explicit providers supplied: `ErrProvidersNotAllowed`.
- Agnostic agent, providers supplied: validate each id against models.dev
  (`ErrUnknownProvider` on a miss); enrich against them.
- Agnostic agent, no providers, `Enrich == EnrichNone`: return outside facts only
  (no providers, provider_env, or models), with a `WarnProvidersRequired`
  warning. `Enrichment` is `EnrichNotApplicable`.
- Agnostic agent, no providers, `Enrich != EnrichNone`: `ErrProvidersRequired`.

`Models.List` with `ModelQuery.Scope.Agent` set resolves the agent to its
provider set by the same rules; an unknown agent id is `ErrAgentUnknown`. With no
agent and no providers, the listing spans every provider models.dev knows.
Caller-supplied provider ids are validated in every role they play.

### R9 Composite model get

`Models.Get(ctx, composite)` splits `composite` on the first slash only: the
prefix is the provider id, the whole remainder is the model key (a model key may
contain slashes; a provider id never does). A value with no slash is
`ErrMalformedModelID`. An unknown provider or an unknown model key is
`ErrNotFound`. The returned `Model` carries its `Provider` and a `CanonicalID`
that is the composite when that composite is a key in the models.dev
provider-agnostic map, else `""`.

### R10 Environment presence at the boundary

Provider env-var presence is read at the boundary through an injectable lookup so
record building is testable from inputs. `Open` accepts `WithEnvLookup(func(string) (string, bool))`,
defaulting to `os.LookupEnv`. Only presence is read, never the value. `Provider.Env`
and `Agent.ProviderEnv` are populated through this lookup.

### R11 Options for Open

`Open` accepts the following functional options, folding in every catalog,
models.dev, detection, and boundary setting the CLI configures today:

```
WithCatalogModule(path string)          // local catalog module override
WithCatalogTTL(d time.Duration)          // catalog version-resolution TTL
WithCacheDir(dir string)                 // cache directory for catalog and models.dev
WithModelsURL(url string)                // models.dev catalog source URL
WithModelsTTL(d time.Duration)           // models.dev cache TTL
WithSearchDirs(dirs ...string)           // extra binary search locations
WithBinPaths(m map[string]string)        // per-agent binary path overrides
WithDisabled(ids ...string)              // exclude agent ids from the catalog view
WithEnvLookup(fn func(string) (string, bool))
WithHTTPClient(hc *http.Client)          // HTTP client for models.dev
WithLogger(l *slog.Logger)               // structured logger; defaults to a discard handler (R19)
```

The per-`Detect` `WithSkipVersion` and `IncludeMissing` from the old surface are
gone: version probing is an internal concern, and inclusion of undetected agents
is governed by `AgentQuery.Installed`.

### R12 Lazy resolution

`Open` performs no network I/O. The agent catalog is resolved lazily on the first
operation that needs it (any `Agents` operation, a `Models.List` scoped by agent,
`Refresh` of the catalog target, or `CatalogStale`). models.dev is fetched lazily
on the first operation that needs it (any `Providers` or `Models` operation, or an
agent operation with `Enrich != EnrichNone`).

This preserves today's behaviour that a pure models.dev operation (a provider
listing) does not require the agent catalog, and that a cold-offline first run
fails only when an operation actually needs the unresolvable catalog, with
`ErrCatalogUnavailable`.

### R13 Refresh

```go
type Target int
const (
    TargetCatalog Target = iota
    TargetModels
    TargetAll
)

type Refreshed struct {
    Catalog bool // the agentdex catalog was re-resolved to a fresh version
    Models  bool // the models.dev catalog was refetched
}
```

`Refresh` forces re-resolution or refetch of the requested targets past their
caches. Catalog re-resolution that fails and falls back to the last resolved
version is not a successful refresh: it is reported as an error, and `Refreshed`
reflects only the targets that did refresh. A models.dev fetch failure is an
error; recognisable schema drift wraps `modelsdev.ErrModelsSchema`. On full
success `Refreshed` reports the targets that refreshed.

### R14 Ordering ownership

The library owns each noun's default order and returns list items already in it:
agents by id with detected agents leading the undetected tail; models newest
release first; providers by id. The library does not accept an arbitrary
sort-by-field request. Re-ordering a projected result by an arbitrary field, and
reversing it, stays in the CLI, because it operates on the CLI's projected
presentation record, not on the domain types.

### R15 CLI as a thin shell

Rewrite `internal/cli` so each command is: build a query, call one `Index` service
method, render the returned `Result` or detail, and map the library's facts to
output. No domain policy may remain in `internal/cli`: scope resolution, the
coverage rollup, the composite split, models-across-providers assembly, and
enrichment degrade classification all move to the library per R5 through R9.

Presentation stays in the CLI: the `record`/`fieldSet` projection, `--fields`
selection, table and detail rendering, `internal/tui`, the JSON envelope, the
empty-state and price-footer formatting, arbitrary-field ordering (R14), and the
exit-code taxonomy. The CLI maps library facts to exit codes as follows:

| Library fact | Exit code |
|---|---|
| `ErrCatalogUnavailable` | 75 transient |
| `ErrAgentUnknown`, `ErrNotFound` | 3 not-found |
| `ErrUnknownProvider`, `ErrProvidersRequired`, `ErrProvidersNotAllowed`, `ErrMalformedModelID` | 2 usage |
| `modelsdev.ErrModelsSchema` | 78 config |
| `AgentDetail.Coverage.Status` = `CoverageNonePresent` or `CoverageSchemaDrift` | 78 config, agent still reported |
| `Refresh` catalog re-resolution failed (stale) | 75 transient |
| `Refresh` models.dev fetch failed | 75 transient, or 78 on schema drift |
| config load `config.ErrConfig` / `fs.ErrPermission` / other | 78 / 4 / 1 |
| any `Warning` on an otherwise successful result | 0, warning emitted |

The library never chooses an exit code; it returns typed errors, coverage
verdicts, warning kinds, and `Detection.Found`, and the CLI maps them.

### R16 modelsdev unchanged

The `modelsdev` package remains a leaf that imports nothing from agentdex. Its
exported surface and behaviour do not change. The new `Index` composes over it;
`Provider` and `Model` embed `modelsdev.Provider` and `modelsdev.Model`.

### R17 Documentation

Update every repository document that describes the public library API —
including `README.md` and `AGENTS.md` — so they describe only the new surface.
Remove every description of the removed functions and types. The documents must
read as though this surface were the original; no document may reference a
migration from a prior API.

### R18 Test coverage

Test coverage is a deliverable of this project, not a side effect. The rewrite
must leave the codebase at least as well tested as it is today, with the relocated
behaviour tested where it now lives.

- Behaviour relocated from the CLI into the library (scope resolution, the
  provider coverage rollup, the composite model split, models-across-providers
  assembly, enrichment degrade classification, the agnostic/home rules, the
  not-found versus not-installed distinction) must gain direct library-level
  tests. Do not leave that behaviour verified only through the CLI.
- The root-package tests that exercise the removed API
  (`agentdex_test.go`, `catalog_test.go`, `enrich_test.go`, `resolve_test.go`,
  `version_test.go`) are rewritten to exercise the new surface. No behavioural
  assertion they carry is dropped without an equivalent assertion against the new
  API.
- Every new public operation is tested directly: `Open` and its options, the
  three services' `List` and `Get`, `Refresh`, `CatalogStale`, the R7 error set,
  the `EnrichmentState` and `ProviderCoverage` outcomes, the warning injection,
  and lazy resolution including the cold-offline `ErrCatalogUnavailable` path.
- The CLI end-to-end tests that drive `NewRootCommand` with captured output are
  retained as the observable-behaviour oracle. They verify that the rewrite did
  not change the JSON envelope, exit codes, warnings, ordering defaults,
  `--fields`, or empty-state output. A change forced on one of these assertions is
  a signal to investigate a regression, not to edit the assertion.
- Tests follow the repository practice: real CUE validation, real files via
  `t.TempDir()`, environment isolation via `t.Setenv` or the injected
  `WithEnvLookup`, table-driven cases, and real behaviour over mocks. The
  models.dev and catalog test doubles already in the suite are reused rather than
  replaced.

The CLI end-to-end suite is a strong oracle on exit codes and the noun/verb happy
paths, but it has blind spots where an observable behaviour is exercised without
being asserted, so a rewrite could regress it with no test failing. Close these
gaps as part of this project — add the missing assertions before relying on the
suite as the parity net, not after a regression ships:

- The `--json` failure envelope is asserted only on usage (2) and not-found (3).
  Assert the envelope shape (`status`, `error`, and `data` presence, and the
  `omitempty` behaviour of the `error`/`warnings` keys) on a config (78), a
  transient (75), and a permission (4) failure as well.
- Warnings are asserted only by substring today, so a reworded message passes.
  Assert each warning message by full-string equality — stale catalog, models.dev
  unreachable degrade, schema-drift omission, some-providers-absent,
  not-installed, and the agnostic-needs-provider guidance — so the verbatim
  wording R6 requires is actually enforced.
- `agents get` on a none-present or schema-drift data fault asserts only exit 78.
  Assert that the agent payload is still reported on that fault (R5, R15).
- The JSON null-versus-`[]` model-count distinction is pinned, but the text-cell
  distinction is not. Assert that an agnostic/not-applicable agent renders the
  `-` cell and a degraded agent renders `0`.
- Only the filter-matched-nothing empty-state line is asserted. Assert the
  genuine-empty line (no filter) for agents, models, and providers too.
- `refresh` against a reachable but malformed models.dev has no test. Assert it
  exits 78 (schema drift as config), matching the other model surfaces.
- `refresh all` and the default target assert only that two caches refreshed, not
  which. Assert the identity of the refreshed targets and the success wording.
- `agents get` some-present asserts the warning but not the surviving data. Assert
  that the present provider's models still populate.
- The `--provider`-on-home-provider rejection asserts the exit code but not the
  message. Assert the guidance text.
- Warnings-to-stderr in text mode is spot-checked on one command. Assert the
  stream discipline across commands: warnings to stderr in text mode and into the
  envelope under `--json`, data to stdout.

### R19 Observability

The policy relocated into the library (R5 through R9) carries the decision points
the CLI logs today through its `--debug` slog logger: the detection run, the
provider coverage verdict, model scope resolution, composite model resolution,
enrichment degrade, and refresh outcomes. Those log points must survive the move,
so the library is observable in its own right rather than going dark when its
logic leaves the CLI.

- `Open` accepts `WithLogger(*slog.Logger)` and defaults to a logger over a
  discard handler, so the library is silent unless a caller opts in and never
  writes to a stream it was not given.
- The library logs at decisions and boundaries, not per statement: where the
  catalog is resolved (fresh or stale), where models.dev is fetched, where
  enrichment degrades, where the coverage verdict is decided, where a scope or
  composite resolves, and at the start and end of a refresh. Use structured
  fields (agent id, provider ids, verdict, target) at debug level. Never log an
  environment variable's value; only its presence is ever read.
- The CLI passes the `slog.Logger` it builds from `--debug` into `Open` via
  `WithLogger`, so `--debug` continues to surface these decisions. These are
  stderr debug lines outside the JSON envelope, so they do not affect the
  observable-output contract or its tests.

## Constraints

- Go 1.25. Pure Go: no cgo, no C dependencies. The binary must build with
  `CGO_ENABLED=0`.
- Do not add dependencies. Compose over the standard library, the already-carried
  `cuelang.org/go` and `cobra`/`pflag`, and the existing `modelsdev` package.
- Preserve the boundary discipline: the library reports only the outside of an
  agent (identity, location, paths, version, capability) and never reads an
  agent's internal configuration. Nondeterministic inputs — clock, filesystem,
  network, environment — enter only through `Open` options and context.
- No compatibility layer. The removed API leaves no alias, shim, or forwarding
  wrapper.
- The CLI's observable behaviour is a fixed contract: command tree, flags, JSON
  envelope shape and keys, exit codes, warning wording, ordering defaults,
  `--fields` keys and defaults per Current State, filter empty-state messages,
  and price-footer rendering are unchanged by this project.
- Agent ids stay kebab-case; the catalog map key remains the single source of
  agent identity.
- Commit messages follow Scoped Commits.

## Implementation Plan

1. Introduce the new public types and sentinel errors in the root package with no
   behaviour: `Index`, the three service types, `Result[T]`, the query types,
   `Agent`/`AgentDetail`/`Detection`/`Provider`/`Model`, `Enrich`,
   `EnrichmentState`, `ProviderCoverage`/`CoverageStatus`, `Warning`/`WarningKind`,
   `Target`/`Refreshed`, and the R7 error set. Carry the slimmed `KnownAgent` and
   `ResolvedPaths` forward per R3; make the catalog `PathPair` and `VersionProbe`
   types unexported.

2. Implement `Open` and lazy wiring (R11, R12, R19): construct the `Index` over the
   existing catalog loader and a `modelsdev.Client`, resolving the catalog and
   fetching models.dev on first need. Accept `WithLogger` with a discard default
   and thread the logger to the decision points. Move the `internal/config` option
   mapping so that `config.Config` produces `[]agentdex.Option` for `Open`; the
   config package keeps ownership of `config.cue` loading.

3. Implement `AgentService` (R3, R4, R5, R8): detection plus enrichment, the
   enrichment-state encoding, the provider coverage rollup as data, the
   not-installed fact, and the agnostic/home rules. Lift the rollup and degrade
   policy out of `internal/cli/agents.go` into the library.

4. Implement `ProviderService` (R3, R10): list and exact get over models.dev with
   env presence read through the injected lookup.

5. Implement `ModelService` (R3, R8, R9): scope resolution, models-across-providers
   assembly with canonical ids, the browse filter, and the composite `Get`. Lift
   `resolveModelsScope`, `modelsList`, and `modelsGet` out of the CLI.

6. Implement `Index.Refresh` and `Index.CatalogStale`, and the automatic
   `WarnStaleCatalog` injection into every `Result` and detail (R6, R13).

7. Remove the old public API (R1) and relocate the detection, probe, resolve,
   enrich, and version mechanics to unexported scope or `internal/`. Confirm the
   root package's exported surface is exactly R2 through R13.

8. Rewrite `internal/cli` as thin shells over the services (R15): each command
   builds a query, calls one method, renders the result, and maps facts to the
   exit-code table. Pass the `--debug` `slog.Logger` into `Open` via `WithLogger`
   (R19). Keep the presentation machinery. Delete the CLI-resident domain policy
   now living in the library.

9. Move test coverage down with the behaviour (R18): give the relocated policy
   direct library-level tests, rewrite the root-package tests that exercise the
   removed API against the new surface, and add direct tests for every new public
   operation. Keep the CLI end-to-end tests that drive `NewRootCommand` with
   captured output as the parity oracle for observable behaviour.

10. Update the documentation (R17).

11. Run the finalisation sweep.

Steps 3, 4, and 5 depend on 1 and 2. Steps 7 and 8 depend on 3 through 6. Step 9
runs alongside 3 through 8, so each relocated piece of behaviour lands with its
tests.

## Implementation Guidance

- Treat the `modelsdev` leaf as fixed. Everything new composes over it; nothing
  new is added to it.
- The presentation boundary in R15 is what keeps this project finite. Do not pull
  the `record`/`fieldSet` projection, `internal/tui`, or arbitrary-field ordering
  into the library, and do not let the library learn presentation field names.
- The enrichment-state and coverage encodings (R4, R5) exist to replace the
  nil-versus-empty-versus-null signalling the CLI uses today. Model the domain
  fact explicitly and let the CLI translate it to its null/dash/count cells.
- This is an internal re-plumb with a new public face. The detection engine, the
  merge, and the caches already compute the right answers; the work is exposing
  them through the new surface and moving the policy that leaked into the CLI back
  into the library. The CLI end-to-end tests describe the observable contract —
  a change forced on one of them is a signal to check for a behaviour regression,
  not to edit the assertion.
- Validate the exit-code mapping table (R15) against the current
  `internal/cli/exit.go` before finishing: every code the CLI can emit today must
  be reconstructable from a library fact the new surface exposes.

## Acceptance Criteria

1. `go doc github.com/start-cli/agentdex` shows exactly the surface defined in R2
   through R13 and none of the removed symbols (`Detect`, `DetectOne`,
   `LoadCatalog`, `Catalog.ResolveModel`, `ValidateCallerProviders`, the old
   `With*` options, `Option`, `ModelsOption`, `ErrModelNotFound`,
   `ErrModelAmbiguous`).
2. `go doc github.com/start-cli/agentdex/modelsdev` is unchanged from before the
   project.
3. Each of the six list/get commands and `refresh` is implemented as one `Index`
   service (or `Index.Refresh`) call plus rendering; `internal/cli` contains no
   model scope resolution, coverage rollup, composite split, or enrichment
   degrade classification.
4. The agnostic and home-provider rules of R8 are enforced in the library and
   surfaced through `ErrProvidersRequired`, `ErrProvidersNotAllowed`, and
   `ErrUnknownProvider`.
5. A stale catalog surfaces as a `WarnStaleCatalog` warning on every `Result` and
   detail, and `Index.CatalogStale()` reports true.
6. An agnostic agent listed or fetched without a provider set carries
   `EnrichmentState == EnrichNotApplicable`; a models.dev outage on a count
   enrichment carries `EnrichmentState == EnrichDegraded`; neither fails the
   operation.
7. `Models.Get` resolves a composite by first-slash split with the R9 error
   behaviour, and fills `CanonicalID` from the models.dev agnostic map.
8. `Open` exposes `WithLogger`, defaults to a discard handler when it is not
   given, and the CLI passes its `--debug` logger through, so running a command
   under `--debug` emits the library's decision logs (catalog resolution,
   models.dev fetch, enrichment degrade, coverage verdict, refresh) to stderr.
9. The CLI's observable behaviour is unchanged: JSON envelope shape and keys, the
   exit codes in the R15 table, warning wording, ordering defaults, `--fields`
   keys and defaults per Current State, and filter empty-state messages match the
   pre-project behaviour, as demonstrated by the retained CLI end-to-end tests.
10. Every API-describing document, including `README.md` and `AGENTS.md`, describes
    only the new surface, with no reference to the removed API.
11. The behaviour relocated from the CLI into the library has direct library-level
    tests; the root-package tests that exercised the removed API are rewritten
    against the new surface with no behavioural assertion dropped; every new public
    operation, the R7 error set, the enrichment and coverage outcomes, warning
    injection, and the cold-offline `ErrCatalogUnavailable` path are tested
    directly; the ten CLI oracle gaps named in R18 are closed with new assertions;
    and the CLI end-to-end tests pass unchanged in their surviving assertions.
12. The finalisation sweep passes: `gofmt -l .` clean, `go build ./...` and
    `go vet ./...` pass, `golangci-lint run` clean, `go test ./...` passes, and
    from `catalog/` `cue vet ./...` passes with `cue mod tidy` clean.
