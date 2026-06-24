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
