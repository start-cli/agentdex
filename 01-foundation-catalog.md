# 01 Foundation and Agent Catalog

## Goal

Establish the agentdex repository substrate and deliver the agent catalog: a published-shape CUE module defining the `#KnownAgent` schema and a seed set of agents, plus the Go loader that fetches, caches, validates, and loads that catalog from the CUE Central Registry. This is the data layer the detection engine consumes; nothing in agentdex works without it.

## Scope

In scope:

- Repository scaffold: Go module, repo-level agent-instruction file, lint config, README, directory layout.
- The agent catalog CUE module under `catalog/`: `#KnownAgent` schema and a seed `agents` map.
- The Go catalog loader: registry fetch via `modconfig`, version-resolution caching, schema validation, and load into Go structs.
- The catalog data types, defined in the root package and produced by mapping the loader's internal representation, so there is no import cycle between the root package and `internal/catalog`.

Out of scope:

- The detection engine, presence/config/skills/version probing, and the root-package public API (`Detect`, `DetectOne`, `ResolveModel`). Document 03.
- The models.dev client and any model enrichment. Document 02.
- The CLI, `config.cue` loading, and XDG resolution for user config. Document 04.
- Publishing the catalog module to the registry. Document 05 (gated release).
- A comprehensive catalog of every known agent. The seed here is minimal and proves the fetch path; the full catalog is a separate future project.

## Current State

The repository at `github.com/start-cli/agentdex` contains only `LICENSE` (MPL-2.0). There is no Go module, no CUE module, no agent-instruction file, and no source. This document creates the substrate every later document builds on, so it runs first.

The full design this project implements is `docs/agentdex-design.md`. Read it for rationale; this document carries the slice needed to build the foundation and catalog.

## References

- `docs/agentdex-design.md` — the complete agentdex design. Sections most relevant here: Repository layout, Agent catalog, Caching, Configuration (the `catalog` block).
- CUE Central Registry: https://registry.cuelang.org — the registry the catalog is fetched from at runtime.
- `cuelang.org/go/mod/modconfig` — the Go entry point for registry-aware CUE module loading. Construct it with `modconfig.NewRegistry(nil)`, which configures the registry from the environment and honours `CUE_REGISTRY` and `cue login`. This is the standard way to load a CUE module from the registry in Go.

## Requirements

1. Repository scaffold

   - Initialise the Go module `github.com/start-cli/agentdex` targeting Go 1.25.
   - Add a root `.golangci.yml` consistent with the organisation's Go linting conventions.
   - Add a `README.md` describing agentdex in one or two paragraphs (a detection library plus thin CLI) and noting the dual module layout (Go module at the root, CUE module under `catalog/`).
   - Add a repo-level agent-instruction file (`AGENTS.md`) capturing the cross-cutting standards every later document depends on (see Implementation Guidance for required contents). Later documents reference this file rather than restating these standards.

2. Agent catalog CUE module

   - Create the CUE module under `catalog/` with module path `github.com/start-cli/agentdex/catalog@v1` and a `cue.mod/module.cue` pinning the CUE language version to `v0.16.0`.
   - Define `#KnownAgent` and the `agents` map exactly to the schema contract below. The agent id is the map key constrained to `^[a-z0-9]+(-[a-z0-9]+)*$`; there is no id field on `#KnownAgent`.
   - Author a minimal seed `agents` map containing `claude-code` (the worked example below); one verified entry proves the published fetch path. Verify every seed value (bin, config paths, skills paths, version command, provider) against the real tool, and leave any optional value that cannot be verified unset rather than guessing. Exercising the loader across multiple entries and providers belongs to the testdata fixture (Implementation Plan step 5), which carries synthetic multi-provider entries and needs no real-tool verification.
   - `cue vet` must pass for the module, and `cue mod tidy` must leave it clean.

3. Catalog loader (`internal/catalog`)

   - Fetch the catalog module from the CUE Central Registry using `cuelang.org/go/mod/modconfig`, honouring `CUE_REGISTRY` and `cue login` with no agentdex-specific auth settings.
   - Reach the registry through an injectable interface — a small registry-client boundary (see Implementation Guidance for the shape) — with at minimum a resolve-latest-version operation and a fetch operation that returns the module's on-disk source directory. Production wires the real `modconfig`-backed implementation; the loader takes the interface as a dependency rather than constructing the registry inline. This boundary is the test seam: a stub returning a fixture source directory and canned resolve/fetch outcomes (successes and failures) drives the loader through its real load, validate, and cache logic with no registry. Keep the network behind this interface, alongside the clock and filesystem inputs already called out in Implementation Guidance.
   - Validate the fetched catalog by evaluating the module: its bundled `schema.cue` constrains `agents` to `#KnownAgent`, so unifying that schema with the fetched `agents.cue` surfaces any contract violation as a load error before decode. The loader carries no schema of its own — the schema travels with the data in the registry module and updates with it, consistent with the catalog being downloaded and cached on first use rather than embedded in the binary. Decode the validated `agents` map into the loader's own internal representation, populating each agent's ID from its map key. The root package maps that representation into the public catalog types (see Catalog data types); `internal/catalog` never imports the root package.
   - Implement version-resolution caching under `$XDG_CACHE_HOME/agentdex/`, keyed by module path: hold one entry per module (recording the resolved version and the time it was resolved), so resolutions for different module paths are independent and the resolution cached for one module is never served for another. The lookup is by the requested module path; there is no shared slot to collide in. Cache each resolved version for a TTL (24h default), and rely on CUE's own module content cache for the module data. This is version-resolution caching layered over CUE's content cache, not a JSON snapshot of the catalog.
   - On a failed re-resolution after the TTL expires, keep using the last resolved version rather than failing, and report the stale-resolution condition to the caller so the CLI can warn (document 04) while still working.
   - A first run with no network and no previously resolved version fails clearly with `ErrCatalogUnavailable`. This is accepted behaviour, not a defect.
   - Accept the source module path, cache directory, and TTL as inputs (options or parameters) with built-in defaults. Do not read user `config.cue` here; document 04 maps config to these inputs. The default module path is `github.com/start-cli/agentdex/catalog@v1`.
   - Support an override of the source module path so production and forks can select an alternative published module to load. This override selects which registry coordinate is loaded; it is not the test seam (the injectable registry interface above is). Fixture loading and failure injection go through the interface, not through this string. Because the version-resolution cache is keyed by module path, switching the override is simply a different cache key and needs no special invalidation.

4. Catalog data types

   - Define `Catalog`, `KnownAgent`, `PathPair`, and `VersionProbe` in the root package `agentdex`, per the contract below. `ErrCatalogUnavailable` is defined in the root package as well (document 03 defines the remaining sentinel errors). The public types live in the public package because they are the published API surface, and because `Catalog` carries methods (document 03's `ResolveModel`) that Go requires to be defined in the package that owns the type — a type defined in `internal/catalog` and aliased out could not.
   - Break the import cycle by direction, not by re-export: `internal/catalog` performs fetch, validate, and cache and returns its own internal representation; the root package maps that representation into the public `Catalog` (the surface document 03 finishes as `LoadCatalog`). `internal/catalog` never imports the root package, and no public type is defined in `internal/` and aliased out. The detection-result types (`Agent`, `ResolvedPaths`) are not defined here; they belong to document 03.

## Constraints

- Pure Go. No cgo, no C dependencies. The binary must build with `CGO_ENABLED=0`.
- Go 1.25. CUE module language pin `v0.16.0`; do not use CUE features beyond that pin.
- The catalog CUE module schema must match the contract below field-for-field. Later documents and the published catalog depend on it.
- Do not publish the catalog module to the registry. Acceptance is verified against a fixture or locally-overridden module only.
- The version-resolution cache relies on `modconfig` serving a previously-fetched canonical module version from CUE's content cache with no network: resolve-latest (`ModuleVersions`) requires the network, but `Fetch` of a canonical `module@version` is served from the content cache offline. This is the basis for both keep-last-resolved-after-TTL and the accepted first-run-offline failure. Record this behaviour in `AGENTS.md` so later documents inherit it.
- Follow the repo `AGENTS.md` this document creates for Go style, platform, and markdown standards.

## Schema contract

The catalog CUE schema. The map key is the agent id; the loader sets `KnownAgent.ID` from it.

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
        pattern?: string            // optional regex to extract the version
    }
    provider: [string, ...string]   // models.dev provider ids; the join key; at least one required
    homepage?: string
}

agents: [=~"^[a-z0-9]+(-[a-z0-9]+)*$"]: #KnownAgent
```

Worked seed entry:

```cue
agents: "claude-code": {
    name:        "Claude Code"
    bin:         "claude"
    description: "Anthropic's agentic coding tool that runs in the terminal."
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

## Type contract

The Go catalog data types this document delivers, defined in the root package `agentdex`. Types are illustrative; the field sets are the contract. The loader in `internal/catalog` decodes into its own internal representation and the root package maps that into these public types, so `internal/catalog` never imports the root package and no public type is aliased out of `internal/`.

`KnownAgent.Description` carries the catalog `description` so it is loaded end-to-end rather than dropped: document 03 must add a matching `Description` to the detection-result `Agent` and copy it from `KnownAgent`, and document 04's `get` surfaces it. This field is intentionally broader than the design's illustrative Go `KnownAgent`, which omits it; the schema-level `description?` is the source and this contract surfaces it.

```go
// Catalog is the loaded set of known agents.
type Catalog struct {
    Agents map[string]KnownAgent // keyed by id
}

// KnownAgent is one catalog entry: the static facts about an agent.
type KnownAgent struct {
    ID          string        // populated from the map key by the loader
    Name        string
    Bin         string
    Description string        // "" when the catalog entry omits it
    Config      PathPair
    Skills      *PathPair     // nil if the agent has no skills concept
    Version     *VersionProbe // nil if version is not resolvable
    Provider    []string
    Homepage    string
}

type PathPair struct {
    Global string // e.g. "~/.claude"
    Local  string // e.g. ".claude" (optional; "" when the catalog defines no local scope)
}

type VersionProbe struct {
    Args    []string // arguments appended to the detected binary, e.g. ["--version"]
    Pattern string   // optional regex to extract the version from combined stdout+stderr
}
```

## Implementation Plan

1. Create the Go module, `.golangci.yml`, `README.md`, and `AGENTS.md`. Establish the directory layout (`catalog/`, `internal/catalog/`, `testdata/`) plus the initial root-package files that hold the catalog data types and `ErrCatalogUnavailable`; later documents add `modelsdev/`, the remaining root-package files, the rest of `internal/`, and `cmd/agentdex/`.
2. Author the catalog CUE module: `catalog/cue.mod/module.cue`, `catalog/schema.cue` (the `#KnownAgent` schema), and `catalog/agents.cue` (the verified seed). Run `cue vet` and `cue mod tidy`.
3. Define the catalog data types (`Catalog`, `KnownAgent`, `PathPair`, `VersionProbe`) and `ErrCatalogUnavailable` in the root package `agentdex`, and have `internal/catalog` return its own internal representation that the root package maps into them, so the root↔`internal/catalog` dependency stays one-way with no cycle.
4. Implement the loader behind the injectable registry interface: `modconfig`-based fetch in the production implementation, schema validation, load into the types with id-from-key, and the version-resolution cache with keep-last-resolved-on-failure and clear first-run-offline failure.
5. Add a testdata fixture catalog module — a complete CUE module (`cue.mod/module.cue`, its own `schema.cue`, and a synthetic `agents.cue`) carrying multiple synthetic agents across more than one provider so map iteration and id-from-key are exercised across entries — and a stub registry that serves its source directory and canned resolve/fetch outcomes. Because validation is by evaluating the fetched module, the fixture carries its own `schema.cue` and so exercises the identical load-evaluate-validate-decode path used in production. Drive the loader tests through the stub so the loader is verifiable without a registry publish. Cover load-and-validate (including a fixture whose data violates the schema, which must fail the load), id-from-key across multiple entries, cache TTL behaviour, and stale/keep-last-resolved on re-resolution failure.
6. Exercise the production `modconfig`-backed `Registry` against a local in-process registry, not just the stub. Stand up an on-disk or in-memory OCI registry (CUE's `ociregistry` backends, selected via `CUE_REGISTRY`), publish the fixture catalog module to it, and drive the real implementation through resolve, fetch, and the on-disk `sourceDir` contract offline with no public registry. This is the only coverage of the code the stub replaces; without it the production wiring is unverified until the gated release in document 05.

## Implementation Guidance

- The `AGENTS.md` this project creates should state, at minimum: the dual module layout (Go module at root, independent CUE module under `catalog/`, published as `github.com/start-cli/agentdex/catalog@v1`); pure-Go / no-cgo / `CGO_ENABLED=0`; Go 1.25 and CUE `v0.16.0`; target platforms Linux, macOS, and WSL (Linux-native installs) with no native Windows; dependencies resolved to latest at build; the finalisation sweep (`gofmt`, `go vet`/`go fix`, `golangci-lint run`, tests); and the agent-facing markdown conventions (no bold, italic, emojis, horizontal rules, or headings deeper than `###`). Later documents rely on these living here rather than in each project doc.
- Keep the loader's nondeterministic inputs (clock, filesystem, network, environment) at the boundary so the cache and load logic are testable from inputs. The catalog-coverage rollup against models.dev (the per-provider supported/partial/absent verdicts) is consumed by the CLI and is not built here; this loader only needs to fetch, validate, and load.
- Use one established registry-loading shape rather than inventing a new one: a `modconfig` client (`modconfig.NewRegistry(nil)`) honouring `CUE_REGISTRY` and `cue login`, consumed through a small registry-client interface that production backs with the real client and tests substitute with a stub. A minimal shape:

  ```go
  type Registry interface {
      // ResolveLatestVersion maps a major-version module path (…/catalog@v1)
      // to a canonical version (…/catalog@v1.0.3). Requires the network.
      ResolveLatestVersion(ctx context.Context, modulePath string) (string, error)
      // Fetch returns the on-disk source directory for a canonical
      // module@version. Served from CUE's content cache offline.
      Fetch(ctx context.Context, modulePath string) (sourceDir string, err error)
  }
  ```

  This interface seam is what makes the loader testable with an on-disk fixture and injected failures rather than a live registry.

## Acceptance Criteria

- `go build ./...` and `go vet ./...` pass; `golangci-lint run` is clean.
- `cue vet` passes for the `catalog/` module and `cue mod tidy` leaves it clean.
- The seed `agents` map contains `claude-code`, every value verified against the real tool, and validates against `#KnownAgent`.
- The loader loads a fixture catalog through a stub registry, and the root-package mapping yields a public `Catalog` whose `KnownAgent.ID` values equal their map keys across the fixture's multiple entries and providers, with `Description`, `Config`, `Skills`, `Version`, and `Provider` populated as authored.
- A fixture whose `agents` data violates the schema (served through the stub) fails the load with a clear error rather than producing a partial `Catalog`, confirming validation happens by evaluating the fetched module.
- Loader tests, driven through the stub registry, demonstrate: a fresh resolve populates the version cache; a within-TTL load uses the cached version without re-resolving; a failed re-resolution after TTL keeps the last resolved version; a first run with no network and no resolved version returns `ErrCatalogUnavailable`. The same load, validate, and cache code runs under the stub as in production.
- Two different source module paths produce independent cached resolutions: resolving one populates only its own entry, and the resolution cached for one module is never served when loading the other.
- The public catalog types and `ErrCatalogUnavailable` are defined in the root package, with `internal/catalog` returning an internal representation the root maps into them; `internal/catalog` does not import the root package and no public type is aliased out of `internal/` (verifiable by document 03 with no cycle).
- The production `modconfig`-backed `Registry` is exercised end-to-end against a local in-process OCI registry (the fixture module published to it, selected via `CUE_REGISTRY`): resolve, fetch, and load succeed offline through the real implementation, so the code the stub replaces is covered before the document 05 release.
