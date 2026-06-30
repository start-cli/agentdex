# agentdex Design

## Overview

agentdex is a Go module that detects AI coding agents installed on the local
machine and reports, for each known agent: where its binary lives, its installed
version, its configuration directory, its skills directory, the model provider(s)
it uses, and the models those providers offer (enriched from models.dev). It
ships as a library (the primary artefact) plus a thin CLI.

agentdex answers the "outside" questions about an agent: does it exist, where is
it, where is its config, where do skills go, what can it run. It does not read or
interpret an agent's internal configuration content. That separation is the
organising principle of the whole design.

The immediate motivation is the start CLI skills feature: to install a skill into
an agent, something must know which agents are installed and where their skills
directories are. agentdex is that something. start becomes its first consumer.

## Scope

In scope:

- Detect known agents only, driven by a published catalog. Never guess that an
  arbitrary executable is an agent.
- Report binary path, version, config directory, skills directory, provider(s),
  and provider API-key presence.
- Enrich an agent's provider(s) with the full models.dev model list (pricing,
  limits, modalities, capabilities, benchmarks).
- Resolve a fuzzy model string (for example `sonnet`) to a canonical models.dev
  model id.
- Provide a CLI for humans and agents to inspect the above.

Out of scope:

- Installing, uninstalling, or composing skills. That stays in start.
- Launching agents or composing prompts. That stays in start.
- Parsing or validating each agent's internal config schema.
- Native Windows support, and Windows-host agents reached through WSL PATH interop.
  Linux, macOS, and WSL (Linux-native installs) only.

## The outside / inside boundary

agentdex owns the outside of an agent: identity, location, paths, version,
capability. The inside of an agent (the user's selected model, settings, prompt
configuration) belongs to the agent and to start's orchestration. This boundary
decides every ownership question below and keeps agentdex free of per-agent
config-schema knowledge.

A direct consequence: "which models does this agent use" is answered at the
capability level (the agent's provider(s) and the models those providers serve),
not by reading the agent's internal config for a selected model.

## Ownership

| Data | Owner |
| --- | --- |
| Agent identity and discovery: id, bin, config paths, skills paths, version command, provider(s), homepage | agentdex catalog (authority) |
| Launch-time bin (static copy, verified against agentdex) | start/library #Agent |
| Authoritative model list and metadata per agent (via provider id) | models.dev, surfaced by agentdex at runtime |
| Fuzzy model string resolution | agentdex (library function) |
| Launch templates: command string, default_model, variants | start/library #Agent |
| Skill bundles (SKILL.md and resources) | start/library skills category |
| Skill install, uninstall, manifest, doctor | start |

Notes:

- The start/library #Agent `models` alias map is removed. Aliases are replaced by
  agentdex fuzzy resolution.
- start/library #Agent keeps `default_model`. Its stored form (canonical models.dev id
  versus agent-native string) and launch translation are settled by the start
  migration; see start migration.
- start/library #Agent keeps `bin` as static launch config. start needs the binary
  to launch an agent, must do so offline, and must not run detection on the launch
  hot path, so bin stays alongside the launch template. agentdex remains the
  authority: start verifies the static `bin` and `default_model` against the
  agentdex catalog at module install and via `start doctor`, not on every launch.
- Config paths and skills paths are removed from start/library and owned by the
  agentdex catalog. start resolves them by calling agentdex live in its install and
  doctor flows, where a detection dependency is acceptable.

## Dependency graph

The graph is a DAG. No cycles.

```
models.dev (static catalog.json)
   ^                         ^
   | HTTP GET                | HTTP GET
   |                         |
agentdex (Go) --- imports ---+
   ^      |
   |      | fetch (CUE registry)
   | Go   v
   |   agentdex catalog (CUE module, own repo, published to CUE Central Registry)
   |
start (Go) --- reads (CUE registry) --> start/library (launch templates, skill bundles, roles, contexts, tasks)
```

- agentdex depends on: its own catalog (CUE, via registry) and models.dev (HTTP).
  It does not depend on start or start/library.
- start depends on: agentdex (Go import) and start/library (CUE, via registry).
- start/library depends on neither agentdex nor the catalog.

The only link is a shared `id` string: a start/library #Agent variant such as
`claude/interactive` corresponds to the agentdex catalog id `claude-code`. start
carries that catalog id and passes it to agentdex; how start stores the link is
settled in the start migration. It is a data reference start owns, not an import, so
the graph stays acyclic.

## Repository layout

Root-package-as-library. The public API is the root package `agentdex`. A package
is a directory; the root package is split across multiple files in the root
directory. Distinct subsystems live in their own packages: private subsystems
under `internal/`, the reusable models.dev client as a public subpackage.

```
agentdex/
  agentdex.go        package agentdex: Detect, DetectOne, Options
  agent.go           package agentdex: Agent, KnownAgent, Catalog types
  engine.go          package agentdex: detection orchestration (unexported)
  probe.go           package agentdex: binary and config probing (unexported)
  version.go         package agentdex: version command execution (unexported)
  resolve.go         package agentdex: ResolveModel fuzzy matching
  errors.go          package agentdex: sentinel errors
  modelsdev/         public subpackage: models.dev client, cache, types
  internal/
    catalog/         fetch, cache, load, validate the CUE agent catalog
    cli/             cobra commands, envelope, exit codes
    config/          XDG resolution, config.cue loader
    tui/             colour constants, table rendering
  cmd/agentdex/
    main.go          minimal entry point
  catalog/           CUE module: #KnownAgent schema and agent data (published)
    schema.cue
    agents.cue
    cue.mod/module.cue
  testdata/
  .golangci.yml
  go.mod
  LICENSE
  README.md
```

Note: the repo hosts two module systems. The repo root is the Go module
`github.com/start-cli/agentdex`. The `catalog/` subdirectory is a CUE module
published to the CUE Central Registry as `github.com/start-cli/agentdex/catalog`.
They are independent and do not interfere.

## Public library API

The detection contract. Types are illustrative; field sets are the contract.

```go
package agentdex

// Agent is the result of detecting one known agent on this machine.
type Agent struct {
    ID          string            // catalog id, e.g. "claude-code"
    Name        string            // display name
    Bin         string            // binary name from the catalog
    Found       bool              // binary located on PATH or a search dir
    BinaryPath  string            // absolute path when Found
    Version     string            // resolved version, "" if unknown or skipped
    Config      ResolvedPaths     // resolved global/local config dirs, existence per scope
    Skills      ResolvedPaths     // resolved global/local skills dirs; zero value if the agent has no skills concept
    Providers   []string          // models.dev provider id(s)
    ProviderEnv map[string]bool   // provider API-key env var -> present in env
    Models      []modelsdev.Model // enriched; nil unless EnrichModels() was passed to WithModels
    Homepage    string
}

// KnownAgent is one catalog entry: the static facts about an agent.
type KnownAgent struct {
    ID       string
    Name     string
    Bin      string
    Config   PathPair
    Skills   *PathPair      // nil if the agent has no skills concept
    Version  *VersionProbe  // nil if version is not resolvable
    Provider []string
    Homepage string
}

type PathPair struct {
    Global string // e.g. "~/.claude"
    Local  string // e.g. ".claude"  (optional)
}

// ResolvedPaths is a catalog PathPair after tilde, environment, and
// working-directory expansion, with existence recorded per scope. It lets a
// consumer choose the scope (global versus local) rather than receiving one
// collapsed path: start's skill install picks global (~/.claude/skills) or
// local (.claude/skills) from this. Global and Local hold the resolved paths
// whether or not they exist on disk; GlobalExists and LocalExists report
// existence. Local is "" when the catalog defines no local scope. Skills uses
// the zero value when the agent has no skills concept.
type ResolvedPaths struct {
    Global       string
    GlobalExists bool
    Local        string // "" when the catalog defines no local scope
    LocalExists  bool
}

type VersionProbe struct {
    Args    []string // arguments appended to the detected binary, e.g. ["--version"]
    Pattern string   // optional regex to extract the version from combined stdout+stderr
}

// Catalog is the loaded set of known agents.
type Catalog struct {
    Agents map[string]KnownAgent // keyed by id
}
```

Entry points:

```go
// Detect runs every catalog entry through the detection engine and returns the
// agents found, sorted by id. Not-installed agents are omitted from the result.
func Detect(ctx context.Context, opts ...Option) ([]Agent, error)

// DetectOne detects a single agent by catalog id. For any id in the catalog it
// returns a fully populated *Agent (config and skills paths resolved for both scopes,
// Found and the per-scope existence flags reflecting reality) regardless of whether the binary is installed; the
// bool reports whether the binary was found, mirroring Agent.Found. An id absent from
// the catalog returns ErrAgentUnknown, the only "not a catalog agent" signal; unlike
// Detect, a known-but-not-installed agent is a normal result, not an omission.
func DetectOne(ctx context.Context, id string, opts ...Option) (*Agent, bool, error)

// LoadCatalog fetches and loads the agent catalog (registry plus cache). The
// bool is the loader's stale flag: true when re-resolution failed after the TTL
// expired and the last resolved version was reused (the catalog is still
// usable), so a caller can warn before passing it into Detect/DetectOne via
// WithCatalog.
func LoadCatalog(ctx context.Context, opts ...Option) (cat *Catalog, stale bool, err error)

// ResolveModel maps a fuzzy query (e.g. "sonnet") to a models.dev model for the
// given agent's provider(s). It returns the matched provider Model, the real
// models.dev provider id it resolved within, and the model's canonical
// (provider-agnostic) id when the agnostic map carries an entry for it, or ""
// when it does not. Model.ID keeps its source-id meaning; no id is ever constructed.
func (c *Catalog) ResolveModel(ctx context.Context, agentID, query string, mc *modelsdev.Client) (m modelsdev.Model, providerID string, canonicalID string, err error)
```

Options:

```go
func WithModels(c *modelsdev.Client, opts ...ModelsOption) Option // attach a models.dev client (enables ProviderEnv); pass EnrichModels() to also fill Agent.Models
func WithSkipVersion() Option               // do not exec any binary
func WithSearchDirs(dirs ...string) Option  // extra binary search locations
func WithBinPaths(m map[string]string) Option // per-agent binary path override, id -> path
func WithDisabled(ids ...string) Option     // skip these catalog ids
func WithCatalog(c *Catalog) Option         // use a preloaded catalog

// EnrichModels, passed to WithModels, additionally fills Agent.Models with the
// agent's per-provider model list. Without it, WithModels attaches the client for
// provider-env reporting only. The client is mandatory to WithModels, so model
// enrichment can never be requested without a client to serve it.
func EnrichModels() ModelsOption
```

Errors:

```go
var (
    ErrCatalogUnavailable = errors.New("agent catalog unavailable")
    ErrAgentUnknown       = errors.New("unknown agent id")
    ErrModelAmbiguous     = errors.New("model query matched multiple models")
    ErrModelNotFound      = errors.New("model query matched no models")
)
```

## Detection engine

Detection is data-driven. There are no per-agent Go files. One generic engine
walks the catalog and applies the same steps to every entry.

For each KnownAgent:

1. Presence. Resolution order: an explicit per-agent override
   (`--bin-path`/config `bin_paths`) wins; otherwise `exec.LookPath`, extended by
   any search dirs (config `search_dirs` and `--search-dir`). Record `BinaryPath`
   and set `Found`. An agent whose binary is not found is omitted from `Detect`
   results.
2. Config. Expand tilde and environment variables in `Config.Global` and
   `Config.Local` (the latter relative to the working directory) and probe each for
   existence. Record both resolved paths and their per-scope existence in
   `Agent.Config`.
3. Skills. If `Skills` is set, resolve both scopes the same way and record them in
   `Agent.Skills` with per-scope existence. No skills concept leaves `Agent.Skills`
   at its zero value. Scope selection (global versus local) is the consumer's
   choice: agentdex resolves both and reports existence, it does not pick one.
4. Version. If `Version` is set and `WithSkipVersion` is not in effect, exec the
   detected `BinaryPath` with `Version.Args` and a short context timeout, then apply
   `Version.Pattern` to the combined stdout and stderr, since some CLIs print their
   version to stderr. The bin name is never re-resolved through PATH, so the
   `--bin-path` override and search dirs apply here too. Failure is non-fatal: leave
   `Version` empty.
5. Providers. Copy `Provider` ids from the catalog. This is offline: no filesystem
   access and no network.
6. Enrichment (models.dev). When a models.dev client is attached (`WithModels`),
   fetch the providers map and record each provider's API-key env var presence in
   `ProviderEnv`; when `EnrichModels()` was also passed to `WithModels`, enrich
   `Models` from models.dev for the agent's provider(s). With no client attached, or
   when models.dev is unreachable with no cache, both are skipped and `Detect` still
   succeeds with `ProviderEnv` and `Models` nil. Provider-env needs only the small
   providers map, so it stays populated even when the per-model `Models` slice is
   suppressed.

The four core steps (presence, config, skills, version) use only the filesystem and
the catalog and always work offline. Version resolution is the only step that executes
a binary; `WithSkipVersion` makes detection fully exec-free. Enrichment is the only
step that reaches models.dev, and it degrades to nil rather than failing.

The engine processes catalog entries concurrently and honours the context.

## Agent catalog

The catalog is agentdex's own CUE module, authored in this repo and published to
the CUE Central Registry. It is the single source of truth for agent discovery
metadata. It is thin by design: it stores only what models.dev cannot supply, and
references models.dev for everything about models and providers.

### Schema

The map key is the agent id, constrained to `^[a-z0-9]+(-[a-z0-9]+)*$`. There is no
id field on #KnownAgent: the key is the single source, mirroring how models.dev
derives a model id from its path. The loader populates `KnownAgent.ID` and
`Agent.ID` from the key when building the Go structs.

```cue
#KnownAgent: {
    name:        string & !=""
    bin:         string & !=""
    description?: string
    config: {
        global: string & !=""
        local?: string & !=""
    }
    skills?: {
        global: string & !=""
        local?: string & !=""
    }
    version?: {
        args: [string, ...string]   // appended to the detected binary, e.g. ["--version"]
        pattern?: string
    }
    provider: [string, ...string]   // models.dev provider ids; the join key; at least one required
    homepage?: string
}

agents: [=~"^[a-z0-9]+(-[a-z0-9]+)*$"]: #KnownAgent
```

Example entry:

```cue
agents: "claude-code": {
    name: "Claude Code"
    bin:  "claude"
    config: {
        global: "~/.claude"
        local:  ".claude"
    }
    skills: {
        global: "~/.claude/skills"
        local:  ".claude/skills"
    }
    version: {
        args:    ["--version"]
        pattern: "([0-9]+\\.[0-9]+\\.[0-9]+)"
    }
    provider: ["anthropic"]
    homepage: "https://github.com/anthropics/claude-code"
}
```

### Home and publishing

- Module path: `github.com/start-cli/agentdex/catalog@v1`.
- Lives in the `catalog/` subdirectory of the agentdex repo.
- Published to the CUE Central Registry, same mechanism start uses for
  start/library.
- Versioned independently of the Go binary, so adding or updating agents does not
  require an agentdex release.

### Delivery

- Not embedded in the binary. The catalog is fetched from the registry at runtime
  and cached. Updating the catalog (publishing a new version) reaches all
  installs within the cache TTL without a binary rebuild.
- Fetch uses `cuelang.org/go/mod/modconfig`, which honours `CUE_REGISTRY` and
  `cue login` exactly as start does.
- Caching mirrors start's registry approach, not a JSON file with a TTL. agentdex
  caches the resolved catalog version under `$XDG_CACHE_HOME/agentdex/` for the TTL
  (24h default) and lets CUE's own module cache hold the module content. When the
  version-resolution TTL expires, agentdex re-resolves; if re-resolution fails it
  keeps using the last resolved version. This is version-resolution caching over
  CUE's content cache, distinct from the models.dev stale-JSON cache below.
- First run with no network and no resolved version fails clearly. This is accepted.
- The source module can be overridden via config (`catalog.module`) to point at a
  fork or a locally published module.

### Research deliverable

The complete set of known agents is compiled by research and authored into this
catalog. models.dev provider ids are the seed: each provider tends to have one or
more agent CLIs. The catalog must aim to be comprehensive (Claude Code, Google
Antigravity CLI / agy, Codex, aichat, Copilot, and others), each with verified
bin name, config paths, skills paths, version command, and provider mapping.

agy is the Google Antigravity CLI, the successor to the Gemini CLI, and one of
several Antigravity programs. Its skills directory is `~/.agents/skills` (global)
and `.agents/skills` (local), per the start skills design; its provider is google.

Paths and version commands are verified against each tool rather than assumed.
Where a value cannot be verified it is left unset rather than guessed.

## models.dev integration

models.dev is a community database of model specifications, pricing, and
capabilities. agentdex consumes it as the authoritative model layer and never
duplicates its data.

### Data source

- models.dev publishes static JSON. There is no dynamic API.
- agentdex performs a plain HTTPS GET of `https://models.dev/catalog.json`.
- `catalog.json` is `{ models, providers }`: the provider-agnostic model map plus
  the per-provider map. One request yields everything.
- The repo-root `models.json` in the models.dev GitHub repo is a vendored
  OpenRouter dump, not the published schema. It is not used.
- The published `catalog.json` is generated and validated from the zod schema in
  `packages/core/src/schema.ts`. Go types mirror that schema, not the OpenRouter
  shape.

### Why catalog.json (the merge)

The per-provider Model carries pricing, limits, and status. Benchmarks and weights
live only in the provider-agnostic model map (generate.ts omits them from provider
models). To present the fullest picture, agentdex fetches `catalog.json` and merges
the two:

- provider Model: cost, limit, status, modalities, capability flags
- provider-agnostic ModelMetadata: benchmarks, weights

The two maps are distinct keyspaces. The provider map is nested under (provider id,
model key); a provider model's short `id` field restates its model key. The
provider-agnostic map is keyed by the real path-style id. The merge is a lookup join
over real keys, driven from the agnostic side: it iterates the provider-agnostic map,
splits each real path-style id on its single slash into its provider and model parts,
and attaches that entry's benchmarks and weights to the matching provider model. Both
sides keep their source ids; nothing is written onto `Model.ID`, and no composite id is
ever constructed. Driving the merge from the agnostic map means every key touched is a
real models.dev id — the provider-side `id` is never assembled into a path, so the
merge cannot mint an identifier that does not exist upstream.

The join lands only where a provider model has a real agnostic entry. A first-party
model decomposes cleanly: the agnostic id `anthropic/claude-opus-4-5` splits to provider
`anthropic`, model `claude-opus-4-5`, which is exactly that provider's model key.
Aggregator and proxy providers re-expose other providers' models under path-bearing keys
(for example requesty's `xai/grok-4`); no agnostic id decomposes to them, so they receive
nothing — correctly, as they carry no benchmarks or weights of their own. agentdex
enriches only the first-party agent providers its catalog lists, so a low attachment rate
measured against the full models.dev catalog is expected, not a defect.

### Go types

```go
package modelsdev

// Catalog is the merged top-level result of fetching models.dev catalog.json:
// the provider-agnostic model map plus the per-provider map. It mirrors the
// upstream { models, providers } shape. Distinct from agentdex.Catalog (the set
// of known agents); package qualification keeps the two unambiguous.
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
    Env    []string          // API-key env var names, e.g. ["ANTHROPIC_API_KEY"]
    Models map[string]Model  // keyed by short model id within the provider
}

type Model struct {
    ID               string   // source id: path-style in the agnostic map, short within a provider map
    Name             string
    Family           string
    Attachment       bool
    Reasoning        bool
    ToolCall         bool
    StructuredOutput bool
    Temperature      bool
    Knowledge        string   // YYYY-MM or YYYY-MM-DD
    ReleaseDate      string
    LastUpdated      string
    Modalities       Modalities // input/output of text|audio|image|video|pdf
    OpenWeights      bool
    Limit            Limit      // context, input, output (token counts)
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

// Tier is one entry in a model's tiered pricing: a per-token cost subset plus
// the dimension and threshold at which it applies. Upstream nests the dimension
// under a "tier" object.
type Tier struct {
    Input, Output, CacheRead, CacheWrite float64
    Tier TierDimension
}

// TierDimension is the nested "tier" object on each tiered-pricing entry: the
// dimension and the threshold at which the tier takes effect.
type TierDimension struct {
    Type string // tier dimension, e.g. "context"
    Size int    // threshold at which this tier takes effect, e.g. 200000
}
```

### Client

```go
type Client struct{ /* http client, cache dir, ttl, url */ }

func New(opts ...ClientOption) *Client
func (c *Client) Catalog(ctx context.Context) (*Catalog, error)         // fetch+cache+merge
func (c *Client) Provider(ctx context.Context, id string) (Provider, bool, error)
func (c *Client) Models(ctx context.Context, providerIDs ...string) ([]Model, error)
```

- Cache at `$XDG_CACHE_HOME/agentdex/catalog-modelsdev.json`, 24h TTL, stale on
  failure.
- URL overridable via config (`models.url`) for mirrors or local copies.
- The package is public and reusable by start and others.

models.dev is an unversioned community JSON with no contract, and Go's decoder
silently ignores unknown fields and zero-fills renamed ones, so an upstream schema
change would otherwise degrade enrichment to blank cost, limits, and capabilities
with no signal. To make drift loud rather than invisible without coupling agentdex to
providers it never reads, validation is scoped in two tiers. After decode, `Catalog`
checks only the top-level shape — non-empty `models` and `providers` — which is the
gross-drift signal: a wholesale schema change that renames or empties the top-level
maps. A top-level violation fails the fetch with `ErrModelsSchema` ("models.dev schema
unrecognised") rather than returning a hollow catalog, and follows the existing
stale-on-failure behaviour: a previously cached copy is served, and only a first fetch
with no cache surfaces the error to the caller. The per-model required-field check
(`id` non-empty, `limit` present) is applied only to the providers a caller actually
requests, in `Provider` and `Models`, never across the full upstream catalog of
thousands of models from providers agentdex never enriches. So an unrelated provider's
malformed model — a limitless embedding model, a half-entered community entry — cannot
break enrichment for agents whose own providers are well-formed; only a requested
provider carrying a malformed model raises `ErrModelsSchema`, from the accessor that
requested it. This is a coarse guard against gross drift, not a full schema check; the
`models.url` override remains the way to pin a frozen mirror.

### Model resolution

`Catalog.ResolveModel(ctx, agentID, query, mc)` resolves a fuzzy query against the
agent's provider model set, in this order:

1. exact models.dev id
2. exact name, case-insensitive
3. unique substring or prefix match
4. ambiguous match returns ErrModelAmbiguous with the candidate ids; no match
   returns ErrModelNotFound

This is where `--model sonnet` resolves to a concrete model. ResolveModel returns the
matched provider Model, the real models.dev provider id it resolved within, and the
model's canonical id, as explicit values `(modelsdev.Model, providerID string,
canonicalID string, error)`. The canonical id is the model's real provider-agnostic id:
the library probes the agnostic map under `providerID + "/" + modelKey` and returns the
actual key it finds, or `""` when the agnostic map has no entry for that model. The
composite is used only as a lookup probe — never returned as-is and never written onto
`Model.ID`, so `Model.ID` keeps its documented source-id meaning (short within a provider
map) and the library never surfaces an identifier that does not exist in models.dev. The
provider id is returned so a caller holding only the Model — which carries no provider
field — still has authoritative provider context without parsing the opaque canonical id.
start calls this rather than maintaining an alias map. The agentdex CLI surfaces the same
resolution via `agentdex models <agent> <query>` (selector matching), so it is reachable
without importing the library. The CLI exposes the canonical id as a distinct
`canonical_id` output field, shown only when non-empty; the short `Model.ID` stays the
`id` field, so the CLI and library never disagree on what `id` means. Fuzzy matching is an
input convenience only; how start stores `default_model` is settled in the start migration.

### Provider env reporting

Each models.dev Provider lists its API-key env var names in `env`. agentdex reports
whether those vars are present in the environment (`Agent.ProviderEnv`), a signal
of whether the agent is actually usable. Sourced entirely from the models.dev
providers map; nothing added to the catalog. It is enrichment, not core detection:
it requires a models.dev client (attached via `WithModels`) and is left nil when none
is attached or models.dev is unreachable, never failing `Detect`. It needs only the
small providers map, so `get` attaches the client unconditionally and adds
`EnrichModels()` unless `--no-models`, keeping provider-env on even under
`--no-models`; `list` attaches no client by default, adding one only under `--models`,
to stay offline-fast.

## Catalog and models.dev coverage

The curated catalog and models.dev are independent sources. The catalog is hand
authored and intentionally covers only known agents, not every provider models.dev
lists. Cross-referencing a queried agent against both drives `get` reporting,
catalog validation, and start doctor.

The models.dev axis is evaluated per provider, because `provider` is a list and an
agent (aichat, for example) can serve several providers. Each of an agent's
providers is independently either present in a reachable models.dev or absent. The
agent-level outcome is a rollup of those per-provider verdicts:

| catalog | providers in models.dev | meaning | result |
| --- | --- | --- | --- |
| yes | all present | fully supported agent | report agent, enrich models, exit 0 |
| yes | some present | partially supported: at least one provider enriches, at least one is absent | report agent, enrich from the present providers, emit a warning naming the absent provider(s), exit 0; the absent providers are flagged per provider by catalog validation and start doctor |
| yes | none present | catalog agent whose every provider is absent from models.dev: a catalog data error | report agent, emit error, exit 78; also flagged by catalog validation and start doctor |
| no | query matches a models.dev provider | not a catalog agent, but the query names a provider models.dev knows | report that provider and its models.dev models, labelled as provider data not an agent, note that install details are unavailable because it is not catalogued, exit 3; flagged by catalog validation and start doctor as a coverage gap |
| no | query matches no models.dev provider | unknown | informational: no such agent, likely a typo, list valid catalog ids, exit 2 |

The split is deliberate: get is detection and enrichment, not validation, so a
working multi-provider agent stays at exit 0 even when one provider is missing
upstream, with a warning rather than a failure. The data problem is still surfaced
exactly once, per absent provider, through catalog validation and start doctor,
which is where an author or operator acts on it. The all-present and all-absent
rows are the special cases of this rule for single-provider agents.

Here a provider absent from models.dev means it is genuinely missing from a
reachable models.dev. A models.dev that cannot be reached (no network, no cache) is
a different situation outside this table: get degrades with a warning and exits 0,
while a model-centric command fails transient (exit 75). See CLI get behaviour.

The library returns `ErrAgentUnknown` whenever a query matches no catalog agent
(the two no rows). The CLI then matches the query against models.dev provider ids and
names (exact, then unique substring or prefix) to choose between the no/yes path
(report that provider and its models.dev models, exit 3) and the no/no path (exit 2).
Model ids are not matched here: an agent maps onto models.dev only through a provider,
so the provider axis is the only one an uncatalogued agent could resolve against, and
the data reported is that provider's, not the uncatalogued agent's models. An agent's
name rarely equals a provider id, so the no/yes path fires mainly when the query itself
names a provider; it is a discovery aid, not a claim that the query is a known agent.

## CLI

The CLI is a thin wrapper over the library. Single domain, so the noun is omitted.

### Commands

```
agentdex list                    detected agents, table by default
agentdex get <agent>             detail for one agent (aliases: view, show)
agentdex models <agent> [query]  models available to the agent; query fuzzy-matches
agentdex refresh [target]        force refresh caches: catalog | models | all
agentdex skills <agent> [name]   skills in the agent's skills dir (read-only)
agentdex version
agentdex completion
```

Selector matching:

Every positional selector — `<agent>`, the model `[query]`, the skill `[name]` —
resolves by the same rule against its relevant set (catalog agents, the agent's
models, the skills found in the dir): exact match first (id, then case-insensitive
name), then a unique substring or prefix match. The outcome drives behaviour:

- none matched: report that nothing matched and exit 3
- one matched: act on that single match
- two or more matched: list the candidates so the user can refine, and exit 3

This is one rule applied everywhere, so `agentdex models <agent> sonnet` resolving
to a single model is the CLI surface of the library `ResolveModel`. The agent
selector adds one distinction handled below: a query that matches a known agent
which is simply not installed is reported differently from a query that matches no
known agent (see Output and exit codes).

Behaviour:

- `list` does not enrich models by default (fast, offline once cached). `--models`
  opts in.
- `get <agent>` enriches models and reports provider env presence by default.
  `--no-models` opts out of the per-model enrichment; provider-env still shows, since
  it needs only the providers map. `--tree` prints the config directory tree (no
  parsing of contents). When detection succeeds but models.dev is unreachable with no
  cache, get degrades: it prints the detected agent, omits the Models and provider-env
  sections, emits a warning that model enrichment was unavailable, and exits 0.
  Enrichment is not the point of get, so its absence does not fail the command.
- `models <agent> [query]` lists the agent's provider models with pricing, limits,
  and capabilities. With a `query` it applies selector matching: a single match
  prints that model (use `--json` or `--fields canonical_id` to script the canonical
  id, shown only when the model has a real models.dev agnostic id); multiple matches
  list the candidates.
- `refresh` forces a cache refresh for the catalog, models.dev, or both.
- `skills <agent> [name]` is read-only discovery. With no name it lists SKILL.md
  entries in the agent's resolved skills directory. With a name it applies selector
  matching: a single match prints that SKILL.md body to stdout, multiple matches
  list the candidates. It never writes. Skill install, uninstall, and management
  stay in start. This command is a separate project (see Phasing and separate
  projects); its shape is provisional and may be omitted from the core build.

### Global flags

| Flag | Purpose |
| --- | --- |
| --json | Emit a JSON envelope on stdout (long form only; no -j) |
| --verbose | Add detail to stdout |
| --quiet | Suppress non-essential output |
| --color | auto, always, never |
| --debug | Diagnostic logging to stderr |
| --search-dir | Extra binary search locations; csv and repeatable (StringSlice) |
| --bin-path | Override a specific agent's binary path; format id=path, repeatable |

### Output and exit codes

- Text by default; JSON envelope under `--json` with the standard
  status/data/error/warnings shape.
- `--fields` selection on list, get, and models.
- Exit codes follow the start CLI taxonomy: 0 success, 1 failure, 2 usage,
  3 not found, 4 permission, 5 conflict, 75 transient, 78 config.
- `get <agent>` reporting follows the catalog and models.dev coverage table:
  fully supported is exit 0; a catalog agent with at least one provider present in
  models.dev stays exit 0 and warns about any absent provider; a catalog agent whose
  every provider is absent from a reachable models.dev is a catalog data error,
  exit 78; a query that is not a catalog agent but names a models.dev provider reports
  that provider's models.dev data, labelled as provider data, exit 3; a query that
  matches neither a catalog agent nor a models.dev provider is genuinely unknown,
  exit 2 (`ErrAgentUnknown`). A known catalog agent that is not installed is not
  found, exit 3.
- Transient exit 75 applies when a command has no usable result at all: the catalog
  cannot be loaded, or a model-centric command (`models`) cannot reach models.dev
  with no cache. A `get` whose detection already succeeded degrades with a warning
  and exits 0 instead (see CLI get behaviour). A malformed config.cue returns
  exit 78.

## Configuration

User config at `$XDG_CONFIG_HOME/agentdex/config.cue`, CUE, validated at load. All
fields optional; a clean config is empty.

```cue
cache_ttl?: string                 // global cache TTL override; built-in default 24h
catalog: {
    module?: string | *"github.com/start-cli/agentdex/catalog@v1"
    ttl?:    string                // overrides cache_ttl for the catalog
}
models: {
    url?: string                   // override models.dev catalog URL (mirror or local)
    ttl?: string                   // overrides cache_ttl for models.dev
}
search_dirs?: [...string]          // persistent extra binary search locations
bin_paths?: [string]: string       // persistent per-agent binary path override, id -> path
disabled_agents?: [...string]      // catalog ids to skip during detection
enrich_models?: bool | *true       // default for get per-model enrichment only
color?: "auto" | "always" | "never" | *"auto"
```

TTL resolution per cache: the section ttl, then cache_ttl, then the built-in 24h.

`enrich_models` sets the default for `get`'s per-model enrichment only. It exists so
a slow or frequently-offline machine can make `get` default to no enrichment without
typing `--no-models` each time. It does not affect `list`, which stays offline-fast
and opts in per call via `--models`, nor `models`, whose enrichment is inherent.
Precedence for `get`: an explicit `--models`/`--no-models` flag wins over
`enrich_models`, which wins over the built-in default. Provider-env reporting is
unaffected and still shows whenever a client is attached, since it needs only the
small providers map.

No registry-auth settings are needed; modconfig honours CUE_REGISTRY and cue login.

## Caching

Two independent caches under `$XDG_CACHE_HOME/agentdex/`, with different mechanisms:

- the agent catalog: a resolved CUE module version. agentdex caches the resolved
  version for the TTL and relies on CUE's module content cache for the data. On a
  failed re-resolution it keeps the last resolved version.
- the models.dev catalog.json: a plain JSON file. agentdex caches the file for the
  TTL and serves the stale copy on network failure.

Both default to a 24h TTL and refresh when stale and needed. `agentdex refresh`
forces a refresh.

## Platforms

Linux, macOS, and Windows via WSL. No native Windows. WSL support covers agents
installed natively in the WSL Linux environment only: detection resolves binaries on
the WSL PATH and configs under WSL home and XDG paths. Windows-host agents reached
through PATH interop (a `.exe` on the interop PATH, configs under `/mnt/c/Users/...`)
are out of scope; `exec.LookPath` does not append `.exe` and catalog paths do not
point at the Windows profile. XDG base directories are resolved from the published
environment variables, with the documented home fallbacks, rather than
platform-specific user-dir helpers.

## Dependencies

Resolved to latest at build time.

- github.com/spf13/cobra (CLI)
- github.com/fatih/color (colour, NO_COLOR aware)
- golang.org/x/term (terminal detection)
- cuelang.org/go (CUE load and validate)
- cuelabs.dev/go/oci/ociregistry (registry fetch)
- standard library for HTTP, JSON, exec, filesystem
- testing: net/http/httptest (stdlib); go.uber.org/goleak optional

## Testing

- Detection engine: table-driven tests against a fake HOME and XDG layout seeded
  from testdata, covering presence, config probing, skills resolution, search
  dirs, and version parsing (with a stub binary).
- Catalog loader: load and validate a fixture catalog; cache TTL and
  stale-on-failure paths.
- models.dev client: an httptest server serving a fixture catalog.json; the
  provider and provider-agnostic merge, cache TTL, and stale-on-failure paths.
- Model resolution: table-driven cases for exact id, exact name, unique substring,
  ambiguous, and no-match.
- CLI: envelope shape, exit codes, and JSON golden output.
- Isolation via t.TempDir and t.Setenv; goleak optional for goroutine leaks.

## Build and distribution

- Library: `go get github.com/start-cli/agentdex`, imported as package agentdex.
- CLI install: `go install github.com/start-cli/agentdex/cmd/agentdex@latest`.
- Homebrew: distributed through the org tap (start-cli/homebrew-tap), matching
  start; a tagged release tarball drives the formula.
- Version is injected at build time via ldflags into the cli package Version
  variable, the same pattern start uses.
- Linting: a .golangci.yml at the repo root, run before commits.

## Relationship to start

start becomes agentdex's first consumer.

- start imports agentdex (Go) to detect agents and resolve their skills paths,
  replacing start's internal/detection.
- start's skill install resolves target skills directories by calling agentdex
  rather than reading #Agent.skills.
- start keeps the skills install mechanism (manifest, materialisation, doctor) and
  the skills category in start/library.

Effect on the in-flight start skills design:

- Made redundant: the #Agent.skills attribute in start/library and start's
  internal/detection. Both move to agentdex.
- Unchanged: the SKILL.md format, the skills category, the manifest, install and
  uninstall, and doctor reconciliation.
- Changed: skills-path resolution is sourced from agentdex.

start/library #Agent changes:

- Remove config-path and skills-path data (owned by agentdex). start resolves these
  by calling agentdex live in its install and doctor flows.
- Remove the models alias map.
- Keep the launch command template, `bin`, and default_model. bin and default_model
  are static launch config so start can launch offline without detection on the hot
  path. The stored form of default_model and its launch translation are a start
  migration concern; see start migration.

Verification at install and doctor, not at launch:

- When start installs an agent module from the library (first use), it detects the
  agent with agentdex and verifies the module's static `bin` resolves to that
  detected agent and that `default_model` is consistent with the agent's provider(s),
  in the form settled by the start migration. Mismatches are reported at install time.
- `start doctor` re-runs the same verification on demand and surfaces the catalog
  and models.dev coverage states, evaluated per provider: a catalog agent whose
  every provider is absent from models.dev (a data error), any individual absent
  provider on an otherwise-supported agent (a per-provider data flag), and an
  installed agent recognised by models.dev but not yet in the catalog (a coverage
  gap).
- Launch reads `bin` straight from the installed #Agent with no agentdex call, so a
  launch is fast and works offline.

## start migration

This design will be split into projects to build agentdex. One of those projects
migrates start to consume agentdex. This section collects the start-side concerns the
migration must resolve, so the project writing has them in one place. These are start
and start/library changes, not agentdex core work: agentdex exposes the capabilities
described above and start chooses how to adopt them.

The integration shape is set out in Relationship to start. The concerns below are the
decisions and migrations that section implies but does not settle.

### default_model storage and launch translation

start/library #Agent currently stores agent-native model strings that are passed
verbatim to the launch command (`command: "{{.bin}} --model {{.model}} ..."`): claude
uses `sonnet`/`opus`, aichat uses `vertexai:gemini-2.5-flash`. agentdex provides
`ResolveModel`, which returns the matched model, its provider id, and its real
models.dev canonical (agnostic) id when one exists; it constructs no id and stores no
model for launch. The migration must decide what `default_model` holds and how launch
obtains the agent-native string the binary accepts:

- store the canonical id and translate to the native string at launch (the resolved
  provider-nested short id is the candidate translation source)
- store the native string and treat resolution as verification and selection only
- store both

Verifying `default_model` against the catalog uses the canonical id where the model
has an agnostic entry and otherwise falls back to unique resolution within the agent's
providers, so the chosen option must reconcile verification with launch usage. Existing
`default_model` values are non-canonical and must be migrated as part of this work.

### Resolving a library agent to its catalog id

start's install and doctor flows call `agentdex.DetectOne(ctx, id, ...)`, which needs
the agentdex catalog id. A library #Agent has no id field: it is identified by its map
key and variant path (for example `claude/interactive`), while the catalog id is
`claude-code`. These do not map by any obvious transform, and `bin` ("claude") is
launch config, not identity, and need not be unique across catalog entries. The
migration must give start a deterministic way to resolve a library agent to its
catalog id. The recommended approach is an explicit field on start/library #Agent (for
example `agentdex_id: "claude-code"`) that start passes to DetectOne, making the link a
stated, verifiable field rather than a convention to reverse-engineer.

## Phasing and separate projects

This document specifies the core agentdex module. Three pieces of work are scoped as
separate projects and are not part of the core build:

- Agent catalog authoring. Researching and verifying the full set of known agents
  (bin name, config paths, skills paths, version command, provider mapping) is a
  standalone project. The core build delivers the #KnownAgent schema and the fetch
  and cache mechanism, plus an initial published version of the catalog module
  carrying a minimal seed of agents to prove the pattern. The seed is published to
  the registry and fetched at runtime like any other version, not embedded in the
  binary. The comprehensive catalog is authored and published separately and
  updates independently of the binary.
- Skills discovery command. The `skills` CLI surface is its own project. Its shape
  is provisional and must be settled alongside the start skills feature so the two
  do not diverge. The core build may omit it or ship only a minimal read-only
  version.

- start migration. Migrating start to consume agentdex (replacing
  internal/detection, resolving skills paths via agentdex, and the start/library
  #Agent changes) is a separate downstream project in the start repo. The decisions
  and migrations it must resolve are collected under start migration.

This phasing follows the org convention of tracking discrete projects (as in the
start and library project files) rather than landing everything in one change.

## Open items and future extensions

- Capability metadata. The catalog may later gain a small, factual capabilities
  block (for example the model flag name, a non-interactive flag) that start could
  read to build launch commands. This is purely additive to #KnownAgent and is
  deferred. The launch command template and per-variant policy stay in start; only
  factual flags would ever move, never launch recipes.
- The `skills` CLI command is a separate project; see Phasing and separate
  projects.

## Decisions log

- Root-package-as-library. Public API at the repo root package; subsystems under
  internal/; models.dev client a public subpackage.
- Detection is data-driven from a published catalog. No per-agent Go code.
- Outside/inside boundary. agentdex owns discovery metadata; agents and start own
  internal config and launch.
- The catalog is agentdex's own CUE module in the agentdex repo, published to the
  CUE Central Registry, fetched at runtime, cached 24h, not embedded.
- models.dev is consumed as the static catalog.json, cached 24h, merged across the
  provider and provider-agnostic maps. Go types mirror the real zod schema.
- Fuzzy model resolution lives in agentdex. start drops its alias map; how start
  stores default_model is settled by the start migration. The agentdex CLI surfaces
  resolution through selector matching on `models <agent> <query>`; there is no
  separate resolve command.
- Model identity is models.dev's own provider-agnostic id, never a constructed string.
  The merge joins agnostic-first over real path-style ids; ResolveModel returns the real
  provider id plus the canonical agnostic id when one exists (empty otherwise), using the
  composite only as a lookup probe. No synthetic identifier is minted or surfaced.
- start/library #Agent keeps a static `bin` for offline launch with no detection on
  the hot path. start verifies it (and default_model) against agentdex at install
  and via start doctor. Config and skills paths leave start/library entirely.
- CLI selector matching is one rule (none/one/many) applied to `<agent>`, model
  `[query]`, and skill `[name]`.
- Catalog and models.dev are cross-referenced to drive get exit codes, evaluated per
  provider: all providers present 0, some present 0 with a warning naming the absent
  provider(s), every provider absent 78 (catalog data error), not a catalog agent but
  the query names a models.dev provider 3 (reports that provider's data, labelled as
  provider data), neither a catalog agent nor a models.dev provider 2. The uncatalogued
  match is by provider id and name only, never model id; an agent maps onto models.dev
  through its provider, so the provider axis is the only one an uncatalogued query can
  resolve against. get degrades to exit 0 with a warning when detection succeeds but
  models.dev is unreachable.
- The catalog id is the map key; #KnownAgent has no id field. The loader sets the
  Go ID from the key.
- --json is long form only; no -j.
- --search-dir is csv and repeatable; --bin-path overrides a specific agent's
  binary path (id=path) and wins over PATH and search dirs.
- Agent catalog authoring and the skills discovery command are separate projects,
  not part of the core build.
- Platforms: Linux, macOS, WSL (Linux-native installs only; no Windows-host interop).
  License: MPL-2.0. Dependencies: latest at build.
