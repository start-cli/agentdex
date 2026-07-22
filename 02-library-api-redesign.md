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
- Removing agent disabling end to end: the `disabled_agents` config key, its
  mapping, the `WithDisabled` option, and the skip branch in the detection walk.
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
  messages remain as they are today, with the three exceptions named in
  Constraints — agent detail on a not-installed agent, which stops short-circuiting
  enrichment and answers what an agent offers before it is installed (R4), the
  removal of the `disabled_agents` config key (R11), and the models table's
  provider column, decided from the returned rows rather than the requested
  scope (R15).
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
`schema.cue` there is the closed `#Config`: `cache_ttl`, `catalog.module`,
`catalog.ttl`, `models.url`, `models.ttl`, `search_dirs`, `bin_paths`,
`disabled_agents`, and `color`. There is no key naming a local catalog directory,
and `disabled_agents` reaches one line of the engine — the id skip in the
catalog walk — and is undocumented and covered only by a config-parsing test.

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
  (all-present, some-present, none-present, unreachable, schema, plus a
  no-providers branch nothing can reach).
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
`AGENTS.md` are the API-describing documents. Their project framing already
presents the data-first noun model (agents, providers, models), but neither yet
describes the new library surface this project introduces.

The behaviour that this project must preserve is encoded in the existing test
suites, which are the parity oracle for the rewrite:

- Root package: `agentdex_test.go`, `catalog_test.go`, and `enrich_test.go`
  exercise the current public API and are rewritten against the new surface.
  `resolve_test.go` covers the fuzzy matcher R1 deletes and goes with it.
  `version_test.go` tests pure helpers (version extraction, the capped buffer)
  and needs no rewrite. Of the 47 root tests, 32 pass a Go-built `Catalog`
  through `WithCatalog` and a further 9 build one to call `ResolveModel`.
- CLI end-to-end and behavioural tests (drive `NewRootCommand` with captured
  output; these encode the observable contract): `harness_test.go`,
  `agents_list_test.go`, `agents_get_test.go`, `agnostic_test.go`,
  `models_test.go`, `providers_test.go`, `refresh_test.go`, `order_test.go`,
  `order_cli_test.go`, `views_test.go`, `cli_test.go`, `root_test.go`.
- Unchanged-subsystem tests: `internal/catalog/*_test.go`,
  `internal/tui/table_test.go`, `modelsdev/client_test.go`,
  `modelsdev/merge_test.go`. `internal/config/config_test.go` is unchanged apart
  from the two schema edits this project makes: it decodes `disabled_agents`
  today, and gains `catalog.dir` (R11).

`internal/match` is not among them. Its none/one/many matcher exists for the
fuzzy model query, and `resolve.go` — the file R1 deletes — is its only
non-test caller.

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

What the removal leaves behind goes with it. `internal/match` and its tests are
deleted alongside `resolve.go`: the fuzzy none/one/many query it serves has no
successor, since model lookup becomes the exact composite `Get` (R9), and a
matcher kept with no caller reads as live code to the next person to open it.
Fuzzy model selection, if it is ever wanted, belongs to the surface that asks for
it and is designed then.

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
func (x *Index) CatalogStale(ctx context.Context) (bool, error)
```

`CatalogStale` resolves the agent catalog lazily like any catalog-touching
operation (R12), so it takes a `ctx` and returns `ErrCatalogUnavailable` on a
cold-offline first call rather than reporting a misleading `false`.

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
    Enrich    Enrich   // how much provider and models.dev data to attach
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
    Providers   []string          // resolved provider ids the operation used; empty below EnrichProviders, and when agnostic and unresolved
    ProviderEnv map[string]bool   // API-key env var -> present; nil when models.dev was not consulted
    Enrichment  EnrichmentState
    ModelCount  int               // meaningful when Enrichment == EnrichApplied
    Models      []modelsdev.Model // populated when Enrich == EnrichFull; newest release first
}

type AgentDetail struct {
    Agent
    Coverage ProviderCoverage
    Warnings []Warning // stale catalog, not-installed, coverage degrade, and agnostic-guidance warnings for this fetch
}

type Detection struct {
    Found      bool
    BinaryPath string
    Version    string
    Config     ResolvedPaths
    Skills     ResolvedPaths
}

type Provider struct {
    modelsdev.Provider                    // embedded; carries the env-var names and Models
    EnvPresent         map[string]bool    // API-key env var -> present in the environment
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

`Agent.Providers` is the resolved provider set the operation actually used:
`KnownAgent.Provider` for a home-provider agent, the caller-supplied and validated
set for an agnostic agent. Resolving it is what `EnrichProviders` names, so it is
filled from that level upward and is empty at `EnrichNone`, where no provider fact
was asked for, and empty when an agnostic agent is resolved without a provider set
(the `EnrichNotApplicable` case). It is distinct from the embedded
`KnownAgent.Provider`, which stays the static catalog declaration (empty for an
agnostic agent). The `providers` output field renders from `Agent.Providers`, so
the CLI needs no agnostic-versus-home branching to build it.

`ResolvedPaths` is carried forward with its current fields. The catalog `PathPair`
type and the `VersionProbe` recipe become unexported: raw path pairs and version
probing are internal to detection and are not part of the public surface.

### R4 Enrichment level and enrichment state

`Enrich` selects how much provider and models.dev data an agent operation
attaches. It is the single demand axis: each level is a superset of the one
below it, and what the operation resolves and reports — whether models.dev is
contacted, whether caller-supplied provider ids are validated against it, and
whether the agnostic-needs-providers condition arises at all — is keyed off it.

```go
type Enrich int
const (
    EnrichNone      Enrich = iota // catalog and detection facts only
    EnrichProviders               // + the resolved provider set
    EnrichCount                   // + ProviderEnv, ProviderCoverage, and ModelCount
    EnrichFull                    // + the full Models list
)
```

`EnrichNone` is silent and offline for every agent, agnostic or not: no provider
resolution, no warning, no models.dev round-trip. It is what a caller that wants
only identity, paths, version, or binary facts asks for. Supplying provider ids
alongside it leaves the query offline on `Agents.Get`: ids that will not be
reported are not validated against models.dev. `Agents.List` is the exception, and
the only one — its provider set is listing-wide input that is validated at the
boundary at every level (R8).

What the level governs is what is resolved, reported, and validated against
models.dev, never what is rejected as malformed input. A provider set given to a
home-provider agent contradicts catalog data already in hand, so that rejection is
level-independent (R8); only a verdict that models.dev alone can give is deferred
by an offline level.

`EnrichProviders` resolves the provider set and nothing else. For a home-provider
agent that is the catalog list, which is offline catalog data, so the level costs
no models.dev round-trip. For an agnostic agent the set is caller input rather
than catalog truth, so the ids are validated against models.dev before they are
reported (`ErrUnknownProvider` on a miss) and the level does contact models.dev.
This asymmetry is the point of the level: the resolved provider set is the one
fact whose source differs by agent kind.

`EnrichCount` and `EnrichFull` are models.dev-backed for every agent: both fill
`ProviderEnv` and `ProviderCoverage`, `EnrichCount` adds `ModelCount`, and
`EnrichFull` adds `Models`. What separates them is the models list, not the
round-trip: models.dev arrives as one document, so both levels pay the same fetch
and `EnrichCount` is the level for a caller that wants provider-env and coverage
without a models list attached to every agent. That is what an unfiltered agent
detail needs, which reports provider-env and coverage and carries no models until
they are asked for. `ModelCount` is the summary fact that level can hand over for
free; it is not the source of any count the CLI renders. The levels that save the
fetch are the two below: `EnrichNone`, and `EnrichProviders` over home-provider
agents.

Installation status gates none of this. Detection decides `Found`, `BinaryPath`,
and `Version`, and nothing else: the provider set is catalog data, the models are
models.dev data, and provider-env is read from the environment, so every level
resolves the same way for a catalogued agent whose binary is absent as for one
that is present. Coverage is probed and caller-supplied provider ids are
validated for an uninstalled agent exactly as for an installed one. This is what
lets a caller ask what an agent offers before installing it, and it makes agent
detail consistent with the listing, which already reports model counts for
uninstalled agents. There is no not-installed enrichment state because
not-installed omits nothing; `WarnNotInstalled` reports it as the status it is.

`EnrichmentState` records the outcome of enrichment on each returned `Agent`,
replacing the nil/empty/null encodings the CLI uses today:

```go
type EnrichmentState int
const (
    EnrichNotRequested EnrichmentState = iota // Enrich was EnrichNone
    EnrichApplied                             // the requested level was satisfied in full
    EnrichNotApplicable                       // agnostic agent with no providers supplied
    EnrichDegraded                            // models.dev could not fill it; the count is not a true zero
)
```

A caller distinguishes an agnostic agent shown without a provider set
(`EnrichNotApplicable`) from a real empty result and from a models.dev failure
(`EnrichDegraded`) by this field alone.

`EnrichDegraded` covers both ways models.dev can come up short: unreachable and
uncached, and reachable but serving data agentdex cannot parse. The state answers
one question — can this count be trusted — and both faults answer it the same way,
so they share the state; the accompanying warning kind says which fault it was
(`WarnModelsUnreachable` or `WarnModelsSchemaDrift`, R6). Encoding drift as
`EnrichApplied` instead would make an unparseable catalog indistinguishable from
an agent that genuinely offers no models, which is the ambiguity this type exists
to remove.

`EnrichApplied` is level-relative: a home-provider agent at `EnrichProviders` is
applied once its catalog provider set is on the `Agent`, with no models.dev data
owed at that level.

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
    CoverageNotProbed CoverageStatus = iota // the level reached no models.dev; no verdict
    CoverageAllPresent
    CoverageSomePresent
    CoverageNonePresent
    CoverageUnreachable
    CoverageSchemaDrift
)
```

Coverage is a verdict on the catalog entry's provider ids against models.dev, so
it is computed identically whether or not the agent's binary is installed (R4). A
catalog entry naming a provider models.dev does not know is the same data fault
on a machine that has the agent and one that does not.

Coverage is only probed at `EnrichCount` and above; the two levels below reach no
models.dev at all (R12), so they reach no verdict. `CoverageNotProbed` is that
outcome and it is the zero value, which makes every other status a positive
verdict a probe actually established. This is the encoding `EnrichmentState`
already uses for the same question one requirement earlier (R4): a caller must be
able to tell "nothing was asked" from "the answer was yes", and an
`AgentDetail` whose zero-valued coverage read as `CoverageAllPresent` would
assert a models.dev result nothing checked. `Present` and `Absent` are empty at
this status.

There is no empty-provider-set verdict, because no path reaches one. The coverage
set is the catalog provider list for a home-provider agent, and the schema makes
that list non-empty (`provider` is required, with at least one id, unless
`agnostic` is true); for an agnostic agent with no provider set R8 answers with
`EnrichNotApplicable` and probes no coverage at all. The rollup the CLI carries
today has such a branch and nothing can reach it; it does not come across.

### R6 Structured warnings

Warnings are structured and carry their own human-readable message. The library
sets the message; a caller emits it verbatim, adding a remedy clause only where
the remedy names something the caller alone owns (see below).

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

Warnings ride on the returns of the operations that raise them. Every `Result`
carries a `Warnings` slice, and `AgentDetail` carries one; `Providers.Get` and
`Models.Get` return their bare embedded type with no warnings channel, because
they load no agent catalog and emit none of the enrichment, coverage, or
not-installed conditions. `AgentDetail` is the one detail type with a `Warnings`
field: agent detail is the only exact fetch that resolves the catalog, probes
coverage, and can be not-installed.

When the loaded catalog is a stale fallback (`CatalogStale` reports true), the
return of every operation that resolved the agent catalog must include a
`WarnStaleCatalog` warning: the `Result` from `Agents.List` and from
`Models.List` scoped by agent, and the `AgentDetail.Warnings` from `Agents.Get`.
A pure models.dev operation loads no catalog, so it has no stale state to report
and none to inject. Enrichment failures (models.dev unreachable and uncached, or
recognisable schema drift) never fail a `List`; they degrade the result and
attach the matching warning.

The message wording carried in `Warning.Msg` is the library's to own, and each
message is preserved verbatim from the string the CLI emits for that condition
today — save for the one remedy clause split out below — so the retained CLI
end-to-end tests pass their warning assertions unchanged. `Kind` and `Msg` are one-to-many: the kind classifies the condition so
a caller can branch on it, while the message says what that condition cost the
operation that raised it. The same kind therefore carries different wording from
different operations, and an implementation that maps a kind to a single string
loses one of them.

`WarnModelsUnreachable` is where this bites: the same outage costs a listing its
model counts and costs an agent detail its enrichment and provider-env, and each
surface says so in its own words.

| Raised by | Message |
|---|---|
| `Agents.List` | `model counts unavailable: models.dev is unreachable and not cached` |
| `Agents.Get` | `models.dev is unreachable and not cached: model enrichment and provider-env omitted` |

Neither is asserted today, so nothing catches a collapse into one message. R18
requires both to be asserted by full-string equality.

A message states the condition. Where the remedy for that condition names
something only the caller has — a flag, a subcommand, an interface affordance —
the caller supplies that clause, because the library has no such vocabulary and a
second caller would print advice for a command line its user is not running.
`WarnProvidersRequired` is the only kind this reaches, and the split is fixed by
today's wording:

| Layer | Text |
|---|---|
| library `Warning.Msg` | `"<id>" is provider-agnostic` |
| CLI-appended remedy | `: supply --provider with models.dev provider ids to enrich providers, provider-env, and models` |

Concatenated they are the string the CLI emits today, character for character, so
the full-string assertion R18 adds at step 0 is written once and never edited.
Every other kind's message is complete as the library sets it and is emitted
unchanged.

`WarnNotInstalled` is the one message that changes, and it changes by shrinking.
Today it grows a ": models and provider-env omitted" suffix when the caller asked
for models.dev-backed data, because the CLI skipped the round-trip. Enrichment no
longer depends on installation (R4), so nothing is omitted and the message is the
status alone: `agent "<id>" is catalogued but not installed`.

### R7 Error set

The exported sentinels are:

```go
var (
    ErrCatalogUnavailable = errors.New(...) // cold offline, no previously resolved catalog version
    ErrModelsUnavailable  = errors.New(...) // models.dev unreachable and uncached on a Providers/Models operation
    ErrAgentUnknown       = errors.New(...) // agent id not in the catalog
    ErrUnknownProvider    = errors.New(...) // provider id not known to models.dev
    ErrProvidersRequired  = errors.New(...) // model listing scoped to an agnostic agent with no provider set
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

A non-schema models.dev fetch failure (unreachable and uncached) on a `Providers`
or `Models` operation surfaces as `ErrModelsUnavailable`, the models.dev analog of
`ErrCatalogUnavailable`. The library wraps the underlying `modelsdev` error at the
agentdex boundary, so `modelsdev` stays unchanged (R16). Agent operations do not
return it: a models.dev outage there degrades the result with `EnrichDegraded` and
a `WarnModelsUnreachable` warning (R4, R6) rather than failing.

### R8 Scope resolution and agnostic rules in the library

The library owns the agnostic/home-provider rules; they must not live in the CLI.
Two rules hold for every operation:

- Home-provider agent, no explicit providers: enrich against the agent's catalog
  providers.
- Agnostic agent, providers supplied: validate each id against models.dev
  (`ErrUnknownProvider` on a miss); enrich against them.

Validation is a verdict models.dev has to be able to give. When it cannot be
reached at all — unreachable and uncached — there is no unknown-id verdict to
report, and a caller's ids are not rejected on the strength of an outage.
Validation is skipped, the operation carries on, and the outage surfaces through
the path that already owns it: `EnrichDegraded` with `WarnModelsUnreachable` on
an agent operation (R4, R7), `ErrModelsUnavailable` on a `Providers` or `Models`
operation. Recognisable schema drift is a data fault, not an outage, and keeps
its own treatment.

A home-provider agent given an explicit provider set is `ErrProvidersNotAllowed`,
but only in the single-target operations, where the provider set unambiguously
targets that one agent: `Agents.Get` and `Models.List` scoped by `--agent`. The
verdict is a comparison against the catalog entry, so it needs no models.dev and
is not gated by `Enrich`: `Agents.Get` raises it at every level, `EnrichNone`
included, before any level-dependent resolution runs. A field selection therefore
never decides whether a contradictory provider set is accepted.

`Agents.List` takes `AgentQuery.Providers` as a listing-wide enrichment set for
its agnostic rows alone; a home-provider row ignores it and enriches from its
catalog providers, so `agents list --provider <id>` never fails on the
home-provider agents a mixed catalog always contains.

The agnostic-without-providers case splits by whether the operation has a partial
answer to give. An agent operation always does — identity, paths, and version are
real facts that need no provider — so it degrades and warns. A model listing does
not: without a provider set there is nothing to list, so it fails.

Agent detail resolution — `Agents.Get`:

- Home-provider agent, providers supplied, any `Enrich`:
  `ErrProvidersNotAllowed`, decided from the catalog entry before the level is
  consulted.
- `Enrich == EnrichNone`, provider set absent or accepted: nothing
  provider-related is resolved for any agent, so the agnostic case does not
  arise. No provider set, no validation, no warning, no models.dev round-trip.
  `Enrichment` is `EnrichNotRequested`.
- Agnostic agent, providers supplied, `Enrich >= EnrichProviders`: validate each
  id against models.dev and resolve against them.
- Agnostic agent, no providers, `Enrich >= EnrichProviders`: never an error.
  Outside facts only (no providers, provider_env, coverage, or models), with
  `Enrichment == EnrichNotApplicable` and a `WarnProvidersRequired` warning. A
  caller that treats an explicitly requested but unfillable field as a fault maps
  the not-applicable state itself; the library reports the state, not a verdict
  on it (R15).

Model listing resolution — `Models.List` scoped by `--agent`:

- Agnostic agent, no providers, any filter: `ErrProvidersRequired`.
- Agnostic agent, providers supplied: validate each id, list across them.

Agent listing resolution — `Agents.List`:

- Agnostic agent, no providers, any `Enrich`: never an error. The agent is
  returned with `Enrichment == EnrichNotApplicable` and no models data, and the
  `Result` gains no per-agent warning. The not-applicable state is the whole
  signal; the CLI renders it as the `-` cell, matching today's silent listing.
- `AgentQuery.Providers` is validated once at the boundary, before any agent is
  resolved, independently of which agents the query returns and at every `Enrich`
  level. A caller-supplied id is caller input whatever the catalog holds, so an
  unknown one is `ErrUnknownProvider` whether the listing contains an agnostic
  agent, contains only home-provider agents, or is narrowed by `Installed` or
  `Filter` to nothing at all. The verdict must not depend on which binaries a
  machine happens to have. This is the one models.dev contact an `EnrichNone`
  agent operation makes, and it is why the rule is stated for the listing alone:
  agent detail reports nothing it did not resolve, so it has no unvalidated id to
  hand back (R4).

`Models.List` with `ModelQuery.Scope.Agent` set resolves the agent to its provider
set by the rules above, and an unknown agent id is `ErrAgentUnknown`. With no
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

Every environment read that shapes what the library reports enters at one point.
`Open` accepts `WithEnvLookup(func(string) (string, bool))`, defaulting to
`os.LookupEnv`, and it is the source for both of them, so a caller that supplies
it gets reported data that is a function of its inputs rather than of the host
process.

It governs both of the library's data-shaping environment reads:

- Provider env-var presence. `Provider.EnvPresent` and `Agent.ProviderEnv` are
  populated through the lookup, and only presence is taken, never the value.
- Catalog path resolution. Catalog paths are written with `~` and `$VAR` forms, so
  expanding one takes both the named variables and the home directory. Both come
  from the lookup: `$VAR` expansion resolves each name through it, and `~`
  expansion resolves `HOME` through it, keeping the existing `os.UserHomeDir`
  fallback for the case where `HOME` is unset. Path expansion is already written as
  a pure function of a captured environment; this changes where that capture comes
  from, not how expansion works.

Scoping the lookup to provider-env alone would leave every agent's config and
skills paths resolving against the ambient process environment, which is the input
a caller most needs to control and the one the Constraints rule names.

One environment read stays on the process environment: resolving the default
cache directory from `XDG_CACHE_HOME`, with its home fallback, when `WithCacheDir`
is not supplied. It happens twice — in the catalog loader and again inside
`modelsdev` — and the `modelsdev` half cannot move, because taking a lookup there
means changing that package's exported surface, which R16 forbids. Splitting the
rule so one cache directory honours the lookup and the other does not would be
worse than either whole answer, so both stay put. This read decides where bytes
are cached, not what the library reports, so a caller that supplies a lookup still
gets reported data that is a function of its inputs. `WithCacheDir` is the option
that controls it, and it is why R18 keeps `t.Setenv` for the cache-directory
tests.

### R11 Options for Open

`Open` accepts the following functional options, folding in the catalog,
models.dev, detection, and boundary settings the CLI configures today, less the
one this project removes:

```
WithCatalogModule(path string)           // catalog module path override
WithCatalogDir(dir string)               // load the catalog from a local CUE module directory
WithCatalogTTL(d time.Duration)          // catalog version-resolution TTL
WithCacheDir(dir string)                 // cache directory for catalog and models.dev
WithModelsURL(url string)                // models.dev catalog source URL
WithModelsTTL(d time.Duration)           // models.dev cache TTL
WithSearchDirs(dirs ...string)           // extra binary search locations
WithBinPaths(m map[string]string)        // per-agent binary path overrides
WithEnvLookup(fn func(string) (string, bool)) // provider-env presence and path expansion (R10)
WithHTTPClient(hc *http.Client)          // HTTP client for models.dev
WithLogger(l *slog.Logger)               // structured logger; defaults to a discard handler (R19)
```

The per-`Detect` `WithSkipVersion` and `IncludeMissing` from the old surface are
gone: version probing is an internal concern, and inclusion of undetected agents
is governed by `AgentQuery.Installed`.

Agent disabling goes with them, and it goes further than the option: `WithDisabled`
is removed from the library, `disabled_agents` from the config schema, `Disabled`
from `config.Config`, and the skip branch from the detection walk. agentdex indexes
data and reports detection; a per-user preference that hides a catalogued agent
from one listing is neither, and it earns a permanent semantic question — whether a
disabled agent is also absent from an exact `Agents.Get` and from a `Models.List`
agent scope — that the new surface should not have to answer. `AgentQuery.Filter`
and `AgentQuery.Installed` are the narrowings the listing keeps, and both describe
the data rather than suppressing it.

Removal is visible to anyone who set the key. Because `#Config` is closed, an
unknown field is a load-time error, so a `config.cue` carrying `disabled_agents`
fails to load with a config fault (exit 78) rather than silently ignoring it. That
is the correct outcome — a setting that no longer does anything must say so — and
the key is undocumented in `README.md` and `AGENTS.md` and has never had a
behavioural test, so the exposure is small. The config-load failure message names
the unknown field already; no bespoke handling is added for this one key.

`WithCatalogDir` is a second catalog source, not a variant of the module path.
The catalog is loaded by evaluating the CUE module rooted at `dir` — the same
validate-and-decode step a fetched module goes through, so `schema.cue` travelling
with the data still does the validating. No version is resolved and no registry is
contacted, so the directory source needs no network on any run, is never stale,
and makes `WithCatalogTTL` and the catalog half of `WithCacheDir` inert, along
with the catalog target of `Refresh` (R13). It wins over `WithCatalogModule` when
both are given.

The directory source exists because editing the catalog and seeing the result is a
first-class workflow: an agent added to `catalog/agents.cue` must be confirmed
through `agentdex agents list` and `agents get` before a version is published, and
a module path cannot express an unpublished working tree. The CLI reaches it
through a new `catalog.dir` key in `config.cue`, mapped to `WithCatalogDir`
alongside the existing `catalog.module` mapping. This adds one optional
configuration key; it adds no command and no flag, so the fixed CLI contract in
Constraints is untouched.

### R12 Lazy resolution

`Open` performs no network I/O. The agent catalog is resolved lazily on the first
operation that needs it (any `Agents` operation, a `Models.List` scoped by agent,
`Refresh` of the catalog target, or `CatalogStale`). models.dev is fetched lazily
on the first operation that needs it: any `Providers` or `Models` operation, an
agent operation at `EnrichCount` or `EnrichFull`, any operation with
caller-supplied provider ids to validate, and an `Agents.List` at any level whose
query carries a provider set (R8). An `Agents.Get` at `EnrichNone`, and an agent
operation at `EnrichProviders` over home-provider agents only, resolve entirely
from the catalog and never contact models.dev.

This preserves today's behaviour that a pure models.dev operation (a provider
listing) does not require the agent catalog, that an agent query for offline
catalog facts does not require models.dev, and that a cold-offline first run
fails only when an operation actually needs the unresolvable catalog, with
`ErrCatalogUnavailable`. A catalog supplied by `WithCatalogDir` is read from disk
on that same first need and reaches no registry, so it never raises
`ErrCatalogUnavailable` and never reports stale.

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

A refreshed target replaces the `Index`'s own resolved state for that target, so
the operations a caller makes next serve the refreshed data. This is the point of
the operation and it does not follow from forcing the on-disk caches: the
`modelsdev.Client` memoises its merged catalog for its lifetime and never
re-merges, so a refetch performed through a second, force-refresh client leaves
the `Index`'s existing client serving the pre-refresh answers. `Refresh` therefore
installs the refreshed models.dev client in place of the old one, and re-resolves
the agent catalog the lazy path holds, along with the staleness `CatalogStale`
reports from it. A target that failed to refresh leaves its existing state
untouched, so a failed refresh never costs a caller a working index — it is the
error return, not a reset.

A catalog supplied by `WithCatalogDir` has no version to re-resolve, so the
catalog target is not refreshed and is not failed either: `Refreshed.Catalog` is
false and `Refresh` returns no error, leaving the models.dev half of `TargetAll`
to run and report as usual. Reporting it as refreshed would claim a re-resolution
that never happened, and failing it would send the bare `agentdex refresh` — which
defaults to every target — to a transient fault on the very machine the directory
source is meant to serve. A directory catalog is always current; nothing to
refresh is the honest answer. The CLI knows which source is configured, because it
maps the `catalog.dir` key, so it omits the catalog success line and emits a
warning naming the directory as the reason — stderr in text mode, the envelope's
`warnings` key under `--json`, like every other warning. That is presentation over
a fact the library already reports, and it needs no warnings channel on `Refresh`.

The `Index` is safe for concurrent use, so this replacement is the one point where
that matters: a refresh landing while another goroutine is mid-operation must
leave that operation on the state it started with, and hand the refreshed state to
the operations that follow.

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
empty-state and price-footer formatting, arbitrary-field ordering (R14), the
exit-code taxonomy, and the flag-naming remedy clause the CLI appends to
`WarnProvidersRequired` (R6).

The models table's provider column follows from that ownership. It is shown when
the returned rows span more than one distinct provider, decided from the rows the
CLI renders. Today the CLI decides it from the size of the resolved provider
scope, which it can no longer see once scope resolution moves into the library
(R8) — and which was only ever a proxy: the column disambiguates ids that would
otherwise collide, so the rows are what it is a property of. A scope of several
providers where only one contributes rows yields a column of one repeated value
that disambiguates nothing. `Result[Model]` therefore carries no resolved scope,
and the three services keep the one symmetric result type; a column-visibility
rule is not a reason to widen the library's returns.

Because `--fields` and `--models` are the CLI's own surface, translating them into
an `Enrich` level is presentation, and the CLI owns it. `agents get` maps its
requested output to the lowest level that can fill it, so a field selection never
pays for data it does not show:

| Requested output | Level |
|---|---|
| no provider-related field (`--fields id,bin,skills_dir`) | `EnrichNone` |
| `providers` only | `EnrichProviders` |
| unfiltered detail, or `provider_env` selected | `EnrichCount` |
| `--models`, or `models` selected | `EnrichFull` |

The table governs `agents get` alone. `agents list` has no field-driven choice to
make: it requests `EnrichFull` on every invocation, whatever `--fields` selects,
because its JSON payload carries each agent's full model array while the text
column renders that array's length. Enriching the listing at a lower level would
change the `models` key from an array of model objects to a count or drop it
altogether, which the fixed envelope contract does not allow.

The same ownership settles strictness. `Agents.Get` reports an agnostic agent
without a provider set as `EnrichNotApplicable` with a `WarnProvidersRequired`
warning and never fails (R8), because whether an unfillable field is tolerable
depends on whether the user named it. An unfiltered detail is a browse: it emits
the warning and exits 0. A `--fields` selection or `--models` that names a field
the not-applicable state leaves empty is an explicit request the CLI cannot
honour, so it wraps `ErrProvidersRequired` into the usage fault it emits today.
This is a rule about the CLI's own flags, not about agents, and it is the only
provider-related decision the CLI keeps.

The CLI maps library facts to exit codes as follows:

| Library fact | Exit code |
|---|---|
| `ErrCatalogUnavailable`, `ErrModelsUnavailable` | 75 transient |
| `ErrAgentUnknown`, `ErrNotFound` | 3 not-found |
| `ErrUnknownProvider`, `ErrProvidersRequired`, `ErrProvidersNotAllowed`, `ErrMalformedModelID` | 2 usage |
| `modelsdev.ErrModelsSchema` | 78 config |
| `AgentDetail.Coverage.Status` = `CoverageNonePresent` or `CoverageSchemaDrift` | 78 config, agent still reported |
| `EnrichNotApplicable` on a field the user named explicitly | 2 usage, wrapping `ErrProvidersRequired` |
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

`AGENTS.md`'s add-an-agent procedure needs a correction beyond the rename. Its
"Exercise through the library" step tells a contributor to point the loader at the
local module with the `catalog.module` override, which never worked: that override
is a module path the registry resolves, so an unpublished working tree cannot be
named by it. Rewrite the step against the directory source (R11) so the documented
workflow is the one the code performs.

### R18 Test coverage

Test coverage is a deliverable of this project, not a side effect. The rewrite
must leave the codebase at least as well tested as it is today, with the relocated
behaviour tested where it now lives.

- Behaviour relocated from the CLI into the library (scope resolution, the
  provider coverage rollup, the composite model split, models-across-providers
  assembly, enrichment degrade classification, the agnostic/home rules, the
  not-found versus not-installed distinction) must gain direct library-level
  tests. Do not leave that behaviour verified only through the CLI.
- The root-package tests that exercise the removed API (`agentdex_test.go`,
  `catalog_test.go`, `enrich_test.go`) are rewritten to exercise the new surface.
  No behavioural assertion they carry is dropped without an equivalent assertion
  against the new API, save for the two classes this project retires by design:
  the fuzzy-match assertions in `resolve_test.go`, whose subject R1 deletes with
  no successor, and any assertion whose state the catalog schema cannot express
  (see the fixture rule below).
- Every new public operation is tested directly: `Open` and its options, the
  three services' `List` and `Get`, `Refresh`, `CatalogStale`, the R7 error set,
  the `EnrichmentState` and `ProviderCoverage` outcomes, the warning injection,
  and lazy resolution including the cold-offline `ErrCatalogUnavailable` path.
- `Refresh` is tested through the same `Index`, not just by its return value: an
  operation run before the refresh and the same operation run after it, against a
  source whose content changed in between, must return the old data and then the
  new. A test that only asserts `Refreshed` cannot see the memoised-client trap
  R13 exists to close. Assert too that a failed target leaves the index serving
  what it served before.
- Each `Enrich` level is tested for what it attaches and what it costs, with the
  no-fetch levels proved against a models.dev endpoint that fails the test if it
  is contacted at all (the existing `mustNotFetchModelsServer` double): an
  `Agents.Get` at `EnrichNone`, with and without a provider set, and an
  `EnrichProviders` operation over home-provider agents, must complete without
  contacting models.dev and without a `WarnProvidersRequired` warning, while
  `EnrichProviders` over an agnostic agent with caller ids must validate them.
  The no-fetch levels must also report `CoverageNotProbed` (R5), so the unprobed
  state is asserted rather than inferred from an empty struct.
- The CLI's field-selection-to-level mapping (R15) is asserted end to end for
  each row of its table, including that a non-provider `--fields` selection on an
  agnostic agent stays offline, silent, and exits 0, that an explicitly
  named unfillable field is the usage fault, and that `--provider` on a
  home-provider agent is the same usage fault at every level, including a
  non-provider `--fields` selection that maps to `EnrichNone`.
- The CLI end-to-end tests that drive `NewRootCommand` with captured output are
  retained as the observable-behaviour oracle. They verify that the rewrite did
  not change the JSON envelope, exit codes, warnings, ordering defaults,
  `--fields`, or empty-state output. A change forced on one of these assertions is
  a signal to investigate a regression, not to edit the assertion.
- Tests follow the repository practice: real CUE validation, real files via
  `t.TempDir()`, table-driven cases, and real behaviour over mocks. Library tests
  isolate the environment through the injected `WithEnvLookup`, which now covers
  path expansion as well as provider-env (R10), rather than through `t.Setenv`;
  `t.Setenv` stays where the process environment really is the subject, in the CLI
  end-to-end harness and the config and cache-directory tests. The models.dev and
  catalog test doubles already in the suite are reused rather than replaced; the
  ones a library test needs and `internal/cli` currently owns privately — the
  models.dev fixture server, `closedModelsServer`, and `mustNotFetchModelsServer` —
  move to a shared test package rather than being copied.
- A library test supplies its catalog as a CUE module directory loaded through
  `WithCatalogDir`. The old surface let a test hand `Detect` a `Catalog` built in
  Go; the new surface has no such option and needs none, because a fixture module
  is validated by the same evaluation the published catalog goes through, so a
  fixture that the real schema would reject cannot pass a test.

  The module is materialised at test time rather than checked in per shape. A
  helper in `internal/catalogtest` writes a module into `t.TempDir()` from an
  inline `agents.cue` body supplied by the test, `cue.mod/module.cue`, and the
  repository's own `catalog/schema.cue` read from disk. One schema governs every
  fixture, so a schema change cannot leave a stale copy behind, and a test
  declares only the entries it cares about. This matters because the shapes the
  rewritten suite needs are not a handful: beyond the agnostic entry, the
  home-provider entry whose provider models.dev does not know (the coverage data
  fault), the mixed catalog, and the entry with no version probe, the relocated
  detection assertions need a search-dir-versus-PATH binary, a binary-path
  override driving the version exec, a multi-agent catalog for ordering and the
  `Installed` narrowing, an entry with no skills block and no local config, an
  entry whose paths carry a `$VAR` to expand, and a multi-agent catalog for the
  concurrent fail-fast path. Checked-in directories at that count would duplicate
  the schema a dozen times over.

  A state the schema forbids is not a fixture and not a test. A non-agnostic entry
  with no providers is unreachable — `provider` is required unless `agnostic` is
  true — so the current assertion that such an agent leaves `ProviderEnv` nil
  retires rather than migrating, for the same reason R5 carries no
  empty-provider-set verdict.

  The two checked-in fixture modules under `testdata/` stay, because the registry
  tests publish a real module and need a stable one to publish. Reserve them, and
  the in-process OCI registry harness, for the tests that are about registry
  behaviour itself — version resolution, the TTL, the stale fallback, and the
  cold-offline `ErrCatalogUnavailable` path — which the directory source bypasses
  by design. That harness exists today only as two private copies, in
  `internal/catalog/registry_oci_test.go` and `internal/cli/harness_test.go`;
  promote it into `internal/catalogtest` alongside the module-materialising helper
  rather than adding a third copy for the root package.

The CLI end-to-end suite is a strong oracle on exit codes and the noun/verb happy
paths, but it has blind spots where an observable behaviour is exercised without
being asserted, so a rewrite could regress it with no test failing. Close these
gaps first, as step 0 of the Implementation Plan.

The sequencing is the point, not a preference. An assertion detects a regression
only if it was written against the behaviour it protects. Written after the
rewrite, each of the assertions below records whatever the new CLI prints, the
suite goes green, and the twelve areas named here stay exactly as unprotected as
they are today — silently, because nothing fails. Written against the current
code, before step 1 removes anything, each one turns its area into a hard failure
if the rewrite moves it. None of them needs the new surface: they are assertions
about the CLI exactly as it stands, so they can land in full before the redesign
begins. The rest of R18's test work stays in step 9, where the relocated policy's
tests land with the code they cover.

- The `--json` failure envelope is asserted only on usage (2) and not-found (3).
  Assert the envelope shape (`status`, `error`, and `data` presence, and the
  `omitempty` behaviour of the `error`/`warnings` keys) on a config (78), a
  transient (75), and a permission (4) failure as well.
- Warnings are asserted only by substring today, so a reworded message passes.
  Assert each warning message by full-string equality — stale catalog, both
  models.dev unreachable degrade messages (the listing's and agent detail's, which
  differ per R6), schema-drift omission, some-providers-absent, not-installed, and
  the agnostic-needs-provider guidance — so the verbatim wording R6 requires is
  actually enforced.
- `agents get` on a none-present or schema-drift data fault asserts only exit 78.
  Assert that the agent payload is still reported on that fault (R5, R15).
- The JSON null-versus-`[]` model-count distinction is pinned, but the text-cell
  distinction is not. Assert that an agnostic/not-applicable agent renders the
  `-` cell and a degraded agent renders `0`, for both degrade causes — models.dev
  unreachable and models.dev drifted — which share `EnrichDegraded` and differ
  only in their warning kind (R4).
- Only the filter-matched-nothing empty-state line is asserted. Assert the
  genuine-empty line (no filter) for agents, models, and providers too.
- `refresh` against a reachable but malformed models.dev has no test. Assert it
  exits 78 (schema drift as config), matching the other model surfaces.
- `refresh all` and the default target assert only that two caches refreshed, not
  which. Assert the identity of the refreshed targets and the success wording for
  both, so a refresh that silently drops one target fails. The directory-catalog
  case — with `catalog.dir` set, `refresh` exits 0, refreshes models.dev alone,
  omits the catalog success line, and warns that the catalog comes from a
  directory — is new behaviour and lands with R13 in step 9, not here.
- `agents get` some-present asserts the warning but not the surviving data. Assert
  that the present provider's models still populate.
- The `--provider`-on-home-provider rejection asserts the exit code but not the
  message. Assert the guidance text.
- Warnings-to-stderr in text mode is spot-checked on one command. Assert the
  stream discipline across commands: warnings to stderr in text mode and into the
  envelope under `--json`, data to stdout.
- The models table's provider column is not asserted at all. Assert it at step 0
  against the scope-based rule the CLI applies today: shown for a multi-provider
  scope, absent for a single-provider one, including the case a filter narrows a
  multi-provider scope to one provider's rows. That last case is the one exception
  three inverts, so it is rewritten to the row-based rule with R15, and the other
  two must keep passing unchanged across the switch.
- Listing-wide provider validation is asserted only where an agnostic agent is
  catalogued. Assert that `agents list --installed --provider <unknown>` is the
  same usage fault when no agnostic agent is in the result set, and that a
  listing with `--provider` against an unreachable, uncached models.dev still
  exits 0 with the degrade warning rather than rejecting the ids (R8).

Five of the step-0 assertions are themselves updated later, and only these: the
four below, which encode the not-installed short-circuit R4 removes, and the
provider-column case exception three inverts. They are updated deliberately, and
they are the only end-to-end assertions this project is permitted to change; every
other forced change stays a regression signal.

- `TestGetNotInstalledEnrichmentOmissionWarning`: the omission suffix is gone.
  Assert the bare not-installed status message, and assert that `--models` on an
  uninstalled agent now fills the models list.
- `TestGetAgnosticSoftPathNotInstalled` and `TestGetAgnosticProviderNotInstalled`:
  `provider_env` and `models` are no longer absent by virtue of the agent being
  absent. Assert them against the same rules an installed agnostic agent follows —
  omitted without `--provider`, filled with it.
- Add the case none of them covers: an uninstalled agent whose catalog provider is
  missing from models.dev reports the same coverage data fault (exit 78, agent
  still reported) as an installed one.

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
  and price-footer rendering are unchanged by this project. Three exceptions are
  named, and no other end-to-end assertion may change.
- Exception one: agent detail on a not-installed agent, which now enriches like any
  other agent (R4): its payload gains `provider_env` and `models`, its warning
  loses the omission suffix (R6), and its coverage verdict is reported. The four
  assertions this changes are listed in R18.
- Exception two: the `disabled_agents` config key is removed (R11), so a
  `config.cue` that sets it now fails to load as a config fault instead of hiding
  the named agents from `agents list`. The config schema is the only surface this
  touches: no command, flag, envelope key, or exit-code mapping changes with it.
- Exception three: the models table's provider column is decided from the returned
  rows rather than the requested scope (R15). The two rules agree whenever every
  scoped provider contributes a row; where one does not — a filter matching one
  provider's models, a provider with no models, an `--agent` naming a provider
  models.dev does not carry — the column is now absent instead of repeating a
  single value. The JSON payload is unaffected: `provider` is a field of every
  model record at every scope.
- Agent ids stay kebab-case; the catalog map key remains the single source of
  agent identity.
- Commit messages follow Scoped Commits.

## Implementation Plan

0. Harden the CLI oracle (R18). Add the twelve missing end-to-end assertions
   against the current code, before any API work begins, and commit them
   separately. They must pass on the unmodified repository; an assertion that
   needs the new surface belongs to step 9, not here.

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
   and thread the logger to the decision points. Add the catalog directory source
   and its `catalog.dir` config key, and remove `disabled_agents` and its mapping.
   Move the `internal/config` option mapping so that `config.Config` produces
   `[]agentdex.Option` for `Open`; the config package keeps ownership of
   `config.cue` loading.

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
   `WarnStaleCatalog` injection into the `Result` and `AgentDetail` returns of the
   catalog-resolving operations (R6, R13).

7. Remove the old public API (R1) and relocate the detection, probe, resolve,
   enrich, and version mechanics to unexported scope or `internal/`. Delete
   `resolve.go` and `internal/match` together. Confirm the root package's exported
   surface is exactly R2 through R13.

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

Step 0 precedes everything and is committed on its own. Steps 3, 4, and 5 depend
on 1 and 2. Steps 7 and 8 depend on 3 through 6. Step 9 runs alongside 3 through
8, so each relocated piece of behaviour lands with its tests.

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
   `ErrUnknownProvider`. `Agents.Get` raises `ErrProvidersNotAllowed` at every
   enrichment level, so a `--fields` selection cannot turn the rejection into a
   silent success.
5. A stale catalog surfaces as a `WarnStaleCatalog` warning on the returns of the
   operations that resolve the agent catalog — the `Result` from `Agents.List` and
   `Models.List` scoped by agent, and `Agents.Get`'s `AgentDetail.Warnings` — and
   `Index.CatalogStale(ctx)` reports true.
6. An agnostic agent without a provider set is `EnrichmentState ==
   EnrichNotApplicable` with a `WarnProvidersRequired` warning at
   `EnrichProviders` and above, and never fails `Agents.Get`; at `EnrichNone` it
   is `EnrichNotRequested`, silent, and contacts no models.dev; a `Models.List`
   scoped to it is `ErrProvidersRequired`; a models.dev outage on a count
   enrichment carries `EnrichmentState == EnrichDegraded` and does not fail the
   operation.
7. `agents get <id> --models` on a catalogued agent whose binary is absent fills
   the same providers, provider-env, coverage, and models an installed agent
   fills, warns only that the agent is not installed, and exits 0 — or 78 with the
   agent reported when the coverage verdict is a data fault, as for an installed
   agent.
8. `Models.Get` resolves a composite by first-slash split with the R9 error
   behaviour, and fills `CanonicalID` from the models.dev agnostic map.
9. `Open` exposes `WithLogger`, defaults to a discard handler when it is not
   given, and the CLI passes its `--debug` logger through, so running a command
   under `--debug` emits the library's decision logs (catalog resolution,
   models.dev fetch, enrichment degrade, coverage verdict, refresh) to stderr.
10. `WithCatalogDir` loads the catalog by evaluating a local CUE module directory
    with no registry contact, so an agent added to a working-tree `catalog/` is
    visible to `agents list` and `agents get` — through the `catalog.dir` config
    key — before any version is published, and an entry the published schema would
    reject fails there. With that key set, `agentdex refresh` exits 0 having
    refreshed models.dev alone, naming the directory source rather than claiming a
    catalog refresh or failing as transient.
11. Agent disabling is gone end to end: `go doc` shows no `WithDisabled`, `#Config`
    rejects `disabled_agents`, `config.Config` has no `Disabled` field, and the
    detection walk has no id-skip branch.
12. The CLI's observable behaviour is unchanged apart from the three exceptions
    named in Constraints: JSON envelope shape and keys, the exit codes
    in the R15 table, warning wording, ordering defaults, `--fields` keys and
    defaults per Current State, and filter empty-state messages match the
    pre-project behaviour, as demonstrated by the retained CLI end-to-end tests.
13. Every API-describing document, including `README.md` and `AGENTS.md`, describes
    only the new surface, with no reference to the removed API.
14. The behaviour relocated from the CLI into the library has direct library-level
    tests; the root-package tests that exercised the removed API are rewritten
    against the new surface, supplying their catalogs through the module-materialising
    fixture helper, with no behavioural assertion dropped beyond the two classes R18
    retires by design; every new public
    operation, the R7 error set, the enrichment and coverage outcomes, warning
    injection, and the cold-offline `ErrCatalogUnavailable` path are tested
    directly; the CLI oracle gaps named in R18 were closed at step 0, in a commit
    that passes against the pre-project code; and the CLI end-to-end tests pass
    unchanged except for the five assertions R18 names as deliberately updated.
15. The finalisation sweep passes: `gofmt -l .` clean, `go build ./...` and
    `go vet ./...` pass, `golangci-lint run` clean, `go test ./...` passes, and
    from `catalog/` `cue vet ./...` passes with `cue mod tidy` clean.
