# 03 Detection Core

## Goal

Deliver the agentdex root package: the public detection API (`Detect`, `DetectOne`, `LoadCatalog`, `ResolveModel`), the data-driven detection engine, and the result types. This is the library agentdex exists to provide — given the agent catalog and a models.dev client, it reports for each known agent where it lives, its version, its config and skills paths, its providers, provider-env presence, and optionally its enriched model list, and it resolves a fuzzy model string to a canonical id.

## Scope

In scope:

- The root package public API: `Detect`, `DetectOne`, `LoadCatalog`, the `Option` set, and `Catalog.ResolveModel`.
- The detection-result types `Agent` and `ResolvedPaths`, and the root-package re-export of the catalog data types.
- The generic detection engine: presence, config, skills, version, providers, and models.dev enrichment, run concurrently over the catalog.
- The model-resolution sentinel errors and the fuzzy matching rule, shared with the CLI selector.

Out of scope:

- The CLI, `config.cue`, XDG user-config resolution, output envelope, and exit codes. Document 04.
- The catalog CUE module and loader internals (document 01) and the models.dev client internals (document 02). This document consumes both.
- The catalog/models.dev coverage rollup that drives `get` exit codes. That is CLI behaviour (document 04); the engine here reports raw per-provider facts the CLI rolls up.

## Current State

Documents 01 and 02 are complete:

- `internal/catalog` loads the agent catalog from the registry (or a source-module override) and returns the catalog data types (`Catalog`, `KnownAgent`, `PathPair`, `VersionProbe`), placed so the root package can re-export them without an import cycle. `ErrCatalogUnavailable` is defined there.
- `modelsdev` is a public leaf package: `Client` with `Catalog`, `Provider`, and `Models`; the merge; two-tier validation; the stale-JSON cache. `ErrModelsSchema` is defined there.

This document adds the root package files (`agentdex.go`, `agent.go`, `engine.go`, `probe.go`, `version.go`, `resolve.go`, `errors.go`). The full design is `docs/agentdex-design.md`.

## References

- `docs/agentdex-design.md` — sections: Public library API, Detection engine, Model resolution, Provider env reporting.
- The result and option contracts below are the integration surface document 04 (CLI) and future external consumers build against.

## Requirements

1. Result types

   - Define `Agent` and `ResolvedPaths` to the contract below. `Agent.Models` is `[]modelsdev.Model`, nil unless model enrichment was requested. `Agent.Config` and `Agent.Skills` are `ResolvedPaths`; `Skills` is the zero value when the agent has no skills concept.
   - Re-export the catalog data types so the public surface is `agentdex.Catalog`, `agentdex.KnownAgent`, `agentdex.PathPair`, and `agentdex.VersionProbe`, without `internal/catalog` importing this package.

2. Entry points

   - `Detect(ctx, opts...) ([]Agent, error)` runs every catalog entry through the engine and returns the agents found, sorted by id. Not-installed agents are omitted.
   - `DetectOne(ctx, id, opts...) (*Agent, bool, error)` detects a single agent by catalog id. For any id in the catalog it returns a fully populated `*Agent` (config and skills resolved for both scopes, `Found` and per-scope existence flags reflecting reality) whether or not the binary is installed; the bool mirrors `Agent.Found`. An id absent from the catalog returns `ErrAgentUnknown` — the only "not a catalog agent" signal. Unlike `Detect`, a known-but-not-installed agent is a normal result here, not an omission.
   - `LoadCatalog(ctx, opts...) (*Catalog, error)` fetches and loads the agent catalog via document 01's loader (registry plus cache).

3. Options

   - `WithModels(c *modelsdev.Client, opts ...ModelsOption) Option` attaches a models.dev client, enabling provider-env reporting. `EnrichModels()` passed to it additionally fills `Agent.Models`. The client is mandatory to `WithModels`, so enrichment can never be requested without a client.
   - `WithSkipVersion() Option` runs detection fully exec-free.
   - `WithSearchDirs(dirs...) Option` adds binary search locations.
   - `WithBinPaths(map[string]string) Option` overrides a specific agent's binary path by id; this wins over PATH and search dirs.
   - `WithDisabled(ids...) Option` skips catalog ids.
   - `WithCatalog(c *Catalog) Option` uses a preloaded catalog instead of loading one.

4. Detection engine (data-driven, no per-agent Go code)

   For each `KnownAgent`, in order:

   1. Presence. A `--bin-path`/`WithBinPaths` override for the agent wins; otherwise `exec.LookPath`, extended by any search dirs. Record `BinaryPath` and `Found`. In `Detect`, an agent whose binary is not found is omitted; in `DetectOne` it is a normal not-found result.
   2. Config. Expand tilde and environment variables in `config.global` and `config.local` (local relative to the working directory) and probe each for existence. Record both resolved paths and per-scope existence in `Agent.Config`.
   3. Skills. If `skills` is set, resolve both scopes the same way into `Agent.Skills` with per-scope existence. No skills concept leaves `Agent.Skills` at its zero value. agentdex resolves both scopes and reports existence; it never picks a scope.
   4. Version. If `version` is set and `WithSkipVersion` is not in effect, exec the detected `BinaryPath` (never re-resolved through PATH, so the override and search dirs apply) with `version.args` under a short context timeout, then apply `version.pattern` to combined stdout and stderr. Failure is non-fatal: leave `Version` empty.
   5. Providers. Copy `provider` ids from the catalog. Offline; no filesystem or network.
   6. Enrichment. When a models.dev client is attached, fetch the providers map and record each provider's API-key env var presence in `ProviderEnv`; when `EnrichModels()` was also passed, fill `Models` for the agent's providers. With no client, or when models.dev is unreachable with no cache, both are skipped and detection still succeeds with `ProviderEnv` and `Models` nil. Provider-env needs only the small providers map, so it stays populated even when per-model `Models` is suppressed.

   The four core steps (presence, config, skills, version) use only the filesystem and catalog and always work offline. Version is the only step that execs a binary; `WithSkipVersion` removes it. Enrichment is the only step that reaches models.dev and degrades to nil rather than failing. The engine processes catalog entries concurrently and honours the context.

5. Model resolution

   - `(c *Catalog) ResolveModel(ctx, agentID, query, mc *modelsdev.Client) (m modelsdev.Model, providerID string, canonicalID string, err error)` resolves a fuzzy query against the agent's provider model set, in order: exact models.dev id; exact name case-insensitive; unique substring or prefix; ambiguous returns `ErrModelAmbiguous` with candidate ids; none returns `ErrModelNotFound`.
   - Return the matched provider `Model`, the real models.dev provider id it resolved within, and the model's canonical id. The canonical id is the model's real provider-agnostic id: probe the agnostic map under `providerID + "/" + modelKey` and return the actual key found, or `""` when the agnostic map has no entry. The composite is a lookup probe only — never returned as-is and never written onto `Model.ID`, which keeps its source-id meaning, so the library never surfaces an id that does not exist in models.dev. Return the provider id so a caller holding only the `Model` (which carries no provider field) has authoritative provider context without parsing the opaque canonical id.
   - The none/one/many matching rule here is the same rule the CLI applies to its selectors. Implement it as a single shared helper so the two cannot drift. Document 04 reuses it.

6. Errors

   - Define `ErrAgentUnknown`, `ErrModelAmbiguous`, and `ErrModelNotFound` in this package. `ErrCatalogUnavailable` (document 01) and `ErrModelsSchema` (document 02) already exist; reference them, do not redefine.

## Constraints

- Pure Go, `CGO_ENABLED=0`, Go 1.25. Standard library for exec, filesystem, and concurrency.
- The public API field sets and signatures above are the contract document 04 and future external consumers build against. Match them.
- Do not read user `config.cue` or resolve XDG user-config here; options carry everything the engine needs. Document 04 maps config and flags into these options.
- XDG and home resolution for catalog/cache paths uses published environment variables with documented home fallbacks, not platform-specific user-dir helpers. Platforms: Linux, macOS, WSL (Linux-native) only.
- The engine must remain data-driven. No per-agent branches or per-agent files.
- Follow the repo `AGENTS.md`.

## Type contract

Types are illustrative; the field sets are the contract.

```go
package agentdex

// Agent is the result of detecting one known agent on this machine.
type Agent struct {
    ID          string            // catalog id, e.g. "claude-code"
    Name        string
    Bin         string            // binary name from the catalog
    Found       bool              // binary located on PATH or a search dir
    BinaryPath  string            // absolute path when Found
    Version     string            // resolved version, "" if unknown or skipped
    Config      ResolvedPaths     // resolved global/local config dirs, existence per scope
    Skills      ResolvedPaths     // resolved global/local skills dirs; zero value if no skills concept
    Providers   []string          // models.dev provider id(s)
    ProviderEnv map[string]bool   // provider API-key env var -> present in env
    Models      []modelsdev.Model // enriched; nil unless EnrichModels() was passed to WithModels
    Homepage    string
}

// ResolvedPaths is a catalog PathPair after tilde, env, and working-directory
// expansion, with existence recorded per scope. Global and Local hold the
// resolved paths whether or not they exist; the *Exists flags report existence.
// Local is "" when the catalog defines no local scope.
type ResolvedPaths struct {
    Global       string
    GlobalExists bool
    Local        string
    LocalExists  bool
}
```

## API contract

```go
func Detect(ctx context.Context, opts ...Option) ([]Agent, error)
func DetectOne(ctx context.Context, id string, opts ...Option) (*Agent, bool, error)
func LoadCatalog(ctx context.Context, opts ...Option) (*Catalog, error)
func (c *Catalog) ResolveModel(ctx context.Context, agentID, query string, mc *modelsdev.Client) (m modelsdev.Model, providerID string, canonicalID string, err error)

func WithModels(c *modelsdev.Client, opts ...ModelsOption) Option
func WithSkipVersion() Option
func WithSearchDirs(dirs ...string) Option
func WithBinPaths(m map[string]string) Option
func WithDisabled(ids ...string) Option
func WithCatalog(c *Catalog) Option
func EnrichModels() ModelsOption
```

## Implementation Plan

1. Re-export the catalog data types and define `Agent`, `ResolvedPaths`, and the sentinel errors.
2. Implement the `Option` and `ModelsOption` sets and the internal config struct they populate.
3. Implement the engine steps as independent, individually testable units (presence, config/skills path resolution with tilde and env expansion and per-scope existence, version exec with pattern extraction over combined output, providers copy, enrichment). Keep filesystem, exec, env, and network at the boundary.
4. Wire the engine to run catalog entries concurrently honouring the context, and assemble `Detect` (found-only, sorted) and `DetectOne` (always-populated, `ErrAgentUnknown` only when absent from the catalog).
5. Implement `LoadCatalog` over document 01's loader and `WithCatalog` to bypass it.
6. Implement `ResolveModel` and the shared none/one/many matching helper; return the provider id and the canonical id (the real agnostic id, or empty) as separate values, using the composite only to probe the agnostic map.
7. Tests: table-driven against a fake HOME and XDG layout seeded from `testdata`, covering presence, config probing, skills resolution, search dirs, the `--bin-path` override applying to version exec, and version parsing with a stub binary. Cover `WithSkipVersion` (no exec), enrichment degrading to nil when models.dev is unreachable, provider-env populated with `Models` suppressed, and `ResolveModel` cases (exact id, exact name, unique substring, ambiguous, no match). Isolate with `t.TempDir` and `t.Setenv`.

## Implementation Guidance

- The canonical id is read, not minted: it is the model's real provider-agnostic id, obtained by probing the agnostic map under `providerID + "/" + modelKey` and returning the actual key found, or empty when none exists. This mirrors the merge, which joins agnostic-first over real ids; the composite is only ever a lookup probe, never a returned or stored value. Returning the provider id alongside gives callers provider context without parsing the opaque canonical id.
- `Detect` omits not-installed agents; `DetectOne` returns them populated. This asymmetry is intentional: list-style callers want only what is present, while a targeted query wants the full picture including paths for an agent that is not yet installed. Do not unify them.
- The engine reports raw per-provider facts (which providers exist, which env vars are present). It does not compute the supported/partial/absent coverage verdict — that rollup and its exit codes are the CLI's job in document 04. Keep that policy out of the engine.
- Version resolution failure is always non-fatal. A binary that is present but whose version cannot be parsed is a detected agent with an empty `Version`, not a detection failure.

## Acceptance Criteria

- `go build ./...`, `go vet ./...`, and `golangci-lint run` are clean; the root package compiles with no import cycle against `internal/catalog`.
- `Detect` returns only agents whose binary was found, sorted by id, with config and skills paths resolved and per-scope existence set.
- `DetectOne` on a catalogued but not-installed agent returns a populated `*Agent` with `Found` false and accurate path existence; on an id absent from the catalog returns `ErrAgentUnknown`.
- `WithBinPaths` overrides presence and is the binary used for the version exec; `WithSearchDirs` extends presence; `WithSkipVersion` performs no exec and leaves `Version` empty.
- With a models.dev client attached, `ProviderEnv` reflects real env var presence; with `EnrichModels()` also passed, `Agent.Models` is filled; with the client unreachable and no cache, detection still succeeds with both nil.
- `ResolveModel` returns the matched `Model`, the real provider id it resolved within, and the canonical agnostic id (or `""` when the model has no agnostic entry); it leaves `Model.ID` as its source id, constructs no composite, and returns `ErrModelAmbiguous`/`ErrModelNotFound` for the ambiguous/no-match cases.
- The none/one/many matching is a single shared helper reused by the CLI in document 04.
