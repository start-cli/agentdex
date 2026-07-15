# Provider-agnostic agents

## Goal

Support catalog agents that have no home provider and can drive any models.dev provider, so agents like opencode can be catalogued honestly without hardcoding a provider list that drifts. Add opencode as the first such agent.

## Scope

In scope:

- A new `agnostic` capability on `#KnownAgent` and its Go decode type.
- Completing demand-driven enrichment for every agent: skip models.dev and the home-provider coverage rollup when the requested output does not need them; for agnostic agents the provider set is supplied by the caller, not the catalog. The Models OR rule on get is already shipped (see Progress).
- A `--provider` argument on the enrichment commands, threaded to detection via a new library option and to `ResolveModel` via a required `providers []string` argument (v0 signature change).
- New sentinel errors for the agnostic-without-providers case and for an unknown caller-supplied provider id, both mapped to CLI usage (exit 2); CLI rejection of `--provider` on a home-provider agent.
- The opencode catalog entry as the first agnostic agent.
- Amending the design doc's provider invariant and republishing the catalog module.

Out of scope:

- Reading any agent's internal auth store or config to discover which providers it is configured for. The boundary holds: agentdex reports the outside only.
- Reopening Models-on-by-default on get, restoring `enrich_models` config, or reintroducing `--no-models`. Unfiltered get already reports provider-env and coverage without filling Models; keep that baseline. Home-provider unfiltered `get` (when the models.dev gate fires), `models`, and `list` model counts stay as they are today except where requirement 3's remaining gates narrow client attachment and coverage.
- Auto-discovering providers from the environment as a fallback. Omitting providers for an agnostic agent is an explicit error, never a guess.

## Progress

Done (landed in `cli, config: make model enrichment on get opt-in`, formerly project 03):

- Models fill on get is opt-in under the OR rule: `--models`, or a non-empty `--fields` selection that includes `models`. Empty `--fields` is unfiltered and does not demand Models.
- Unfiltered get still attaches models.dev for provider-env and runs `getCoverage`.
- Config key `enrich_models` and flag `--no-models` removed; leftover `enrich_models` fails closed-schema config load (exit 78).
- Design doc and README describe Models opt-in; CLI helper `modelsDemand` in `internal/cli/get.go` implements the OR rule and is covered by tests.

Not done (this project's remaining work):

- Schema and decode for `agnostic`; opencode catalog entry.
- Soft-path / `ErrProvidersRequired` demand gate and models.dev / `getCoverage` demand gate (requirement 3, first two bullets). Found agents still always enter `getCoverage`.
- `WithProviders`, engine provider-set choice, `ResolveModel` providers argument, `ErrUnknownProvider`, shared validation helper.
- `--provider` on get, models, and list; agnostic command matrix; list `-` marker; design/AGENTS agnostic notes; catalog republish.

## Current State

The Go module root holds the detection library; `catalog/` is the independent CUE module.

Catalog schema (`catalog/schema.cue`) defines `#KnownAgent` with `provider: [string, ...string]` — at least one required. There is no `agnostic` field. The map key is the agent id. `catalog/agents.cue` holds `claude-code` and `agy`, both with a concrete `provider` list. No opencode entry.

Go types (`agent.go`): `KnownAgent` carries `Provider []string` and no agnostic flag; the detected `Agent` carries `Providers []string`, `ProviderEnv map[string]bool`, and `Models []modelsdev.Model`. `KnownAgent` is decoded from the fetched CUE module in `internal/catalog/decode.go` (and mapped through `fromInternalCatalog` in `catalog.go`).

Engine (`engine.go`): `detectAgent` copies `ka.Provider` into `a.Providers`, then calls `enrich`. `enrich` (`probe.go`) is a no-op when no models.dev client is attached; otherwise it iterates `a.Providers`, resolves each through the models.dev client, fills `ProviderEnv`, and — only when `EnrichModels()` was requested — fills `Models`. A models.dev outage with no cache degrades `ProviderEnv` and `Models` to nil rather than failing; a schema fault propagates.

Library API (`agentdex.go`): detection runs are configured through `Option` values. `WithModels(client, ...)` attaches the models.dev client and enables provider-env reporting; `EnrichModels()` additionally fills `Models`. There is no `WithProviders`. Without `WithModels`, no provider data is computed at all — the demand-driven seam already exists at the library layer. `Detect` runs the whole catalog; `DetectOne` targets one id. `Catalog.ResolveModel` (`resolve.go`) takes `(ctx, agentID, query, mc)` and fuzzy-matches against the agent's catalog `provider` list only.

CLI (`internal/cli/`):

- `get`, `models`, and `list` are the enrichment commands. `get` and `models` accept `--fields`. There is no `--provider` flag on any command.
- `get` (`get.go`): after a found detect, always calls `getCoverage` (rollup + models.dev). Models fill is gated by `modelsDemand(models, fields)` before the second detect; provider-env fill is not gated by field selection. Unfiltered get reports provider-env when models.dev is reachable; Models only under the OR rule. get degrades with a `warnings` envelope entry when a provider is absent or models.dev is unreachable. Catalog agent whose every provider is absent still exits 78 via coverage, including under `--fields skills_dir` or `--fields providers` (presentation omits unselected keys, but the rollup still runs).
- `models` lists via `cat.Agents[id].Provider` and resolves a query via `ResolveModel` — neither path accepts caller providers. Unknown provider ids on a catalog list are skipped silently in `modelsList`; empty matches become `ErrModelNotFound`.
- `list` attaches models.dev and `EnrichModels()` unconditionally for model counts, degrading to zero with a warning when unreachable.
- JSON envelope (`envelope.go`, used from `root.go`) carries `status`, `data`, `error`, and `warnings`. Exit mapping is in `exit.go` (`codeFor`) and `modelsCode` in `models.go`.

Sentinel errors today: `ErrCatalogUnavailable`, `ErrAgentUnknown`, `ErrModelAmbiguous`, `ErrModelNotFound`. No `ErrProvidersRequired` or `ErrUnknownProvider`.

Design doc (`docs/agentdex-design.md`) and `AGENTS.md` still state that every catalog entry requires at least one `provider`. Neither describes agnostic agents or caller-supplied providers.

## References

- Cloned opencode source: `~/.agents/context/opencode` (github.com/anomalyco/opencode). Key files consulted: `packages/core/src/global.ts` (XDG path resolution: config is `xdgConfig/opencode` = `~/.config/opencode`), `packages/opencode/src/config/paths.ts` (project config dirs walk up `.opencode`; config files `opencode.json`/`opencode.jsonc`), `packages/opencode/src/skill/index.ts` (skill discovery: `AGENTS_EXTERNAL_DIR = ".agents"`, `CLAUDE_EXTERNAL_DIR = ".claude"`, external pattern `skills/**/SKILL.md`, native pattern `{skill,skills}/**/SKILL.md`), `packages/opencode/src/index.ts` (`--version` / `-v` flag), `packages/core/src/plugin/provider/` (~32 first-class provider plugins). opencode uses models.dev as its own model database, so its provider set is definitionally "all of models.dev."
- models.dev catalog: `https://models.dev/catalog.json`, top-level `{ models, providers }`. Every provider entry carries an `env` array (the API-key variable names) used for provider-env presence. All of anthropic, openai, google, google-vertex, openrouter, github-copilot, groq, mistral, xai, deepseek, amazon-bedrock, azure, cohere, perplexity, togetherai, vercel are present as provider keys.
- `AGENTS.md`, section "Adding an agent to the catalog", including the rule to prefer `.agents/` and `~/.agents/` paths where an agent supports them.
- Models opt-in baseline: `internal/cli/get.go` (`modelsDemand`, `getCoverage`), tests in `internal/cli/get_test.go` (`TestGetAllPresent`, `TestGetModelsOptIn`, `TestModelsDemand`).

## Requirements

1. `#KnownAgent` gains an `agnostic` boolean defaulting to false. When `agnostic` is false, `provider` remains required (unchanged). When `agnostic` is true, `provider` is absent — the entry carries no provider list. The schema must reject an entry that both sets `agnostic: true` and declares `provider`, and must reject an entry that is not agnostic and omits `provider`.

2. The Go decode type for `#KnownAgent` gains the agnostic flag, decoded from the fetched module. Existing entries decode unchanged with agnostic false.

3. Enrichment becomes demand-driven end to end, for every agent (home-provider and agnostic). The provider-related agent fields are exactly `providers`, `provider_env`, and `models`; every other `agentFieldSet` key is non-provider. Demand is evaluated with three separate gates — do not collapse them into one set:

   - Soft path / `ErrProvidersRequired` demand (not done): the requested output intersects `{providers, provider_env, models}`, or the caller passed explicit `--models`. Unfiltered get counts as demanding all three, except on the agnostic soft path (requirement 8).
   - models.dev client and home-provider `getCoverage` (not done): only when the requested output intersects `{provider_env, models}`. The `providers` field alone is catalog or caller list data — filled without models.dev and without the coverage rollup. A home-provider agent whose catalog providers are all absent from models.dev must exit 0 under `--fields skills_dir`, `--fields providers`, or any selection that omits both `provider_env` and `models` (today those selections still run rollup and can exit 78).
   - `EnrichModels()` OR rule (done for home-provider get; reuse for agnostic/`--provider` paths): only when Models is demanded under the OR rule — `--models` is set, or `--fields` is non-empty and the selection includes `models`. Empty `--fields` is unfiltered and does not demand Models on its own. A selection limited to `provider_env` (with or without `providers`) attaches the client for provider-env only and must not fill `Models`. Do not reimplement `modelsDemand`; call the existing helper (or extract it if needed for shared use).

4. For an agnostic agent, the provider set used for enrichment and model resolution comes from the caller, not from the catalog. Thread it through a new library option for `Detect`/`DetectOne` (e.g. `WithProviders`). Change `ResolveModel` to a required provider-set argument:

   ```go
   func (c *Catalog) ResolveModel(ctx context.Context, agentID, query string, mc *modelsdev.Client, providers []string) (m modelsdev.Model, providerID string, canonicalID string, err error)
   ```

   This is an intentional v0 breaking signature change; update in-repo callers (CLI and tests). Semantics: resolve the provider set once at the call site and pass it — home-provider callers pass the catalog `provider` list; agnostic callers pass the caller-supplied ids. Empty `providers` for an agnostic agent is `ErrProvidersRequired`. For a home-provider agent, if the passed list is empty, fall back to the catalog `provider` list (defensive); a non-empty passed list is the search set (callers should pass the catalog list). The Detect/`WithProviders` path still ignores caller providers for home-provider agents and uses the catalog list only.

5. A new sentinel error `ErrProvidersRequired` is returned when soft-path / provider-related demand (requirement 3, first gate) applies for a single agnostic agent and no caller providers were supplied (`DetectOne`, and single-agent CLI paths built on it). The error message names the agent and how to supply providers (e.g. `--provider`). Multi-agent `Detect` must not fail for that case: skip enrichment for the agnostic agent (Providers empty, ProviderEnv and Models nil) and continue, so `list` can render a marker instead of a count.

6. Caller-supplied provider ids are validated against models.dev on every path that uses them — Detect/enrich (`get`, `list` with `--provider`), `models` list, and `models` query via `ResolveModel`. An id that is not a models.dev provider is an error, not a silent drop, when models.dev is reachable. That error is the sentinel `ErrUnknownProvider` (message names the id). Do not rely on enrich alone: `modelsList` and `ResolveModel` today continue on absent providers, which would turn an unknown id into an empty list or `ErrModelNotFound`. Expose one library helper that checks each id when models.dev is reachable and returns `ErrUnknownProvider` for the first unknown; call it from enrich for agnostic caller providers and from the `models` command (or from `ResolveModel` when the caller marks the set as caller-supplied) before listing or resolving. Catalog provider lists on home-provider agents are not validated this way — absent catalog providers stay a coverage/silent-skip fact, not `ErrUnknownProvider`. Both `ErrProvidersRequired` and `ErrUnknownProvider` are caller/usage faults: CLI exit 2 (`codeUsage`), never catalog data error (78) and never transient (75). Map both in `codeFor` and in `modelsCode` so `get` and `models` agree. The get coverage rollup's "none present → catalog data error" path applies only to home-provider agents, whose provider list comes from the catalog.

7. The `get` and `models` commands accept a `--provider` argument (repeatable and/or comma-separated models.dev ids). On a home-provider agent, supplying `--provider` is rejected with a usage error — the catalog is authoritative. The `list` command accepts `--provider` and applies it only to agnostic agents' counts; home-provider agents use their catalog providers and `--provider` is not rejected there.

8. Command behaviour matches this matrix. Not-installed (Found false) always exits 3 with catalogued outside facts first, the same contract as every other agent; soft path exit 0 never applies when Found is false. `get --models` is the explicit opt-in half of the Models OR demand rule (requirement 3) on agnostic agents as on home-provider agents; there is no `--no-models` flag and no config that opts out once demanded (default is already off).

   - `get <agnostic>` soft path (no field filter, no `--provider`, and not `--models`), Found: returns all non-provider fields, omits `providers`, `provider_env`, and `models`, and adds a `warnings` entry stating the agent is provider-agnostic and how to enrich. Exit 0.
   - `get <agnostic>` soft-path conditions, not Found: exit 3 with the not-installed error; payload matches soft path (outside facts, omit the three provider fields, agnostic warning).
   - `get <agnostic> --models` with no `--provider`: `--models` demands the `models` field — `ErrProvidersRequired`, exit 2 (not soft path).
   - `get <agnostic> --fields <non-provider only>`: returns those fields, no models.dev fetch, no warning (exit 3 when not Found).
   - `get <agnostic> --fields models` (or `providers`, or `provider_env`) with no `--provider`: `ErrProvidersRequired`, exit 2 (provider demand is independent of install state).
   - `get <agnostic> --provider <ids>`, Found: enrich against exactly those providers. Models fill follows the OR rule (requirement 3): bare `--provider` without Models demand keeps provider-env and omits Models; `--provider` with `--models` or `--fields` including `models` fills Models. Provider-env is filled whenever a models.dev client is attached. `--fields provider_env` with `--provider` attaches the client without `EnrichModels()`.
   - `get <agnostic> --provider <ids>`, not Found: exit 3 with the not-installed error. Payload mirrors home-provider not-installed: outside facts plus `providers` set to the caller ids; omit `provider_env` and `models` (do not attach a models.dev client until Found). No soft-path agnostic warning — the caller already supplied providers. Same ordering as today: detect without models first, branch on Found, enrich only when Found.
   - `models <agnostic>` with no `--provider`: `ErrProvidersRequired`, exit 2.
   - `models <agnostic> --provider <ids>`: validate ids (requirement 6), then list models for exactly those providers.
   - `models <agnostic> <query> --provider <ids>`: validate ids (requirement 6), then fuzzy-resolve the query within those providers via `ResolveModel`.
   - `get <home-provider>` (no filter, no `--provider`): outside facts, provider-env, and coverage rollup when the models.dev path fires (none present → exit 78); fills Models only under the OR rule (not by default). Already true today for unfiltered get; keep it when adding the second gate.
   - `get <home-provider> --fields <non-provider only>`: returns those fields, no models.dev fetch, no coverage rollup (exit 3 when not Found). Not true today: still always enters `getCoverage`.
   - `get <home-provider> --fields providers` (and any selection that includes `providers` but neither `provider_env` nor `models`): returns the catalog provider list offline, no models.dev fetch, no coverage rollup (exit 3 when not Found). Does not exit 78 when catalog providers are absent from models.dev. Not true today.
   - `get <home-provider> --fields provider_env` (binary found): attaches models.dev for provider-env only, no `EnrichModels()`, still runs coverage for the env path; does not fill `Models`. Models omission under this selection is already true via `modelsDemand`; client attachment is not yet field-gated.
   - `get <home-provider> --provider <ids>`: rejected with a usage error.
   - `models <home-provider> --provider <ids>`: rejected with a usage error.
   - `list`: model count for home-provider agents; for agnostic agents, a real count when `--provider` is given, otherwise not-applicable: text cell `-` and JSON `models: null`. Do not use `[]`/`0` for that case — those remain the degrade shape for a home-provider agent when models.dev is unreachable (see `withModels` today). With `--provider`, agnostic rows match home-provider shape: text count and JSON model array. `list` never hard-fails because an agnostic agent is present.

9. opencode is added to `catalog/agents.cue` as the first agnostic agent, and validates under `cue vet`.

10. The design doc (`docs/agentdex-design.md`) provider section is amended to describe agnostic agents and the caller-supplied-provider contract, replacing the unconditional "at least one required" statement.

## Constraints

- Pure Go, `CGO_ENABLED=0`, Go 1.25. CUE module language pinned to `v0.16.0`; use no feature beyond that pin.
- Catalog data stays backward-compatible: existing entries decode unchanged, agnostic defaults false, no breaking bump of the catalog `@v1` major. Preserve the shipped Models opt-in baseline. Completing demand-driven field selection (requirement 3, second gate) is in scope and must make selections that do not demand `{provider_env, models}` skip models.dev and coverage. The Go library is v0: a breaking `ResolveModel` signature change (required `providers []string`) is allowed and preferred over a variadic or dual-method shim.
- Preserve the boundary. Determine agnostic providers only from the caller's argument and validate them against models.dev. Never read opencode's (or any agent's) auth store or internal config to infer providers.
- Keep nondeterministic inputs at the boundary. Provider-id validation and models.dev access stay behind the existing client seam; core logic remains testable from inputs.
- Follow the catalog-addition workflow and markdown rules in AGENTS.md, including the `.agents/` path-priority rule.

## Implementation Plan

1. Schema. Add `agnostic: bool | *false` to `#KnownAgent` and gate `provider` on it so that a non-agnostic entry requires `provider` and an agnostic entry forbids it. Validate both directions with `cue vet`: a good agnostic entry passes, and an agnostic-plus-provider entry (and a non-agnostic-without-provider entry) fail. Keep synthetic fixtures under `testdata/` and `internal/catalogtest` consistent with the schema.

2. Go decode type. Add the agnostic field to public and internal `KnownAgent`, populate it in `internal/catalog/decode.go`, and map it in `fromInternalCatalog`. Existing entries continue to decode with agnostic false and their provider list intact.

3. Library option, enrichment, ResolveModel, and provider validation. Add an option that carries caller-supplied provider ids (e.g. `WithProviders`). In the engine, choose the enrichment provider set per agent: catalog `provider` for non-agnostic agents, caller providers for agnostic agents. When a models.dev client is attached for an agnostic agent with no caller providers: `DetectOne` returns `ErrProvidersRequired`; multi-agent `Detect` skips enrichment for that agent only (Providers empty, ProviderEnv and Models nil) and continues. Change `ResolveModel` to take required `providers []string` (breaking v0 signature); update `models` CLI and tests. Call sites resolve the set once (catalog list vs `--provider`) and pass it. Empty providers on an agnostic agent → `ErrProvidersRequired`. Add a library helper that validates a caller-supplied provider id list against models.dev when reachable (`ErrUnknownProvider` on the first unknown id) and no-ops into the existing degrade path when models.dev is unreachable. Call it from enrich for agnostic caller providers and from the `models` command before `modelsList` / `ResolveModel` (or inside `ResolveModel` when validating caller sets). Do not run this helper on home-provider catalog lists. Keep the existing degrade-to-nil behaviour for transient models.dev gaps.

4. Sentinel errors. Add `ErrProvidersRequired` and `ErrUnknownProvider` to `errors.go` with godoc describing when each is returned. Map both to `codeUsage` (exit 2) in `codeFor` and in `modelsCode` so neither falls through to failure (1) or transient (75).

5. CLI field-selection gates and `--provider`. Do not reimplement the Models OR rule: keep using `modelsDemand` (third gate). Add the missing first two gates on `get` (and any shared helper): soft-path / `ErrProvidersRequired` demand as intersection with `{providers, provider_env, models}` or explicit `--models`; attach the models.dev client and enter `getCoverage` only when demand intersects `{provider_env, models}`. Unfiltered home-provider get still demands the second gate (provider-env path) and must keep today's coverage rollup. A `--fields providers` request (alone or with other non-models.dev fields) skips models.dev and coverage for every agent. Add `--provider` to `get`, `models`, and `list`. On `get`/`models`, reject `--provider` on a home-provider agent with a usage error. For agnostic agents without `--provider`: soft-path / provider-related demand without caller providers is `ErrProvidersRequired` (regardless of install state); soft path applies only when unfiltered, no `--provider`, and not `--models` — outside facts only, omit the three provider-related fields, agnostic warning, no models client on `DetectOne`. Soft path exit 0 applies only when Found; when Found is false the same payload is reported with the not-installed error at exit 3. For `models`, resolve the provider set (catalog vs `--provider`), run the shared validation helper on caller-supplied ids, then pass the same `[]string` into `modelsList` and `ResolveModel` — do not read catalog `Provider` alone for an agnostic agent, and do not list or resolve before validation. For `get` on an agnostic agent with `--provider`: do not run the catalog-data-error rollup branches. Detect without models first (with `WithProviders` so `Providers` carries the caller ids); when not Found, report exit 3 with that payload and do not attach models.dev; when Found, enrich through a second `DetectOne` with `WithProviders`/`WithModels` (and `EnrichModels()` only via `modelsDemand`) and surface library errors (unknown provider, `ErrProvidersRequired`) directly.

6. list marker. For an agnostic agent without `--provider`, do not call the degrade-normalising `withModels` path: set the models field explicitly to JSON `null` and text `-` (not applicable / not enriched). When `--provider` is supplied, enrich and use the normal count/`[]` path. Keep home-provider degrade as `[]`/`0` so scripts can tell "agnostic, no providers supplied" (`null`/`-`) from "enriched but empty or models.dev down" (`[]`/`0`). Look up agnostic from the loaded catalog (or equivalent); rely on `Detect`'s soft-skip so a mixed catalog never fails the listing.

7. opencode entry. Add opencode to `catalog/agents.cue`:

   ```cue
   agents: "opencode": {
       name:        "opencode"
       bin:         "opencode"
       description: "Open-source, provider-agnostic AI coding agent for the terminal."
       config: {
           global: "~/.config/opencode"
           local:  ".opencode"
       }
       skills: {
           global: "~/.agents/skills"
           local:  ".agents/skills"
       }
       version: {
           args:    ["--version"]
           pattern: "([0-9]+\\.[0-9]+\\.[0-9]+)"
       }
       agnostic: true
       homepage: "https://opencode.ai"
   }
   ```

   The skills paths follow the AGENTS.md `.agents/` priority rule, matching the agy precedent; opencode reads `.agents/skills` via its external skill discovery.

8. Docs. Amend `docs/agentdex-design.md` provider section per requirement 10. Confirm the AGENTS.md catalog-addition workflow still reads correctly for an agnostic agent (agnostic agents omit `provider` and set `agnostic: true`); add a short note there if the field table needs it.

9. Tests. Cover: schema validation both directions (agnostic ok, agnostic-plus-provider fails, non-agnostic-without-provider fails) using real CUE validation; decode of an agnostic entry; the enrichment branch (caller providers for agnostic, catalog for home-provider, `ErrProvidersRequired` on demand-without-providers); shared-helper `ErrUnknownProvider` on unknown-id rejection from enrich and from the models list/query paths (not only get); `ResolveModel` with caller providers for agnostic agents; home-provider `--fields skills_dir` / `--fields providers` with all catalog providers absent from models.dev (exit 0, no models.dev client / no exit 78 — these fail today); and the full CLI matrix from requirement 8, including `models` list and query with `--provider`, the `list` marker (text `-` and JSON `null` for agnostic without `--provider`; count/array with `--provider`; home-provider degrade still `0`/`[]`), the `get` soft-path warning when Found, not-installed exit 3 with soft-path payload when not Found, not-installed exit 3 with `--provider` (caller ids on `providers`, no provider_env/models, no soft-path warning), `get --models` without `--provider` as `ErrProvidersRequired` at exit 2, `models` without `--provider` at exit 2 (not 75), unknown `--provider` at exit 2 on both `get` and `models` (list and query), bare `get --provider …` keeping provider-env while omitting Models, and a separate `--provider --models` (or `--fields models`) case that fills Models. Keep existing Models opt-in tests green; they define the third-gate baseline. Follow the repo's real-behaviour test conventions.

10. Republish. After the catalog module validates and `cue mod tidy` is clean, publish a new `@v1` version of the catalog to the CUE Central Registry so installs resolve opencode and the `agnostic` schema within the cache TTL.

## Implementation Guidance

- The demand-driven seam already exists: `enrich` is a no-op without a models.dev client. Lean on it. The third gate already exists as `modelsDemand` on get — reuse it for agnostic and `--provider` paths; do not invent a second OR rule. Soft-path / `ErrProvidersRequired` uses `{providers, provider_env, models}` (plus explicit `--models`). Client attachment and `getCoverage` use only `{provider_env, models}` — this gate is the main home-provider change left. Unfiltered get demands all three for the first gate and both models.dev fields for the second (provider-env path), except on the agnostic soft path — but does not demand Models fill. Prefer wiring the first two gates to client attachment and whether `getCoverage` runs, over adding a parallel gate inside the engine.
- Keep the reject-on-home-provider rule at the CLI layer for `get`/`models`. At the library layer a `Detect` run legitimately mixes agnostic and home-provider agents while caller providers are set, so the library should apply caller providers to agnostic agents and ignore them for home-provider agents rather than erroring. When caller providers are absent, `Detect` soft-skips enrichment for agnostic agents; only `DetectOne` returns `ErrProvidersRequired`.
- `models` is not a Detect path: resolve the provider set once (catalog vs `--provider`), validate caller-supplied ids with the shared helper, then hand the same `[]string` to `modelsList` and to `ResolveModel(..., providers)`. Prefer the required provider-set argument over rewriting models through `DetectOne`. Do not leave unknown-id rejection to `modelsList`'s silent skip or to `ResolveModel`'s empty-match → `ErrModelNotFound`.
- Split get's coverage path by agent kind. Home-provider: keep `getCoverage` rollup (including "none present → catalog data error, exit 78") only when the models.dev gate fires. Agnostic: no catalog-fault rollup; caller providers are validated via the shared helper, and a bad or missing set is never reported as a catalog data error. With `--provider`, apply `modelsDemand` for `EnrichModels()` the same way as home-provider get; do not hard-code always-on Models.
- Provider-id validation depends on models.dev reachability and lives in one library helper used by every caller-supplied path. When models.dev is unreachable, enrichment already degrades to nil; in that state an unknown-id error is not possible, so the helper participates in the same reachable-only path rather than inventing a separate hard gate.
- opencode's binary may not be installed on a given machine; that is a normal not-found result, exactly like any other catalogued agent. The entry describes the agent regardless of local presence. Not-installed (exit 3) always outranks soft path (exit 0): soft path is only the success-path shape for an unfiltered agnostic get without `--provider` and without `--models` when Found is true. With `--provider` and not Found, still exit 3 without models.dev enrichment — same as home-provider get — while keeping the caller ids on `providers`.
- Tests that only assert presentation omit of unselected keys (e.g. `TestGetFieldsOmitModelsKey`) are not sufficient for the second gate: add cases that prove no models.dev access and no exit 78 when catalog providers are all absent under non-models.dev field filters.
- list's models field is three-valued for rows: a model array (count in text), `null`/`-` (agnostic without `--provider`), and `[]`/`0` (home-provider degrade or genuine empty). Do not collapse the second into the third via `withModels`'s nil→`[]` normalisation.

## Acceptance Criteria

Already true (preserve; do not regress):

- Unfiltered `get <home-provider>` (binary found, models.dev reachable) reports provider-env, omits Models unless `--models` or `--fields` includes `models`, and still runs coverage (none present → exit 78).
- `get <home-provider> --models` and `get <home-provider> --fields models` fill Models when the models.dev path succeeds.
- Leftover config `enrich_models` fails closed-schema load; `--no-models` is rejected as unknown usage.

Still required (not true until this project lands):

- `cue vet ./...` from `catalog/` passes with the opencode entry present, and `cue mod tidy` leaves the module clean.
- An agnostic entry that also declares `provider`, and a non-agnostic entry that omits `provider`, both fail `cue vet`.
- `get opencode --fields skills_dir` returns the skills dir with no models.dev access and no warning.
- `get <home-provider> --fields skills_dir` (binary found) returns the skills dir with no models.dev access and exit 0, even when that agent's catalog providers are all absent from models.dev (no exit 78 under a non-models.dev field filter).
- `get <home-provider> --fields providers` (binary found) returns the catalog provider list with no models.dev access and exit 0, even when those providers are all absent from models.dev (no exit 78).
- `get <home-provider> --fields provider_env` (binary found) reports provider-env and omits Models (no `EnrichModels()` for that selection).
- `get opencode` with no `--provider` and no `--models`, when the binary is found, returns the outside facts, omits `providers`, `provider_env`, and `models`, and carries a provider-agnostic warning, exiting 0.
- Unfiltered `get claude-code` (or equivalent home-provider) still applies today's coverage rollup when the models.dev gate fires.
- `get opencode` with no `--provider` and no `--models`, when the binary is not found, exits 3 with the not-installed error, still omits the three provider fields, and carries the provider-agnostic warning.
- `get opencode --provider anthropic`, when the binary is not found, exits 3 with the not-installed error, reports `providers: ["anthropic"]` (or equivalent), omits `provider_env` and `models`, and does not carry the soft-path agnostic warning.
- `get opencode --fields models`, `get opencode --models`, and `models opencode`, all without `--provider`, fail with `ErrProvidersRequired` and exit 2 (not 1, not 75).
- `models opencode --provider anthropic,openai` lists models for exactly those providers; `models opencode sonnet --provider anthropic` resolves within that set.
- `get opencode --provider anthropic,openai` enriches against exactly those providers with provider-env; Models filled only when `--models` or field demand includes `models`.
- `get opencode --provider anthropic` (bare, no Models demand) reports provider-env for that provider and omits Models.
- `get opencode --provider anthropic --models` fills Models for that provider.
- `get opencode --provider <unknown-id>` and `models opencode --provider <unknown-id>` fail with `ErrUnknownProvider` when models.dev is reachable, exit 2, and are not labelled a catalog data error (not exit 78) or a models.dev outage (not exit 75).
- Unfiltered `get claude-code` (home-provider) still runs coverage when the models.dev/provider-env path fires and rejects `get claude-code --provider openai` as a usage error. Models on that unfiltered get remain opt-in under the OR rule.
- `list` shows a count for home-provider agents and, for opencode without `--provider`, text `-` with JSON `models: null` (not `[]`/`0`); with `--provider` it shows a real count/array. `list` does not fail with opencode in the catalog.
