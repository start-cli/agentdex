# Provider-agnostic agents

## Goal

Support catalog agents that have no home provider and can drive any models.dev provider, so agents like opencode can be catalogued honestly without hardcoding a provider list that drifts. Add opencode as the first such agent.

## Scope

In scope:

- A new `agnostic` capability on `#KnownAgent` and its Go decode type.
- A demand-driven enrichment contract: provider data (models, provider list, provider-env presence) is computed only when the requested output needs it, and for agnostic agents the provider set is supplied by the caller, not the catalog.
- A `--provider` argument on the enrichment commands, threaded to detection via a new library option and to `ResolveModel` via a caller provider-set argument.
- A new sentinel error for the agnostic-without-providers case, and CLI rejection of `--provider` on a home-provider agent.
- The opencode catalog entry as the first agnostic agent.
- Amending the design doc's provider invariant and republishing the catalog module.

Out of scope:

- Reading any agent's internal auth store or config to discover which providers it is configured for. The boundary holds: agentdex reports the outside only.
- Changing how home-provider agents (claude-code, agy) behave. Their path stays byte-for-byte identical.
- Auto-discovering providers from the environment as a fallback. Omitting providers for an agnostic agent is an explicit error, never a guess.

## Current State

The Go module root holds the detection library; `catalog/` is the independent CUE module.

Catalog schema (`catalog/schema.cue`) defines `#KnownAgent` with `provider: [string, ...string]` — at least one required. The map key is the agent id. `catalog/agents.cue` currently holds `claude-code` and `agy`, both with a concrete `provider` list.

Go types (`agent.go`): `KnownAgent` carries `Provider []string`; the detected `Agent` carries `Providers []string`, `ProviderEnv map[string]bool`, and `Models []modelsdev.Model`. `KnownAgent` is decoded from the fetched CUE module in `internal/catalog/decode.go`.

Engine (`engine.go`): `detectAgent` copies `ka.Provider` into `a.Providers`, then calls `enrich`. `enrich` (`probe.go`) is a no-op when no models.dev client is attached; otherwise it iterates `a.Providers`, resolves each through the models.dev client, fills `ProviderEnv`, and — only when `EnrichModels()` was requested — fills `Models`. A models.dev outage with no cache degrades `ProviderEnv` and `Models` to nil rather than failing; a schema fault propagates.

Library API (`agentdex.go`): detection runs are configured through `Option` values. `WithModels(client, ...)` attaches the models.dev client and enables provider-env reporting; `EnrichModels()` additionally fills `Models`. Without `WithModels`, no provider data is computed at all — the demand-driven seam already exists at the library layer. `Detect` runs the whole catalog; `DetectOne` targets one id. `Catalog.ResolveModel` (`resolve.go`) fuzzy-matches a model query against the agent's catalog `provider` list only.

CLI (`internal/cli/`): `get`, `models`, and `list` are the enrichment commands. `get` and `models` already accept a `--fields` selector. `get` enriches models by default and degrades with a `warnings` envelope entry when a provider is absent or models.dev is unreachable. `models` lists via `cat.Agents[id].Provider` and resolves a query via `ResolveModel` — neither path accepts caller providers today. `list` shows a per-agent model count. The JSON envelope (`internal/cli/root.go`, `envelope.go`) carries `status`, `data`, `error`, and `warnings`. Sentinel errors live in `errors.go`.

Sentinel errors today: `ErrCatalogUnavailable`, `ErrAgentUnknown`, `ErrModelAmbiguous`, `ErrModelNotFound`.

## References

- Cloned opencode source: `~/.agents/context/opencode` (github.com/anomalyco/opencode). Key files consulted: `packages/core/src/global.ts` (XDG path resolution: config is `xdgConfig/opencode` = `~/.config/opencode`), `packages/opencode/src/config/paths.ts` (project config dirs walk up `.opencode`; config files `opencode.json`/`opencode.jsonc`), `packages/opencode/src/skill/index.ts` (skill discovery: `AGENTS_EXTERNAL_DIR = ".agents"`, `CLAUDE_EXTERNAL_DIR = ".claude"`, external pattern `skills/**/SKILL.md`, native pattern `{skill,skills}/**/SKILL.md`), `packages/opencode/src/index.ts` (`--version` / `-v` flag), `packages/core/src/plugin/provider/` (~32 first-class provider plugins). opencode uses models.dev as its own model database, so its provider set is definitionally "all of models.dev."
- models.dev catalog: `https://models.dev/catalog.json`, top-level `{ models, providers }`. Every provider entry carries an `env` array (the API-key variable names) used for provider-env presence. All of anthropic, openai, google, google-vertex, openrouter, github-copilot, groq, mistral, xai, deepseek, amazon-bedrock, azure, cohere, perplexity, togetherai, vercel are present as provider keys.
- `AGENTS.md`, section "Adding an agent to the catalog", including the rule to prefer `.agents/` and `~/.agents/` paths where an agent supports them.

## Requirements

1. `#KnownAgent` gains an `agnostic` boolean defaulting to false. When `agnostic` is false, `provider` remains required (unchanged). When `agnostic` is true, `provider` is absent — the entry carries no provider list. The schema must reject an entry that both sets `agnostic: true` and declares `provider`, and must reject an entry that is not agnostic and omits `provider`.

2. The Go decode type for `#KnownAgent` gains the agnostic flag, decoded from the fetched module. Existing entries decode unchanged with agnostic false.

3. Enrichment becomes demand-driven end to end. The provider-dependent agent fields are exactly `providers`, `provider_env`, and `models`; every other `agentFieldSet` key is non-provider. Provider data is computed only when the requested output needs a provider-dependent field. A request limited to non-provider fields must never trigger a models.dev fetch, for any agent.

4. For an agnostic agent, the provider set used for enrichment and model resolution comes from the caller, not from the catalog. Thread it through a new library option for `Detect`/`DetectOne`, and through an explicit provider-set argument on `ResolveModel` (catalog `provider` for home-provider agents; caller set for agnostic; `ErrProvidersRequired` when agnostic and the set is empty). For a home-provider agent, the provider set continues to come from the catalog `provider` list everywhere.

5. A new sentinel error (e.g. `ErrProvidersRequired`) is returned when provider data is demanded for a single agnostic agent and no caller providers were supplied (`DetectOne`, and single-agent CLI paths built on it). It maps to a non-zero CLI exit and a message that names the agent and how to supply providers. Multi-agent `Detect` must not fail for that case: skip enrichment for the agnostic agent (Providers empty, ProviderEnv and Models nil) and continue, so `list` can render a marker instead of a count.

6. Caller-supplied provider ids are validated against models.dev. An id that is not a models.dev provider is an error, not a silent drop, when models.dev is reachable. That error is a caller/usage fault, never a catalog data error (exit 78). The get coverage rollup's "none present → catalog data error" path applies only to home-provider agents, whose provider list comes from the catalog.

7. The `get` and `models` commands accept a `--provider` argument (repeatable and/or comma-separated models.dev ids). On a home-provider agent, supplying `--provider` is rejected with a usage error — the catalog is authoritative. The `list` command accepts `--provider` and applies it only to agnostic agents' counts; home-provider agents use their catalog providers and `--provider` is not rejected there.

8. Command behaviour matches this matrix. Not-installed (Found false) always exits 3 with catalogued outside facts first, the same contract as every other agent; soft path exit 0 never applies when Found is false. The existing `get` flags `--models` and `--no-models` keep the same meaning on agnostic agents as on home-provider agents: `--models` forces per-model enrichment on; `--no-models` opts out of Models while still allowing provider-env when providers are supplied; they remain mutually exclusive.

   - `get <agnostic>` soft path (no field filter, no `--provider`, and not `--models`), Found: returns all non-provider fields, omits `providers`, `provider_env`, and `models`, and adds a `warnings` entry stating the agent is provider-agnostic and how to enrich. Exit 0. `--no-models` alone does not change this path (models are already omitted).
   - `get <agnostic>` soft-path conditions, not Found: exit 3 with the not-installed error; payload matches soft path (outside facts, omit the three provider fields, agnostic warning).
   - `get <agnostic> --models` with no `--provider`: `--models` demands the `models` field — `ErrProvidersRequired`, non-zero exit (not soft path).
   - `get <agnostic> --fields <non-provider only>`: returns those fields, no models.dev fetch, no warning (exit 3 when not Found).
   - `get <agnostic> --fields models` (or `providers`, or `provider_env`) with no `--provider`: `ErrProvidersRequired`, non-zero exit (provider demand is independent of install state).
   - `get <agnostic> --provider <ids>`, Found: enrich against exactly those providers. Whether Models is filled follows the same enrich flag as home-provider get (config `enrich_models`, overridden by `--models` / `--no-models`): provider-env is filled whenever a models.dev client is attached; `--no-models` keeps provider-env and omits Models.
   - `models <agnostic>` with no `--provider`: `ErrProvidersRequired`, non-zero exit.
   - `models <agnostic> --provider <ids>`: list models for exactly those providers.
   - `models <agnostic> <query> --provider <ids>`: fuzzy-resolve the query within those providers via `ResolveModel`.
   - `get <home-provider>` (no filter, no `--provider`): unchanged from today — outside facts plus models from the catalog provider list.
   - `get <home-provider> --provider <ids>`: rejected with a usage error.
   - `models <home-provider> --provider <ids>`: rejected with a usage error.
   - `list`: model count for home-provider agents; for agnostic agents, a real count when `--provider` is given, otherwise `-` in the models count column. `list` never hard-fails because an agnostic agent is present.

9. opencode is added to `catalog/agents.cue` as the first agnostic agent, and validates under `cue vet`.

10. The design doc (`docs/agentdex-design.md`) provider section is amended to describe agnostic agents and the caller-supplied-provider contract, replacing the unconditional "at least one required" statement.

## Constraints

- Pure Go, `CGO_ENABLED=0`, Go 1.25. CUE module language pinned to `v0.16.0`; use no feature beyond that pin.
- The catalog and Go changes must be backward-compatible: existing entries and existing home-provider behaviour are unchanged, and the change is additive (agnostic defaults false). No breaking bump of the catalog `@v1` major.
- Preserve the boundary. Determine agnostic providers only from the caller's argument and validate them against models.dev. Never read opencode's (or any agent's) auth store or internal config to infer providers.
- Keep nondeterministic inputs at the boundary. Provider-id validation and models.dev access stay behind the existing client seam; core logic remains testable from inputs.
- Follow the catalog-addition workflow and markdown rules in AGENTS.md, including the `.agents/` path-priority rule.

## Implementation Plan

1. Schema. Add `agnostic: bool | *false` to `#KnownAgent` and gate `provider` on it so that a non-agnostic entry requires `provider` and an agnostic entry forbids it. Validate both directions with `cue vet`: a good agnostic entry passes, and an agnostic-plus-provider entry (and a non-agnostic-without-provider entry) fail.

2. Go decode type. Add the agnostic field to `KnownAgent` and populate it in `internal/catalog/decode.go`. Existing entries continue to decode with agnostic false and their provider list intact.

3. Library option, enrichment, and ResolveModel. Add an option that carries caller-supplied provider ids (e.g. `WithProviders`). In the engine, choose the enrichment provider set per agent: catalog `provider` for non-agnostic agents, caller providers for agnostic agents. When a models.dev client is attached for an agnostic agent with no caller providers: `DetectOne` returns `ErrProvidersRequired`; multi-agent `Detect` skips enrichment for that agent only (Providers empty, ProviderEnv and Models nil) and continues. Extend `ResolveModel` to accept a caller provider set with the same rule (catalog for home-provider, caller set for agnostic, `ErrProvidersRequired` when agnostic and empty). Validate caller provider ids against models.dev, erroring on an unknown id when models.dev is reachable and degrading as today when it is not. Keep the existing degrade-to-nil behaviour for transient models.dev gaps.

4. Sentinel error. Add `ErrProvidersRequired` to `errors.go` with godoc describing when it is returned. Map it to the appropriate non-zero CLI exit code.

5. CLI flag and field-selection seam. Add `--provider` to `get`, `models`, and `list`. Make the CLI attach the models.dev client only when the requested output needs a provider-dependent field (`providers`, `provider_env`, `models`), so a non-provider `--fields` request skips models.dev entirely. On `get`/`models`, reject `--provider` on a home-provider agent with a usage error. For agnostic agents without `--provider`: an explicit `--fields` that names any provider-dependent field, or an explicit `--models`, is `ErrProvidersRequired` (regardless of install state); soft path applies only when unfiltered, no `--provider`, and not `--models` — outside facts only, omit the three provider-dependent fields, agnostic warning, no models client on `DetectOne`. Soft path exit 0 applies only when Found; when Found is false the same payload is reported with the not-installed error at exit 3. For `models`, pass the resolved provider set into `modelsList` and into `ResolveModel` for the query path — do not read catalog `Provider` alone for an agnostic agent. For `get` on an agnostic agent with `--provider`: do not run the catalog-data-error rollup branches; enrich through `DetectOne` with `WithProviders`/`WithModels` (and `EnrichModels()` only when the enrich flag says so — config/`--models`/`--no-models`, same as home-provider get) and surface library errors (unknown provider, `ErrProvidersRequired`) directly. Home-provider agents keep today's coverage rollup unchanged.

6. list marker. Render the models count as `-` for an agnostic agent without `--provider`, and a real count when `--provider` is supplied. Rely on `Detect`'s soft-skip so a mixed catalog never fails the listing.

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

9. Tests. Cover: schema validation both directions (agnostic ok, agnostic-plus-provider fails, non-agnostic-without-provider fails) using real CUE validation; decode of an agnostic entry; the enrichment branch (caller providers for agnostic, catalog for home-provider, `ErrProvidersRequired` on demand-without-providers, unknown-id rejection); `ResolveModel` with caller providers for agnostic agents; and the full CLI matrix from requirement 8, including `models` list and query with `--provider`, the `list` marker, the `get` soft-path warning when Found, not-installed exit 3 with soft-path payload when not Found, `get --models` without `--provider` as `ErrProvidersRequired`, and `get --provider … --no-models` keeping provider-env while omitting Models. Follow the repo's real-behaviour test conventions.

10. Republish. After the catalog module validates and `cue mod tidy` is clean, publish a new `@v1` version of the catalog to the CUE Central Registry so installs resolve opencode and the `agnostic` schema within the cache TTL.

## Implementation Guidance

- The demand-driven seam already exists: `enrich` is a no-op without a models.dev client. Lean on it. The CLI decision "did the caller ask for provider data" reduces to "should I attach the models.dev client," driven by whether the requested field set intersects `{providers, provider_env, models}` — treating an unfiltered get as requesting those fields, except on the agnostic soft path — and by explicit `--models` (which demands `models`). Prefer wiring that field-set check to client attachment over adding a parallel gate inside the engine.
- Keep the reject-on-home-provider rule at the CLI layer for `get`/`models`. At the library layer a `Detect` run legitimately mixes agnostic and home-provider agents while caller providers are set, so the library should apply caller providers to agnostic agents and ignore them for home-provider agents rather than erroring. When caller providers are absent, `Detect` soft-skips enrichment for agnostic agents; only `DetectOne` returns `ErrProvidersRequired`.
- `models` is not a Detect path: resolve the provider set once (catalog vs `--provider`), hand it to `modelsList` and to `ResolveModel`. Prefer extending `ResolveModel`'s provider source over rewriting models through `DetectOne`.
- Split get's coverage path by agent kind. Home-provider: keep `getCoverage` rollup (including "none present → catalog data error, exit 78"). Agnostic: no catalog-fault rollup; caller providers are validated in the library, and a bad or missing set is never reported as a catalog data error. With `--provider`, reuse the same enrich flag resolution home-provider get uses (`enrich_models` config, `--models` / `--no-models`); do not hard-code always-on Models.
- Provider-id validation depends on models.dev reachability. When models.dev is unreachable, enrichment already degrades to nil; in that state an unknown-id error is not possible, so treat validation as part of the same reachable-only path rather than a separate hard gate.
- opencode's binary may not be installed on a given machine; that is a normal not-found result, exactly like any other catalogued agent. The entry describes the agent regardless of local presence. Not-installed (exit 3) always outranks soft path (exit 0): soft path is only the success-path shape for an unfiltered agnostic get without `--provider` and without `--models` when Found is true.

## Acceptance Criteria

- `cue vet ./...` from `catalog/` passes with the opencode entry present, and `cue mod tidy` leaves the module clean.
- An agnostic entry that also declares `provider`, and a non-agnostic entry that omits `provider`, both fail `cue vet`.
- `get opencode --fields skills_dir` returns the skills dir with no models.dev access and no warning.
- `get opencode` with no `--provider` and no `--models`, when the binary is found, returns the outside facts, omits `providers`, `provider_env`, and `models`, and carries a provider-agnostic warning, exiting 0.
- `get opencode` with no `--provider` and no `--models`, when the binary is not found, exits 3 with the not-installed error, still omits the three provider fields, and carries the provider-agnostic warning.
- `get opencode --fields models`, `get opencode --models`, and `models opencode`, all without `--provider`, fail with `ErrProvidersRequired` and a non-zero exit.
- `models opencode --provider anthropic,openai` lists models for exactly those providers; `models opencode sonnet --provider anthropic` resolves within that set.
- `get opencode --provider anthropic,openai` enriches against exactly those providers (Models filled under the default enrich flag).
- `get opencode --provider anthropic --no-models` reports provider-env for that provider and omits Models.
- `get opencode --provider <unknown-id>` fails with an unknown-provider error when models.dev is reachable, and the failure is not labelled a catalog data error (not exit 78).
- `get claude-code` output is unchanged from before this project, and `get claude-code --provider openai` is rejected as a usage error.
- `list` shows a count for home-provider agents and `-` for opencode without `--provider`, and does not fail with opencode in the catalog.
