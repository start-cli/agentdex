# Models opt-in on get

## Goal

Stop unfiltered `get` from filling the full models.dev model list by default. Models on `get` become an explicit ask (`--models` or a `--fields` selection that demands `models`). Provider-env stays default-on for unfiltered `get`. Remove the `enrich_models` config knob and the `--no-models` flag so the surface matches the rule: you want models, ask for them.

## Scope

In scope:

- CLI `get` default: no `EnrichModels()` unless the caller opts in.
- Removal of config `enrich_models` (schema, loader, tests, design, any user-facing mention).
- Removal of `get --no-models` and the mutual-exclusion path with `--models`.
- Retention of `get --models` as the opt-in flag; `--fields` selections that include `models` continue to demand Models fill (OR rule in requirements).
- Provider fallthrough on `get` (query matches no catalog agent but matches a models.dev provider): model list only when `--models` is set; without it, report provider identity and the not-catalogued note only.
- Design doc, README, and project `02-provider-agnostic-agents.md` wording that still assumes default-on Models, `enrich_models`, or `--no-models`.
- Tests and help text for the new contract.

Out of scope:

- Library API shape (`WithModels`, `EnrichModels()`). The library is already opt-in; this project changes CLI policy and config only.
- `list` model-count enrichment. MODELS column stays always on; do not add `--models` to list.
- `models` command behaviour. It remains the dedicated model surface.
- Provider-agnostic agents, `--provider`, or other work owned by `02-provider-agnostic-agents.md` except amending that document so it does not reintroduce default-on Models, `enrich_models`, or `--no-models`.
- Changing provider-env default for unfiltered `get` (stays on).
- Removing provider fallthrough entirely; only the models dump on that path is gated.

## Current State

The library already separates the two layers:

- `WithModels(client)` attaches models.dev and enables `ProviderEnv`.
- `EnrichModels()` additionally fills `Agent.Models`. Without it, Models stays nil.

CLI policy today undoes that opt-in for `get`:

- `internal/cli/get.go` starts from `cfg.EnrichModels` (default true via config), applies `--no-models` / `--models`, then passes `EnrichModels()` into detection when the resulting flag is true.
- Unfiltered `get` therefore fills Models by default; `--no-models` is the escape hatch.
- `--fields` is presentation-only today: it filters the reported record after detection and never participates in the enrich boolean. Wiring “selection includes `models`” into Models demand (requirement 2) is new behaviour, not a default flip.
- Config schema (`internal/config/schema.cue`) has `enrich_models: bool | *true`. `Config.EnrichModels` is decoded in `internal/config/config.go`.
- Tests cover the old contract: `internal/cli/get_test.go` (`--no-models`), `internal/cli/cli_test.go` (`enrich_models: false` and `--models` override), `internal/config/config_test.go` (default true / explicit false).

`list` (`internal/cli/list.go`) always attaches the client with `EnrichModels()` for the MODELS count column. That is intentional and stays.

Design authority: `docs/agentdex-design.md` (provider-env section, CLI get behaviour, configuration schema and `enrich_models` prose). README CLI paragraph still says get enriches models by default and documents `--no-models`.

When the query matches no catalog agent, `get` fallthrough (`getFallthrough` in `internal/cli/get.go`) classifies the string against models.dev providers. A unique provider match today always embeds that provider's full model list at exit 3. That path does not use `EnrichModels`; it builds the list directly from the provider map.

Sibling project `02-provider-agnostic-agents.md` is not implemented yet. Several of its requirements and acceptance criteria still assume unfiltered get may demand models via `enrich_models` / `--no-models` (for example third-gate wording, agnostic soft path notes, and `get --provider … --no-models`). This project amends 02 so both specs share one Models policy before either is implemented out of order.

## Requirements

1. Unfiltered `get <agent>` (binary found, no Models demand under requirement 2) does not pass `EnrichModels()`. The agent payload still includes outside facts and, when models.dev is attached for the default path, `provider_env`. Leave `Agent.Models` nil when fill did not run — do not assign an empty slice to mean "not enriched". Presentation follows existing `agentReportRecord` rules: omit the `models` record field when nil; text detail skips the Models section. An empty slice is only valid when enrichment ran and returned zero models.

2. Models fill on `get` is demand-driven by an OR rule. Pass `EnrichModels()` when either:
   - `--models` is set, or
   - `--fields` is non-empty and the selection includes `models`.
   Empty `--fields` means unfiltered: only `--models` turns fill on. When fill runs, use the same coverage and degrade rules get already uses for enriched models. When fill does not run, leave `Agent.Models` nil (requirement 1).

3. `--fields` remains a presentation filter on the reported record after detection/enrichment (existing CLI pattern). A run that fills Models because of `--models` but selects fields that omit `models` still runs `EnrichModels()`; the models data is simply not selected into the output. Selections limited to non-models.dev fields continue to skip models.dev attachment when that is already how get behaves for pure non-provider field filters; this project does not redesign field-selection gates beyond the Models demand rule above.

4. Provider-env on unfiltered `get` remains default-on: attach the models.dev client for provider-env without requiring `--models`. `--models` (or field demand for `models`) adds Models on top; it does not become the only way to attach the client.

5. Remove `enrich_models` from the config schema, `Config` / raw decode types, load mapping, design configuration section, and all tests that set or assert it. A user config that still contains `enrich_models` fails closed-schema validation as an unknown field (same as any other removed field). Do not special-case a deprecation shim.

6. Remove the `--no-models` flag from `get`, including mutual-exclusion handling with `--models`, help/Long text, and tests. There is no config or flag that opts out of Models once demanded; the default is already off.

7. Keep `--models` on `get` as the explicit opt-in half of the OR demand rule (requirement 2). Update Short/Long/flag help so the default is described as Models off and `--models` as opt-in, not "force on over a true default."

8. `list` continues to enrich model counts unconditionally (no list `--models` flag; no dependence on removed config). `models` command behaviour is unchanged.

9. Provider fallthrough on `get` (no catalog agent, unique models.dev provider match, exit 3, labelled as provider data not an agent): include the provider's model list only when `--models` is set. Without `--models`, still exit 3 with the not-catalogued / provider-match note and report provider identity (id and name as today); omit the models array and the models table. Classification against models.dev (to decide provider match vs unknown) still runs without `--models` — only the models dump is gated. Unknown (no agent, no provider) stays exit 2. Transient when models.dev is unreachable for classification stays as today.

10. Amend `docs/agentdex-design.md` so provider-env, CLI get behaviour, configuration, and the catalog/models.dev coverage table no longer describe default-on Models, `--no-models`, or `enrich_models`. State the contract: get attaches for provider-env by default for catalog agents; Models only when `--models` or field demand includes `models`; fallthrough models only with `--models`; list counts stay unconditional; models command inherent.

11. Update README CLI prose to match requirement 10.

12. Amend `02-provider-agnostic-agents.md` so it shares this project's Models policy (OR demand for catalog-agent fill; no `enrich_models`; no `--no-models`). Do not leave "see 03" cross-references; fold the policy into 02 so 02 remains implementable alone. Minimum amend map (line numbers are approximate; match on content if the file drifts):

    - Out of scope bullet on home-provider unfiltered enrichment: keep coverage rollup and list counts as baseline; drop "model listing" as something unfiltered get must retain. Unfiltered get Models fill is opt-in under the OR rule (this project's requirement 2), not a preserved default.
    - Current State CLI sentence that says get enriches models by default: rewrite to Models opt-in (`--models` or field demand) with provider-env still default-on for unfiltered catalog-agent get.
    - Requirement 3 third gate: replace "unfiltered get demands models subject to config `enrich_models` and `--models` / `--no-models`" with the OR rule from this project's requirement 2 (unfiltered get does not demand Models; `--models` or `--fields` including `models` does).
    - Requirement 8 intro: drop `--no-models` and mutual exclusion; describe `--models` as opt-in only.
    - Soft-path bullet that mentions `--no-models` alone: remove that clause (path already omits models).
    - Agnostic `--provider` Found bullet: Models fill follows the OR rule; bare `--provider` without Models demand keeps provider-env and omits Models (replaces `--no-models` examples).
    - Requirement 8 home-provider unfiltered matrix row (`get <home-provider>` unchanged / plus models): rewrite so unfiltered home-provider get keeps outside facts, provider-env, and coverage rollup, and fills Models only under the OR rule — not "plus models" by default.
    - Constraints "Home-provider unfiltered/default enrichment behaviour stays as today": rewrite so "today" means post-this-project baseline — provider-env and coverage when the models.dev path fires; Models only under the OR rule; demand-driven field selection (02 requirement 3) still applies.
    - Implementation plan step 5 (CLI flag seam): same third-gate / unfiltered wording as requirement 3.
    - Implementation plan step 9 (tests): replace `get --provider … --no-models` with bare `--provider` (provider-env, no Models) and a separate `--provider --models` (or `--fields models`) case.
    - Implementation guidance bullets that say unfiltered get demands models via config/flags: align with the OR rule and default-off unfiltered get.
    - Acceptance criterion "Models filled under the default enrich flag": require explicit `--models` or field demand for Models on agnostic `--provider` get.
    - Acceptance criterion `get opencode --provider anthropic --no-models`: rewrite to bare `--provider` (provider-env, omit Models).
    - Acceptance criterion `get claude-code` output unchanged: drop or rewrite. After this project, unfiltered home-provider get no longer fills Models by default; 02 must not claim pre-03 byte-identical get output. Keep the intent that home-provider unfiltered get still runs coverage when the models.dev/provider-env path fires, and still rejects `--provider` as usage.
    - Sweep: after the map edits, grep 02 for `enrich_models`, `--no-models`, "enriches models by default", and "plus models"; any remaining hit must match the OR rule and default-off unfiltered Models, or be rewritten.

## Constraints

- Pure Go, `CGO_ENABLED=0`, Go 1.25. No new dependencies.
- Library public API for `WithModels` / `EnrichModels` stays as today; do not invert library defaults to work around CLI.
- Closed config schema: removing `enrich_models` means unknown-field failure for leftover keys — accepted.
- Preserve get degrade behaviour when models.dev is unreachable (warnings, exit 0 for detection success) for both provider-env-only and Models-enriched paths.
- Follow AGENTS.md markdown rules for agent-facing docs (no bold/italic, no horizontal rules, heading depth limit).
- Scoped commits if the implementer commits: scope named for the subsystem (e.g. `cli`, `config`, `docs`).

## Implementation Plan

1. Config. Remove `enrich_models` from `internal/config/schema.cue`, from `Config` and raw wire types, from load mapping, and from schema comments that list it among defaulted fields. Update `config_test.go` so defaults and decode cases no longer mention it. Confirm a fixture or one-off config with `enrich_models` fails validation under the closed schema.

2. CLI get. In `internal/cli/get.go`, delete `noModels` and `--no-models`. Stop reading `cfg.EnrichModels`. Compute catalog-agent Models demand with the OR rule (requirement 2) — the fields half is new wiring: today fields never set enrich. Pass `EnrichModels()` when that demand is true. Update Long text and flag help for `--models` to opt-in language. Remove mutual-exclusion error. For fallthrough, gate the models dump on `--models` only (requirement 9), not on `--fields`.

3. Views and comments. Clear any CLI comments that still describe `--no-models` (e.g. `internal/cli/views.go`).

4. Tests. Replace `--no-models` and `enrich_models` tests with: bare get omits Models and keeps provider-env; `--models` fills Models; `--fields` including `models` fills Models; `--fields` without `models` and without `--models` does not fill Models; `--models --fields skills_dir` (or equivalent) still runs fill even though output omits models. Add fallthrough cases: unique provider match without `--models` has no models payload; with `--models` includes models. Drop config tests that only existed for `enrich_models`.

5. Design and README. Apply requirements 10 and 11.

6. Amend 02. Apply requirement 12's amend map so 02 and 03 do not conflict. Prefer editing 02 in the same change set as this project's docs so a later 02 implementer never reintroduces the removed surface.

7. Finalisation. `gofmt`, `go build ./...`, `go vet ./...`, `golangci-lint run`, `go test ./...` from the repository root.

## Implementation Guidance

- The principled rule is explicit demand, not a three-way precedence ladder. Do not reintroduce a config default that turns Models on silently.
- Prefer computing "should EnrichModels" once at the get command boundary with the OR rule (`--models` or fields contain `models`), then composing `WithModels` / `EnrichModels()` the same way get already composes enrichment for the coverage path. Do not treat field selection as a veto over `--models`.
- Fallthrough is a separate surface from catalog-agent enrichment: it does not use `EnrichModels`, but the same user rule applies — no models dump without `--models`. Keep classifying the query without `--models`; only omit the list/table.
- list and models are out of scope: do not "simplify" them by threading a shared enrich default through config again.
- When amending 02, fold the new Models policy into existing requirements rather than leaving "see 03" cross-references that a fresh implementer of 02 might miss. 02 remains implementable alone after the amend, with Models opt-in as the baseline.

## Acceptance Criteria

- `agentdex get <home-provider-agent>` with binary found and no Models demand reports provider-env (when models.dev is reachable); JSON omits `models` (nil, not `[]`); text has no Models section.
- `agentdex get <home-provider-agent> --models` with binary found includes a populated models list under the same conditions as today's enriched get.
- `agentdex get <agent> --fields models` demands Models fill; `agentdex get <agent> --fields skills_dir` without `--models` does not fill Models; `agentdex get <agent> --models --fields skills_dir` still fills Models internally (output may omit the models field).
- `get` has no `--no-models` flag (help and invocation reject or omit it as unknown).
- Config schema and loader have no `enrich_models`; loading a config that sets `enrich_models` fails as an unknown field.
- `agentdex list` still shows MODELS counts without a new flag.
- `agentdex get <models.dev-provider-id>` (not a catalog agent) without `--models` exits 3 with provider identity and the not-catalogued note, and does not include a models list; the same with `--models` includes the models list as today.
- `docs/agentdex-design.md` and README describe Models on get as opt-in via `--models` / field demand, fallthrough models gated the same way, provider-env default-on for catalog agents, no `enrich_models`, no `--no-models`.
- `02-provider-agnostic-agents.md` no longer requires or documents `enrich_models` or `--no-models`; its third gate, matrix, plan, guidance, and acceptance criteria match the OR demand rule and default-off unfiltered Models (requirement 12 map complete).
