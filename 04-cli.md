# 04 CLI, Configuration, and Distribution

## Goal

Deliver the agentdex command-line interface and the user-facing plumbing around it: `config.cue` loading and XDG resolution, the cobra command tree, the output envelope and exit-code taxonomy, the catalog/models.dev coverage rollup that drives `get` reporting, terminal rendering, and the build-and-distribution wiring. This completes a shippable agentdex: library plus thin CLI.

## Scope

In scope:

- `internal/config`: XDG resolution and the `config.cue` loader, mapping config plus flags into the library option set and client options.
- `internal/cli`: the cobra commands `list`, `get`, `models`, `refresh`, `version`, `completion`; global flags; the JSON envelope; the exit-code taxonomy; `--fields` selection.
- The catalog/models.dev coverage rollup (per-provider) that determines `get` exit codes.
- `internal/tui`: colour constants and table rendering, `NO_COLOR`-aware.
- `cmd/agentdex/main.go`: minimal entry point with ldflags version injection.
- Build and distribution wiring: the homebrew formula authored in the org tap, install instructions, and the version-injection pattern. The actual publish and tag are document 05.

Out of scope:

- The `skills` CLI command. It is a separate future project; omit it from this build.
- Library behaviour (detection, resolution, catalog load, models.dev client). Documents 01 to 03 own it; this document wires the CLI over it and must not duplicate or reimplement it.
- Publishing the catalog module, tagging a release, and filling the formula `sha256`. Document 05 (gated).

## Current State

Documents 01 to 03 are complete. The library is whole:

- `agentdex.Detect`, `DetectOne`, `LoadCatalog`, the `Option`/`ModelsOption` set, `Catalog.ResolveModel`, and the result types `Agent` and `ResolvedPaths`.
- The shared none/one/many matching helper from document 03, which the CLI selectors reuse.
- `modelsdev.Client` and the agent catalog loader, both option-driven and config-agnostic — this document is where `config.cue` and flags are read and mapped into their options.
- The repo `AGENTS.md`, `.golangci.yml`, and the dual module layout.

The engine reports raw per-provider facts; the supported/partial/absent coverage verdict and its exit codes are built here. The full design is `docs/agentdex-design.md`.

## References

- `docs/agentdex-design.md` — sections: CLI, Catalog and models.dev coverage, Output and exit codes, Configuration, Caching, Build and distribution.
- CLI output conventions, defined in full under Requirements below: the `status`/`data`/`error`/`warnings` JSON envelope, the exit-code taxonomy, ldflags injection of version/commit/date, and standard cobra command structure. These are agentdex's own conventions; settle them once here and do not invent divergent shapes per command.
- The agentdex homebrew formula follows the standard Go formula pattern: `std_go_args`, `CGO_ENABLED=0`, and ldflag injection of version/commit/date.

## Requirements

1. Configuration (`internal/config`)

   - Load user config from `$XDG_CONFIG_HOME/agentdex/config.cue`, CUE, validated at load. All fields optional; a clean config is empty. A malformed `config.cue` is a config error (exit 78).
   - Support the fields below and resolve per-cache TTL as: section ttl, then `cache_ttl`, then the built-in 24h.
   - Map config plus global flags into the library options (`WithSearchDirs`, `WithBinPaths`, `WithDisabled`, `WithModels`/`EnrichModels`, `WithCatalog`) and the catalog-loader and `modelsdev.Client` options (module path, URLs, TTLs, cache dir). No registry-auth settings; `modconfig` honours `CUE_REGISTRY` and `cue login`.

   ```cue
   cache_ttl?: string
   catalog: {
       module?: string | *"github.com/start-cli/agentdex/catalog@v1"
       ttl?:    string
   }
   models: {
       url?: string
       ttl?: string
   }
   search_dirs?: [...string]
   bin_paths?: [string]: string
   disabled_agents?: [...string]
   enrich_models?: bool | *true
   color?: "auto" | "always" | "never" | *"auto"
   ```

2. Commands

   ```
   agentdex list                    detected agents, table by default
   agentdex get <agent>             detail for one agent (aliases: view, show)
   agentdex models <agent> [query]  models available to the agent; query fuzzy-matches
   agentdex refresh [target]        force refresh caches: catalog | models | all
   agentdex version
   agentdex completion
   ```

   - `list` does not enrich models by default (offline-fast once cached); `--models` opts in.
   - `get <agent>` enriches models and reports provider-env by default. `--no-models` opts out of per-model enrichment; provider-env still shows (it needs only the providers map). `--tree` prints the config directory tree without parsing contents. When detection succeeds but models.dev is unreachable with no cache, `get` degrades: print the detected agent, omit the Models and provider-env sections, warn that enrichment was unavailable, and exit 0.
   - `models <agent> [query]` lists the agent's provider models with pricing, limits, and capabilities. With a `query`, apply selector matching: a single match prints that model (with `--json` or `--fields canonical_id` exposing the canonical id); multiple matches list candidates.
   - `refresh [target]` forces a cache refresh for `catalog`, `models`, or `all`.

3. Selector matching

   - Every positional selector — `<agent>`, the model `[query]` — resolves by the same none/one/many rule against its relevant set, reusing the shared helper from document 03: exact match first (id, then case-insensitive name), then unique substring or prefix. None matched exits 3; one acts on the match; two or more lists candidates and exits 3.
   - The agent selector adds the catalog/models.dev coverage distinctions below.

4. Coverage rollup and exit codes (`get`)

   The library returns `ErrAgentUnknown` whenever a query matches no catalog agent. The CLI then matches the query against models.dev provider ids and names (exact, then unique substring or prefix; never against model ids) to choose between the two outcomes below. Provider verdicts are evaluated per provider, because `provider` is a list; the agent-level result is a rollup:

   | catalog | providers in models.dev | result |
   | --- | --- | --- |
   | yes | all present | report agent, enrich models, exit 0 |
   | yes | some present | report agent, enrich from present providers, warn naming the absent provider(s), exit 0 |
   | yes | none present | catalog data error: report agent, error, exit 78 |
   | no | query matches a models.dev provider | report that provider's models.dev data labelled as provider data, note install details unavailable (not catalogued), exit 3 |
   | no | query matches no models.dev provider | unknown: no such agent, list valid catalog ids, exit 2 |

   - A catalogued agent that is not installed is not found, exit 3.
   - A models.dev that cannot be reached (no network, no cache) is distinct from a provider being absent: `get` degrades with a warning and exits 0; a model-centric command (`models`) that cannot reach models.dev with no cache exits 75. The catalog being unloadable exits 75.

5. Output and exit codes

   - Text by default; a JSON envelope under `--json` with the standard status/data/error/warnings shape. `--json` is long form only; no `-j`.
   - `--fields` selection on `list`, `get`, and `models`. The `models` canonical id is exposed as a distinct `canonical_id` field; the short id stays `id`, so CLI and library never disagree on what `id` means.
   - Exit codes follow this taxonomy: 0 success, 1 failure, 2 usage, 3 not found, 4 permission, 5 conflict, 75 transient, 78 config.

6. Global flags

   | Flag | Purpose |
   | --- | --- |
   | --json | JSON envelope on stdout (long form only) |
   | --verbose | Add detail to stdout |
   | --quiet | Suppress non-essential output |
   | --no-input | Never prompt; fail fast on missing input |
   | --color | auto, always, never |
   | --debug | Diagnostic logging to stderr |
   | --search-dir | Extra binary search locations; csv and repeatable |
   | --bin-path | Override a specific agent's binary path; id=path, repeatable; wins over PATH and search dirs |

   - `enrich_models` sets the default for `get`'s per-model enrichment only; precedence is explicit `--models`/`--no-models`, then `enrich_models`, then the built-in default. It does not affect `list` (opts in via `--models`) or `models` (enrichment inherent). Provider-env is unaffected and shows whenever a client is attached.

7. Rendering (`internal/tui`)

   - Colour constants and table rendering, `NO_COLOR`-aware, honouring `--color auto|always|never` and terminal detection. Tables for `list`, `get`, and `models` text output.

8. Entry point and distribution

   - `cmd/agentdex/main.go` is a minimal entry point. Inject version, commit, and build date at build time via ldflags into the cli package version variable; `agentdex version` reports them.
   - Author the homebrew formula for agentdex in the org tap (`start-cli/homebrew-tap`) using the standard Go formula pattern: `std_go_args`, `CGO_ENABLED=0`, ldflag injection. Leave the release-specific `url`, `sha256`, and tag as placeholders; filling them is document 05. Document the `go install github.com/start-cli/agentdex/cmd/agentdex@latest` and `brew` install paths in the README.

## Constraints

- Pure Go, `CGO_ENABLED=0`, Go 1.25. Dependencies limited to those in the design: `spf13/cobra`, `fatih/color`, `golang.org/x/term`, `cuelang.org/go`, `cuelabs.dev/go/oci/ociregistry`, and the standard library; `net/http/httptest` for tests.
- The CLI is a thin wrapper over the library. Do not reimplement detection, resolution, the merge, or caching here; call the library. The coverage rollup is the one piece of CLI-only policy, and it composes library facts rather than reaching past the public API.
- Reuse document 03's shared none/one/many matching helper for selectors. Do not write a second matcher.
- Do not edit the homebrew formula's release fields (`url`, `sha256`, `commit`, version) or publish anything. That is document 05.
- Use the envelope and exit-code taxonomy defined above exactly; do not invent a divergent scheme.
- Follow the repo `AGENTS.md`.

## Implementation Plan

1. Implement `internal/config`: XDG resolution, the `config.cue` schema and loader with load-time validation, per-cache TTL resolution, and the mapping from config plus flags into library and client options. A malformed config surfaces as exit 78.
2. Implement `internal/tui`: colour handling (`NO_COLOR`, `--color`, terminal detection) and table rendering.
3. Build the cobra root and global flags, the JSON envelope, and the exit-code taxonomy defined above.
4. Implement `list`, `models`, `refresh`, `version`, and `completion`, wiring selectors to the shared matcher and `models`'s canonical-id output.
5. Implement `get` including `--tree`, `--no-models`, the models.dev-unreachable degrade-to-0 path, and the per-provider coverage rollup with its full exit-code table.
6. Implement `cmd/agentdex/main.go` with ldflags version injection.
7. Author the agentdex homebrew formula in the org tap with placeholder release fields, and document install paths.
8. Tests: envelope shape and JSON golden output, exit codes across the coverage table (all-present, some-present with warning, none-present 78, uncatalogued-but-provider 3, unknown 2, not-installed 3, models.dev-unreachable degrade 0, models command transient 75, malformed config 78), flag precedence for enrichment, and selector none/one/many. Isolate with `t.TempDir` and `t.Setenv`.

## Implementation Guidance

- The coverage rollup is detection-and-enrichment reporting, not validation: a working multi-provider agent stays exit 0 even when one provider is absent upstream, emitting a warning rather than failing. The all-present and all-absent rows are the single-provider special cases of the same per-provider rule. Acting on an absent provider belongs to other projects (catalog validation, doctor-style checks); `get` only surfaces it.
- Keep the unreachable-models.dev path (degrade, exit 0) firmly distinct from the absent-provider path (data error, exit 78). One is transient infrastructure, the other is a catalog data fact; conflating them produces misleading exit codes.
- The uncatalogued-query provider match is by provider id and name only, never model id: an agent maps onto models.dev only through a provider, so the provider axis is the only one an uncatalogued query can resolve against, and the data reported is that provider's, not the uncatalogued agent's. An agent name rarely equals a provider id, so this path is a discovery aid, not a claim the query is a known agent.
- Provider-env reporting needs only the small providers map, so `get` attaches a client unconditionally (adding `EnrichModels()` unless `--no-models`), while `list` attaches none by default (only under `--models`) to stay offline-fast. Honour that split so default `list` never blocks on the network.

## Acceptance Criteria

- `go build ./...`, `go vet ./...`, and `golangci-lint run` are clean; `CGO_ENABLED=0 go build` produces a working `agentdex` binary.
- `list` runs offline once cached and omits model enrichment by default; `--models` enriches.
- `get <agent>` on a fully supported agent reports detection, provider-env, and models at exit 0; on a some-present agent warns and stays exit 0; on an all-absent agent exits 78; on a not-installed catalogued agent exits 3; when models.dev is unreachable with no cache it degrades with a warning and exits 0.
- An uncatalogued query that names a models.dev provider reports that provider's data labelled as provider data and exits 3; a query matching neither exits 2 and lists valid catalog ids.
- `models <agent> sonnet` resolves to a single model and exposes the canonical id via `--json`/`--fields canonical_id` while `id` remains the short source id; an ambiguous query lists candidates and exits 3.
- `refresh catalog|models|all` forces the corresponding cache refresh; `models` with no cache and no network exits 75; a malformed `config.cue` exits 78.
- `--json` emits the standard status/data/error/warnings envelope; the exit-code taxonomy is as defined above.
- `agentdex version` reports the ldflag-injected version, commit, and build date; the org-tap formula builds agentdex with `CGO_ENABLED=0` and placeholder release fields.
- Selectors use document 03's shared matcher; no second matching implementation exists.
