# 01 Foundation and Agent Catalog

## Goal

Establish the agentdex repository substrate and deliver the agent catalog: a published-shape CUE module defining the `#KnownAgent` schema and a seed set of agents, plus the Go loader that fetches, caches, validates, and loads that catalog from the CUE Central Registry. This is the data layer the detection engine consumes; nothing in agentdex works without it.

## Scope

In scope:

- Repository scaffold: Go module, repo-level agent-instruction file, lint config, README, directory layout.
- The agent catalog CUE module under `catalog/`: `#KnownAgent` schema and a seed `agents` map.
- The Go catalog loader: registry fetch via `modconfig`, version-resolution caching, schema validation, and load into Go structs.
- The catalog data types the loader produces, placed so the root package (document 03) can surface them without an import cycle.

Out of scope:

- The detection engine, presence/config/skills/version probing, and the root-package public API (`Detect`, `DetectOne`, `ResolveModel`). Document 03.
- The models.dev client and any model enrichment. Document 02.
- The CLI, `config.cue` loading, and XDG resolution for user config. Document 04.
- Publishing the catalog module to the registry. Document 05 (gated release).
- A comprehensive catalog of every known agent. The seed here is minimal and proves the fetch path; the full catalog is a separate future project.

## Current State

The repository at `github.com/start-cli/agentdex` contains only `LICENSE` (MPL-2.0). There is no Go module, no CUE module, no agent-instruction file, and no source. This document creates the substrate every later document builds on, so it runs first.

The full design this project implements is `../docs/agentdex-design.md` in the sibling `start-cli/org` working tree. Read it for rationale; this document carries the slice needed to build the foundation and catalog.

## References

- `../docs/agentdex-design.md` — the complete agentdex design. Sections most relevant here: Repository layout, Agent catalog, Caching, Configuration (the `catalog` block).
- CUE Central Registry: https://registry.cuelang.org — the registry the catalog is fetched from at runtime.
- `cuelang.org/go/mod/modconfig` — the Go entry point for registry-aware CUE module loading; honours `CUE_REGISTRY` and `cue login`. This is how `start` loads `start/library`; mirror that approach.
- The sibling `start/` repo's registry loading code (`internal/registry`, `internal/cue`) is a working reference for `modconfig`-based fetch and CUE evaluation in this organisation.

## Requirements

1. Repository scaffold

   - Initialise the Go module `github.com/start-cli/agentdex` targeting Go 1.25.
   - Add a root `.golangci.yml` consistent with the organisation's Go linting (use the `start/` config as the reference baseline).
   - Add a `README.md` describing agentdex in one or two paragraphs (a detection library plus thin CLI) and noting the dual module layout (Go module at the root, CUE module under `catalog/`).
   - Add a repo-level agent-instruction file (`AGENTS.md`) capturing the cross-cutting standards every later document depends on (see Implementation Guidance for required contents). Later documents reference this file rather than restating these standards.

2. Agent catalog CUE module

   - Create the CUE module under `catalog/` with module path `github.com/start-cli/agentdex/catalog@v1` and a `cue.mod/module.cue` pinning the CUE language version to `v0.16.0`.
   - Define `#KnownAgent` and the `agents` map exactly to the schema contract below. The agent id is the map key constrained to `^[a-z0-9]+(-[a-z0-9]+)*$`; there is no id field on `#KnownAgent`.
   - Author a minimal seed `agents` map: `claude-code` (the worked example below) plus at least one agent with a different provider, so the loader is exercised against more than one entry and more than one provider. Verify every seed value (bin, config paths, skills paths, version command, provider) against the real tool; leave any value that cannot be verified unset rather than guessing.
   - `cue vet` must pass for the module, and `cue mod tidy` must leave it clean.

3. Catalog loader (`internal/catalog`)

   - Fetch the catalog module from the CUE Central Registry using `cuelang.org/go/mod/modconfig`, honouring `CUE_REGISTRY` and `cue login` with no agentdex-specific auth settings.
   - Validate the fetched CUE against `#KnownAgent` and load it into the Go catalog types, populating each `KnownAgent.ID` from its map key.
   - Implement version-resolution caching under `$XDG_CACHE_HOME/agentdex/`: cache the resolved module version for a TTL (24h default), and rely on CUE's own module content cache for the module data. This is version-resolution caching layered over CUE's content cache, not a JSON snapshot of the catalog.
   - On a failed re-resolution after the TTL expires, keep using the last resolved version rather than failing.
   - A first run with no network and no previously resolved version fails clearly with `ErrCatalogUnavailable`. This is accepted behaviour, not a defect.
   - Accept the source module path, cache directory, and TTL as inputs (options or parameters) with built-in defaults. Do not read user `config.cue` here; document 04 maps config to these inputs. The default module path is `github.com/start-cli/agentdex/catalog@v1`.
   - Support an override of the source module path so tests and forks can point at a local or alternative published module.

4. Catalog data types

   - Define `Catalog`, `KnownAgent`, `PathPair`, and `VersionProbe` per the contract below. `ErrCatalogUnavailable` is defined here as well (document 03 defines the remaining sentinel errors).
   - Place these types so the root package can expose them as `agentdex.Catalog`, `agentdex.KnownAgent`, etc., without `internal/catalog` importing the root package. The detection-result types (`Agent`, `ResolvedPaths`) are not defined here; they belong to document 03.

## Constraints

- Pure Go. No cgo, no C dependencies. The binary must build with `CGO_ENABLED=0`.
- Go 1.25. CUE module language pin `v0.16.0`; do not use CUE features beyond that pin.
- The catalog CUE module schema must match the contract below field-for-field. Later documents and the published catalog depend on it.
- Do not publish the catalog module to the registry. Acceptance is verified against a fixture or locally-overridden module only.
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

## Type contract

The Go catalog data types this document delivers. Types are illustrative; the field sets are the contract.

```go
// Catalog is the loaded set of known agents.
type Catalog struct {
    Agents map[string]KnownAgent // keyed by id
}

// KnownAgent is one catalog entry: the static facts about an agent.
type KnownAgent struct {
    ID       string        // populated from the map key by the loader
    Name     string
    Bin      string
    Config   PathPair
    Skills   *PathPair     // nil if the agent has no skills concept
    Version  *VersionProbe // nil if version is not resolvable
    Provider []string
    Homepage string
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

1. Create the Go module, `.golangci.yml`, `README.md`, and `AGENTS.md`. Establish the directory layout (`catalog/`, `internal/catalog/`, `testdata/`); later documents add `modelsdev/`, root-package files, `internal/`, and `cmd/agentdex/`.
2. Author the catalog CUE module: `catalog/cue.mod/module.cue`, `catalog/schema.cue` (the `#KnownAgent` schema), and `catalog/agents.cue` (the verified seed). Run `cue vet` and `cue mod tidy`.
3. Define the catalog data types (`Catalog`, `KnownAgent`, `PathPair`, `VersionProbe`) and `ErrCatalogUnavailable`, placed to avoid a future root-package import cycle.
4. Implement the loader: `modconfig`-based fetch, schema validation, load into the types with id-from-key, and the version-resolution cache with keep-last-resolved-on-failure and clear first-run-offline failure.
5. Add a testdata fixture catalog module and a loader test path that points at it via the source-module override, so the loader is verifiable without a registry publish. Cover load-and-validate, id-from-key, cache TTL behaviour, and stale/keep-last-resolved on re-resolution failure.

## Implementation Guidance

- The `AGENTS.md` this project creates should state, at minimum: the dual module layout (Go module at root, independent CUE module under `catalog/`, published as `github.com/start-cli/agentdex/catalog@v1`); pure-Go / no-cgo / `CGO_ENABLED=0`; Go 1.25 and CUE `v0.16.0`; target platforms Linux, macOS, and WSL (Linux-native installs) with no native Windows; dependencies resolved to latest at build; the finalisation sweep (`gofmt`, `go vet`/`go fix`, `golangci-lint run`, tests); and the agent-facing markdown conventions (no bold, italic, emojis, horizontal rules, or headings deeper than `###`). Later documents rely on these living here rather than in each project doc.
- Keep the loader's nondeterministic inputs (clock, filesystem, network, environment) at the boundary so the cache and load logic are testable from inputs. The catalog-coverage rollup against models.dev (the per-provider supported/partial/absent verdicts) is consumed by the CLI and is not built here; this loader only needs to fetch, validate, and load.
- Prefer the same registry-loading shape `start` already uses over inventing a new one; the `modconfig` client honouring `CUE_REGISTRY` and `cue login` is the established pattern.

## Acceptance Criteria

- `go build ./...` and `go vet ./...` pass; `golangci-lint run` is clean.
- `cue vet` passes for the `catalog/` module and `cue mod tidy` leaves it clean.
- The seed `agents` map contains `claude-code` plus at least one agent with a different provider, every value verified against the real tool, and validates against `#KnownAgent`.
- The loader loads a fixture catalog via the source-module override and returns a `Catalog` whose `KnownAgent.ID` values equal their map keys, with `Config`, `Skills`, `Version`, and `Provider` populated as authored.
- Loader tests demonstrate: a fresh resolve populates the version cache; a within-TTL load uses the cached version; a failed re-resolution after TTL keeps the last resolved version; a first run with no network and no resolved version returns `ErrCatalogUnavailable`.
- The catalog data types are placed such that a root package can re-export them without `internal/catalog` importing the root package (verifiable by document 03 with no cycle).
