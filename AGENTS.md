# agentdex

agentdex is a Go library plus thin CLI that detects AI coding agents installed on the local machine and reports their binary, version, config and skills directories, providers, and (enriched from models.dev) available models. It owns the outside of an agent — identity, location, paths, version, capability — and never reads an agent's internal configuration. The full design is in `docs/agentdex-design.md`; project documents (`NN-*.md`) carry the slice of work each builds.

## Module layout

The repository hosts two independent module systems that do not interfere:

- Go module at the repository root: `github.com/start-cli/agentdex`. The detection library (root package `agentdex`), the public models.dev subpackage (`modelsdev/`), private subsystems under `internal/`, and the CLI under `cmd/agentdex/`.
- CUE module under `catalog/`: `github.com/start-cli/agentdex/catalog@v1`. The `#KnownAgent` schema and the agent catalog data, published to the CUE Central Registry and fetched at runtime. It is versioned and published independently of the Go binary.

The Go build ignores `catalog/`; the CUE module is a self-contained CUE module with its own `cue.mod/module.cue`.

## Toolchain and platforms

- Go 1.25. Pure Go: no cgo, no C dependencies. The binary must build with `CGO_ENABLED=0`.
- CUE module language version pinned to `v0.16.0`. Do not use CUE features beyond that pin.
- Target platforms: Linux, macOS, and WSL (agents installed natively in the WSL Linux environment). No native Windows, and no Windows-host agents reached through WSL PATH interop.
- XDG base directories are resolved from the published environment variables with the documented home fallbacks, not from platform-specific user-dir helpers.
- Dependencies are resolved to latest at build time. Each new dependency is a standing liability; prefer the standard library and dependencies already carried.

## Catalog delivery and caching

The agent catalog is not embedded in the binary. It is fetched from the CUE Central Registry at runtime using `cuelang.org/go/mod/modconfig`, which honours `CUE_REGISTRY` and `cue login` with no agentdex-specific auth settings. The schema travels with the data: the published module bundles its own `schema.cue`, so the loader validates by evaluating the fetched module rather than carrying a schema of its own.

Caching is version-resolution caching layered over CUE's own module content cache, not a JSON snapshot of the catalog:

- agentdex caches the resolved catalog version under `$XDG_CACHE_HOME/agentdex/`, keyed by module path, for a TTL (24h default), and relies on CUE's content cache for the module data.
- Resolving the latest version (`ModuleVersions`) requires the network. Fetching a canonical `module@version` is served from CUE's content cache offline. This is the basis for the two behaviours below.
- On a failed re-resolution after the TTL expires, agentdex keeps using the last resolved version (the resolution is reported as stale so a caller can warn while still working).
- A first run with no network and no previously resolved version fails clearly with `ErrCatalogUnavailable`. This is accepted behaviour, not a defect.

## Adding an agent to the catalog

An agent is a catalog edit, not a code change. Each entry is one map key in
`catalog/agents.cue`; the key is the kebab-case id (`^[a-z0-9]+(-[a-z0-9]+)*$`)
and the single source of identity, so there is no `id` field inside the entry.
Report only the outside of the agent (identity, location, paths, version,
capability); never add a field that requires reading the agent's internal
configuration.

### 1. Research the outside facts

Gather the static facts the catalog stores and confirm each against the real
agent, not from memory:

- `bin`: the executable name resolved on PATH (`exec.LookPath`), no `.exe`.
- `config` / `skills` paths: global and optional local directories, written with
  `~` and XDG-style paths, not an absolute home.
- `version.args` and optional `version.pattern`: the flag that prints the version
  and a regex to extract it.
- `provider`: one or more real models.dev provider ids. This is the join key to
  models.dev enrichment; a wrong id silently drops model data. An agent that can
  drive any models.dev provider (e.g. opencode) is provider-agnostic: set
  `agnostic: true` and omit `provider` entirely — the schema rejects an entry
  that has both. Callers supply the enrichment set at query time via
  `--provider`; never infer it from the agent's internal configuration.

When an agent supports the shared `.agents/` and `~/.agents/` convention, prefer
those paths over the agent's native equivalents for the slot that maps to them
(usually `skills`), the same way `agy` records `~/.agents/skills` and
`.agents/skills`. Keep the native location only where the agent has no `.agents`
mapping for that slot (usually `config`).

### 2. Add the entry

Add a block alongside the existing agents in `catalog/agents.cue`:

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

An agnostic entry (e.g. opencode) sets `agnostic: true` and omits `provider`.

Fields, per `catalog/schema.cue`:

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Human display name, non-empty |
| `bin` | yes | Executable resolved on PATH, non-empty |
| `config.global` | yes | Global config directory |
| `provider` | unless agnostic | One or more models.dev provider ids; the join key; forbidden when `agnostic` is true |
| `agnostic` | no | Defaults false; true marks a provider-agnostic agent with no home provider |
| `description` | no | One sentence |
| `config.local` | no | Project-local config directory |
| `skills.global` | with `skills` | Required when `skills` is present |
| `skills.local` | no | Project-local skills directory |
| `version.args` | no | Appended to the binary, e.g. `["--version"]` |
| `version.pattern` | no | Regex to extract the version string |
| `homepage` | no | Project URL |

### 3. Validate locally

From `catalog/`:

```bash
cue vet ./...
cue mod tidy
```

`cue vet` validates by evaluation because `schema.cue` travels with the data; a
missing required field or an empty path fails here. `cue mod tidy` must leave the
module clean.

### 4. Exercise through the library

Point the loader at the local module with the `catalog.module` override rather
than the registry, then confirm detection before publishing:

```bash
agentdex list
agentdex get <id>
```

### 5. Publish a new catalog version

The catalog is versioned and published independently of the Go binary, so adding
an agent needs no agentdex release. Publish a new version under the `@v1` major
line to the CUE Central Registry with the same mechanism start/library uses;
`cue login` and `CUE_REGISTRY` are honoured as-is, with no agentdex-specific auth.
Existing installs resolve the new version within the cache TTL (24h default); new
installs resolve it immediately via `ModuleVersions`.

## Style

Go:

- Match the surrounding code's conventions. Push nondeterministic inputs (clock, filesystem, network, environment) to the boundary so core logic is testable from inputs.
- Comments document why, not what. Respect godoc form on exported symbols.
- Tests favour real behaviour over mocks: real CUE validation, real files via `t.TempDir()`, environment isolation via `t.Setenv`. Table-driven tests for multiple cases.

Markdown for agent-facing documents (`AGENTS.md`, project documents, design notes):

- No bold or italic, no horizontal rules, no emojis, no HTML comments.
- No heading depth beyond `###`. No directory structures beyond depth 3.
- Single blank lines between sections. Inline code, code blocks, tables, and lists are fine.

## Finalisation sweep

Before declaring work complete, run from the repository root:

- `gofmt -l .` (and `go fix ./...` where applicable) — formatting clean.
- `go build ./...` and `go vet ./...` — pass.
- `golangci-lint run` — clean.
- `go test ./...` — pass.
- For the catalog CUE module: `cue vet ./...` from `catalog/`, and `cue mod tidy` leaves it clean.

## Commit convention

Scoped Commits (https://scopedcommits.com).

- Format: `<scope>: <description>`, optional body, optional trailers.
- Scope is the subsystem, module, or area touched (e.g. `catalog`, `loader`, `docs`).
- Multiple scopes comma-separated. No `feat`/`fix` type prefix.
