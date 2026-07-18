# noun-verb CLI restructure

Source: design discussion, 2026-07-18
Category: CLI surface
Location: internal/cli (command tree), docs/agentdex-design.md

## Goal

Restructure the agentdex CLI around its three data entities — agents, providers, and models — so every entity is reached the same way: a noun group with two verbs, `list` for browsing and `get` for exact retrieval. This replaces today's uneven mix (`list`, `get <agent>`, `models <agent> [query]`, `providers [filter]`), where each command invented its own positional semantics, with one predictable grammar the user learns once and applies everywhere.

## Scope

In scope:

- A command tree of three noun groups: `agents` (alias `agent`), `providers` (alias `provider`), `models` (alias `model`). Each group requires an explicit `list` or `get` verb.
- Two verb rules applied uniformly. `list [filter]` is a browse: a case-insensitive substring narrowing over id and name where a zero-match is an empty listing at exit 0. `get <id>` is an exact fetch by the entity's canonical id where a miss is not-found at exit 3.
- Definition of a canonical id per entity, including the composite `provider-id/model-id` id for a model.
- A new `providers get <id>` provider-detail view, which does not exist today.
- `models list` as a global, filterable, optionally scoped browse (today `models` requires an agent).
- `models get <provider-id/model-id>` as an exact single-model detail lookup.
- Retirement of the CLI's fuzzy none/one/many selector machinery and the `get` provider fallthrough.
- A clean break: the old top-level `list`, `get`, `models`, and `providers` commands are removed with no back-compat aliases.
- Updates to `docs/agentdex-design.md`: command list, selector-matching section, per-command behaviour notes, and the design-decisions rollup.

Out of scope:

- Any change to the root `agentdex` library, the `modelsdev` client, or their public APIs. This is a CLI-layer restructure; detection, resolution, the merge, caching, the envelope, the exit taxonomy, the `fieldSet`/`record` machinery, and the tui/render layer are reused unchanged.
- Any change to the CUE agent catalog or its schema.
- New dependencies.
- The `skills` command. It is a provisional, separate project (design doc, Phasing and separate projects) and is not built. This restructure neither adds nor removes it. If it is built later it should follow the same grammar (an agent-scoped surface), but that is not this project's concern.
- New enrichment behaviour. The provider-env read, models.dev enrichment, the coverage rollup, and agnostic-agent handling are preserved as they are, only relocated under the new verbs.

## Current State

The CLI is a thin cobra layer over the library and the `modelsdev` client, in `internal/cli`. The command tree is built in `root.go` (`NewRootCommand`), where every command is registered flat under one `groupCore` help group and global flags are bound. `preRun` loads config into `app.cfg` and settles colour and logging. Each command closes over the shared `app` for output (`a.ok`, `a.fail`, `a.failData`, `a.usage`), config (`a.requireConfig`), and the logger.

Today's data commands:

- `list.go` (`newListCmd`): lists detected agents as a table, enriched with a models.dev model count, `--all` to include catalogued-but-not-found agents, `--provider` for agnostic model counts, `--verbose` column widening. No positional filter.
- `get.go` (`newGetCmd`): detail for one agent, selected by the fuzzy none/one/many rule (`matchAgent`). Holds a large amount of behaviour: provider-env reporting, opt-in `--models` enrichment, the agnostic soft-path and `--provider` enrichment (`getAgnosticSoftPath`, `getAgnosticEnrich`), the models.dev coverage rollup (`rollup`, `getCoverage`), catalogued-but-not-installed reporting, and the provider fallthrough (`getFallthrough`) that reclassifies a non-agent query as a models.dev provider. It also houses shared helpers used across commands: `registerFieldsFlag`, `addFieldsHelpSection`, `addHelpSection`, `reportAgent`, `renderAgentDetail`, `renderAgentDetailFields`, `agentReportRecord`, `flattenProviders`, `jsonRecords`.
- `models.go` (`newModelsCmd`): requires an agent argument; with a second `[query]` it resolves a single model through `Catalog.ResolveModel` (fuzzy none/one/many), otherwise lists the agent's provider models. Agnostic agents require `--provider`. `modelsCode` classifies failures.
- `providers.go` (`newProvidersCmd`): lists models.dev providers with an optional substring filter (browse, not selector), an ENV column that folds presence into a `(set)` suffix, a structured `present` map, and a model count. Reads env presence at the boundary via `envPresence(env, os.LookupEnv)`. `providersCode` classifies failures. There is no provider-detail view.

Supporting code:

- `selectors.go`: `catalogIDs`, `matchAgent`, `matchProvider` — the fuzzy selector helpers over `internal/match`.
- `internal/match`: the none/one/many `Match` rule (exact id, then case-insensitive name, then unique substring/prefix).
- `views.go`: the declared field sets (`agentFieldSet`, `modelFieldSet`, `providerFieldSet`) and record builders (`agentRecord`, `modelRecord`, `providerRecord`), plus provider-env rendering (`providerEnvCell` folded, `styledProviderEnv` symmetric `(set)/(unset)`), `withModels`, `sortModelsNewest`, `newerModel`.
- `render.go`: `renderTable`, `renderDetail`, `renderFields`, `tabulate`, `sortedKeys`.
- `exit.go`: the exit taxonomy (`codeOK` 0, `codeFailure` 1, `codeUsage` 2, `codeNotFound` 3, `codePermission` 4, `codeConflict` 5, `codeTransient` 75, `codeConfig` 78) and `codeFor`.
- `envelope.go`: the `status`/`data`/`error`/`warnings` envelope, `fieldSet`, `record`.

Library facts the CLI composes (unchanged):

- `agentdex.LoadCatalog(ctx, opts...) (*Catalog, stale bool, err error)`.
- `agentdex.Detect(ctx, opts...) ([]Agent, error)` and `agentdex.DetectOne(ctx, id, opts...) (*Agent, bool, error)`; `DetectOne` returns `ErrAgentUnknown` for an id absent from the catalog.
- Options: `WithCatalog`, `WithProviders`, `WithModels`/`EnrichModels`, `WithSkipVersion`, `IncludeMissing`, plus `ValidateCallerProviders`.
- `Catalog.ResolveModel(ctx, agentID, query, client, providers)` — the agent-scoped fuzzy model resolver. Public API; kept, but the CLI no longer routes model retrieval through its fuzzy query path.
- `modelsdev.Client.Catalog(ctx)`, `Client.Provider(ctx, id) (Provider, found, err)`; a `Provider` carries `ID`, `Name`, `Doc`, `NPM`, `API`, `Env []string`, `Models map[string]Model`.

Relevant design-doc sections to amend: the command list (around line 706), the selector-matching section (around 719), the per-command behaviour notes (around 744), and the design-decisions rollup bullet on selector matching (around 1093).

The repository is pre-1.0 and under active development, so a clean break with no aliases is acceptable.

## Requirements

1. Command tree. Register three noun groups — `agents` (alias `agent`), `providers` (alias `provider`), `models` (alias `model`) — each carrying `list` and `get` subcommands. The singular alias is a pure synonym for the plural; it carries no meaning and must not select a different operation. The utility commands `refresh`, `version`, and `completion` stay top-level and unchanged. Remove the old top-level `list`, `get`, `models`, and `providers` commands entirely, with no hidden aliases.

2. Bare noun. Invoking a noun group with no verb (`agentdex agents`) is a usage fault: exit 2, routed through the shared usage path so it carries the JSON envelope under `--json`, and it prints the group's help so the available verbs are discoverable.

3. The `list` rule. `<noun> list [filter]` is a browse. With no filter it lists the whole relevant set. With a filter it narrows to entries whose id or name contains the filter as a case-insensitive substring. Several matches list all of them; zero matches is an empty listing at exit 0. It never reports ambiguity and never treats a zero-match as not-found. It does not use `internal/match`.

4. The `get` rule. `<noun> get <id>` is an exact fetch by the entity's canonical id. An id that names no entity is not-found: exit 3. There is no fuzzy matching, no substring or prefix resolution, and no candidate list. `get` never routes through `internal/match`.

5. Canonical ids. An agent's canonical id is its catalog kebab-case key (`claude-code`). A provider's canonical id is its models.dev provider id (`anthropic`). A model's canonical id is the composite `provider-id/model-id` (`anthropic/claude-opus-4-5`) — the composite the code already computes. `models get` takes this composite; a value with no `/` separator is a usage error (exit 2) that directs the user to `models list`.

6. `agents list`. Preserve the current `list` behaviour: detected agents by default, models.dev count enrichment with the reachable-cache degrade and the unreachable-and-uncached warning, the `ErrModelsSchema` re-detect-without-enrichment fallback, `--all` to include catalogued-but-not-found agents, `--provider` applied only to agnostic rows, `--verbose` column widening, and the `--fields`/`--json` contract. Add the positional substring filter from Requirement 3 over agent id and name.

7. `agents get`. Exact fetch by catalog id per Requirement 4. Preserve all of get's substance: provider-env reporting by default, opt-in models fill (`--models` or a `--fields` selection naming `models`), the agnostic soft-path and `--provider` enrichment, the models.dev coverage rollup, and catalogued-but-not-installed reporting (exit 3 with the agent payload). Remove the provider fallthrough: a query that is not a catalog id is simply not-found (exit 3), never reclassified as a models.dev provider. Provider discovery now lives in `providers list` and `providers get`.

8. `providers list`. The current `providers` command behaviour unchanged (substring filter, the presence-folded ENV column, the structured `present` map, the model count over the array-typed `models` field, the boundary env read, the transient/config classification), relocated under `providers list`.

9. `providers get`. A new provider-detail view, exact fetch by provider id per Requirement 4, resolved against models.dev. It renders a detail view structurally consistent with `agents get`: the provider's facts (`id`, `name`, `doc`, `npm`, `api`), its env presence as a provider-env section using the symmetric `(set)/(unset)` markers `get` uses (not the folded browse cell), and its models as a count by default with the full model table filled under `--models`. An unknown provider id is not-found (exit 3). A models.dev outage with no cache is transient (exit 75); gross structural drift is config (exit 78). Env presence is read at the boundary through the injectable lookup, never the variable values.

10. `models list`. A browse over models. With no scope it lists models across all providers; the positional filter narrows by case-insensitive substring over model id and name. `--provider <id>` scopes the listing to one provider; `--agent <id>` scopes it to the named agent's providers. Scope composes with the filter. Preserve the existing agnostic and home-provider rules: an agnostic agent named by `--agent` requires `--provider`, a home-provider agent rejects `--provider` as a usage error, and caller-supplied provider ids are validated against a reachable models.dev. Preserve newest-first ordering and the price footer. A models.dev outage with no cache is transient; structural drift is config.

11. `models get`. Exact single-model fetch by the composite id per Requirement 5: resolve the provider component, fetch that provider, look up the model component, and render the existing single-model detail (the `modelRecord` fields, the price footer, `canonical_id` when the model has a real models.dev agnostic id). No fuzzy matching. An unknown provider component or an absent model component is not-found (exit 3). Failures classify through the models exit taxonomy.

12. Retire fuzzy selection from the CLI. Remove `getFallthrough`, `matchAgent`, `matchProvider`, and the none/one/many usage from the command paths. If `internal/match` has no remaining consumer after this, remove it; if it is used elsewhere, leave it. Do not modify the public library `ResolveModel`.

13. Tests. Rewrite every command-invocation test to the new grammar and add coverage for the new surfaces: the bare-noun usage fault, exact-id not-found on each noun, `providers get` (known, unknown, `--models` fill), `models get` (composite hit, missing-slash usage error, unknown composite), `models list` global and `--provider`/`--agent` scoped with a filter, and confirmation that the removed top-level commands now fail as unknown commands. Follow the existing harness and prefer the frozen models.dev testdata over the network.

14. Documentation. Update `docs/agentdex-design.md`: replace the command list with the noun-verb tree; rewrite the selector-matching section around the two rules (`list` is a browse substring narrowing, `get` is an exact canonical-id fetch) and retire the none/one/many fuzzy rule and its `providers`-exception framing, since browse-vs-exact now applies uniformly; rewrite the per-command behaviour notes; and amend the design-decisions rollup selector bullet to match. Follow the repo markdown rules.

## Constraints

- Go 1.25, pure Go, `CGO_ENABLED=0`. No new dependencies; build from the standard library, cobra, and the already-carried library, `modelsdev` client, and CLI machinery.
- The library is the primary artefact and the CLI is a thin layer. Add no behaviour to the root `agentdex` package. `models get`'s exact composite lookup is composed in the CLI from `modelsdev.Client.Provider` plus a map lookup; do not add a library API for it, and do not alter `ResolveModel`.
- Preserve the exit taxonomy, the JSON envelope contract, the `fieldSet`/`record` machinery, the tui/render layer, and the `a.ok`/`a.fail`/`a.failData`/`a.usage` helpers. Introduce no new output path.
- Keep every entity's `models` field array-typed across commands so `.data[].models` reads uniformly, rendered as a count in table cells.
- Keep nondeterministic inputs at the boundary: env presence through the injectable lookup (never the values), and the clock, filesystem, and network at the edges, so record building and filtering stay testable from inputs.
- Report only the outside of an agent. Do not read or interpret any agent's internal configuration.
- Clean break, pre-1.0: no back-compat aliases for the removed commands.
- The CUE catalog module is untouched; this project publishes no catalog version.
- Design-doc edits follow the repo markdown rules: no bold, italic, horizontal rules, or emojis; headings no deeper than `###`.

## Implementation Plan

1. Restructure `root.go`. Build the three noun groups with their singular aliases, register `list` and `get` under each, make a bare noun group return the shared usage fault while printing group help, and drop the old flat command registrations. Keep `refresh`, `version`, and `completion` top-level.

2. Shared verb helpers. Factor the two rules into reusable pieces: a browse-filter predicate over id and name (generalise the existing `providerMatches`) used by all three `list` commands, and a straightforward exact-lookup per noun for `get`. Keep `registerFieldsFlag`/`addFieldsHelpSection` shared as they are.

3. `agents list`. Adapt the current `list` path under the `agents` group and add the positional substring filter over id and name, applied after detection so the enrichment and `--all`/`--provider`/`--verbose` behaviour is unchanged.

4. `agents get`. Adapt the current `get` path under the `agents` group to exact-by-catalog-id selection (`DetectOne` returns `ErrAgentUnknown` for a miss → exit 3). Remove `getFallthrough`. Keep the agnostic soft-path, `--provider` enrichment, the coverage rollup, provider-env, opt-in `--models`, and catalogued-but-not-installed reporting.

5. `providers list`. Move the current `providers` command under the `providers` group unchanged.

6. `providers get`. Add the new provider-detail command: exact provider-id lookup against models.dev, rendering facts, the symmetric-marker provider-env section, and the model count with a `--models` full-table fill, structurally mirroring `agents get`. Reuse `providerRecord`/`styledProviderEnv`/the model-table render; read env presence at the boundary.

7. `models list`. Split the current `models` list path into a group-global browse: default across all providers, `--provider` and `--agent` scoping flags, the positional filter, and the existing agnostic/home-provider validation rules. Preserve ordering and the price footer.

8. `models get`. Add the exact composite lookup: split on `/`, fetch the provider, look up the model, render the existing single-model detail. A missing slash is a usage error; an unknown provider or model is not-found.

9. Retire fuzzy machinery. Remove `matchAgent`, `matchProvider`, `getFallthrough`, and the none/one/many usage from the CLI; remove `internal/match` only if nothing else references it. Leave `ResolveModel` alone.

10. Tests. Rewrite the command-invocation suites to the new grammar and add the new-surface coverage from Requirement 13.

11. Documentation. Apply the Requirement 14 edits to `docs/agentdex-design.md`.

12. Finalisation sweep. Run the repo sweep: `gofmt -l .`, `go build ./...`, `go vet ./...`, `golangci-lint run`, `go test ./...`.

## Implementation Guidance

- The whole restructure reduces to two rules; keep them visibly separate in the code. Discovery is the `list` filter (fuzzy substring, tolerant of zero and many); identity is the `get` id (exact, intolerant of a miss). Any temptation to make `get` "helpfully" fuzzy reintroduces the none/one/many machinery this project removes.
- `agents get` and `providers get` share a shape — facts, a provider-env section, and a models count/table. Prefer sharing the detail-render structure between them over duplicating it, but do not force a shared abstraction where the two records legitimately differ.
- `models list` with no scope spans every provider and is intentionally large, the same trade `providers list` already makes; the filter is the expected narrowing. Do not silently cap it. Keep the newest-first order and the price footer so the large listing stays readable and honest.
- The composite model id already exists in the code as `pid + "/" + key`, and `canonical_id` is set when that composite is present in the merged models.dev catalog. `models get` should produce the same record `modelsOne` produces today, only selected exactly rather than fuzzily.
- The `get` verb may keep `view`/`show` as aliases if it is cheap, matching the current `get` aliases, but that is optional polish, not a requirement.
- When rewriting the design doc's selector section, state the new invariant plainly: there is no longer a single selector rule with a `providers` exception; instead there are two verb rules that every noun shares. This removes the contradiction the previous framing carried, rather than adding another exception to it.

## Acceptance Criteria

- The six primary flows resolve as: `agentdex agents list` (all detected agents), `agentdex models list` (models across providers), `agentdex providers list` (all providers), `agentdex agents get <id>` (one agent), `agentdex models list --provider <id>` (a provider's models), `agentdex providers get <id>` (one provider).
- Each noun group accepts its singular alias as an exact synonym: `agentdex agent get <id>` behaves identically to `agentdex agents get <id>`.
- A bare noun group (`agentdex agents`) exits 2, prints the group help, and carries the JSON envelope under `--json`.
- The removed top-level commands fail as unknown commands: `agentdex get <id>`, `agentdex list`, `agentdex models <id>`, and `agentdex providers` no longer run.
- `<noun> list <filter>` narrows by case-insensitive substring over id and name; several matches list all, and a zero-match prints an empty listing at exit 0 on every noun.
- `<noun> get <id>` is exact: a known id renders detail, and an unknown id exits 3 with no candidate list on every noun.
- `agents get` on an installed agent renders detail at exit 0; on a catalogued-but-not-installed agent it reports the agent payload at exit 3; the agnostic soft-path, `--provider` enrichment, `--models` fill, and coverage rollup behave as before; a non-catalog query is exit-3 not-found with no provider fallthrough.
- `providers get <id>` renders the provider's facts, a symmetric-marker provider-env section, and a model count, with `--models` filling the full model table; an unknown provider id exits 3; a models.dev outage with no cache exits 75 and structural drift exits 78.
- `models get anthropic/<model>` renders single-model detail; `models get <bare-id>` (no slash) exits 2 pointing at `models list`; an unknown composite exits 3.
- `models list` lists across all providers; `--provider <id>` and `--agent <id>` scope it; the positional filter composes with scope; ordering is newest-first with the price footer; agnostic `--agent` without `--provider` is a usage error and a home-provider agent rejects `--provider`.
- `--fields` and `--json` behave consistently across all six commands, and the `models` field is JSON-array-typed on every command that carries it.
- No CLI command path routes selection through `internal/match`, and `getFallthrough`/`matchAgent`/`matchProvider` are gone.
- `docs/agentdex-design.md` shows the noun-verb command tree, describes the two verb rules without a none/one/many selector rule or a `providers` exception, carries per-command behaviour notes for all six commands, and its design-decisions rollup matches.
