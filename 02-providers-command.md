# providers command

Source: design discussion, 2026-07-17
Category: CLI surface
Location: internal/cli (new command), docs/agentdex-design.md

## Goal

Add `agentdex providers [filter]`, a command that lists the models.dev providers agentdex can enrich against, so a caller can discover the provider ids that `list`, `get`, and `models` accept via `--provider`. Provider ids are currently vocabulary the user is expected to already know; this command makes them discoverable from agentdex itself.

## Scope

In scope:

- A new top-level `providers` command that lists models.dev providers, with an optional positional filter that narrows the list.
- Per-provider reporting of id, display name, API-key environment variable name(s), whether each of those variables is set in the current environment, and the provider's model list (carried structurally, shown as a count in the table).
- `--fields` and `--json` support consistent with the other commands.
- The command's entry in the design doc command list and behaviour section.

Out of scope:

- Any change to the agent catalog or its schema. Providers come from models.dev, not the catalog.
- Any new root-package (`agentdex`) library API. The command is a thin CLI layer over the existing `modelsdev.Client`.
- A separate single-provider detail view or page. The listing columns carry the provider facts, and the structured `models` field carries the model array for scripting, but no text surface renders a per-provider model detail table; that stays with `agentdex models`. There is no none/one/many selector switch (see Requirement 3).
- Reading or interpreting any agent's internal configuration. The command takes no agent argument.
- New dependencies.

## Current State

The CLI is a thin cobra layer over the library and the `modelsdev` client, in `internal/cli`. The command tree is built in `internal/cli/root.go` (`NewRootCommand`), where each command is registered under the single `groupCore` help group. Global flags (`--json`, `--verbose`, `--quiet`, `--color`, `--debug`, `--search-dir`, `--bin-path`) are bound there. `preRun` loads config into `app.cfg` and settles colour and logging.

The nearest existing command is `models` (`internal/cli/models.go`). Its list path (`modelsList`) shows the shape to follow: it calls `a.requireConfig()`, builds a `modelsdev.Client` via `cfg.ModelsClient()`, iterates providers, builds `record` rows against a declared `fieldSet`, tabulates, and emits through the shared `a.ok(...)` envelope with a text render. Its `modelsCode` helper classifies failures into the exit taxonomy. `models` is model-centric, so a models.dev outage with no cache is transient (exit 75).

The `modelsdev` client already exposes everything the listing needs:

- `Client.Catalog(ctx) (*Catalog, error)` returns the merged catalog; `Catalog.Providers` is `map[string]Provider` keyed by provider id.
- `Provider` carries `ID`, `Name`, `Doc`, `NPM`, `API`, `Env []string` (API-key variable names), and `Models map[string]Model`.
- `Client.Provider(ctx, id)` and `Client.Models(ctx, ids...)` apply per-model required-field validation; a full listing does not need them and should avoid triggering per-model validation (see Implementation Guidance).

The output machinery lives in `internal/cli/envelope.go`: `envelope` (status/data/error/warnings), `fieldSet` (declared valid keys plus default table columns), and `record` (an ordered, selectable set of present fields). `views.go` builds agent and model records and holds `providerEnvText` / `formatProviderEnv` / `styledProviderEnv`, which render a `map[string]bool` of env var to presence as deterministic ordered `NAME=present` markers. `render.go` holds `renderTable`, `renderDetail`, `renderFields`, `tabulate`, and the `sortedKeys` helper. `get.go` holds the field-flag registration (`registerFieldsFlag`, `addFieldsHelpSection`).

Env-var presence is already read at the boundary in `probe.go` (`enrich`), which calls `os.LookupEnv(env)` per provider env var and records the result in `Agent.ProviderEnv`. That is the same presence semantics this command reports, computed per provider.

Exit codes are in `exit.go` (`codeOK` 0, `codeUsage` 2, `codeNotFound` 3, `codeTransient` 75, `codeConfig` 78, plus others). models.dev schema drift is `modelsdev.ErrModelsSchema`, classified as config (78), never transient.

There is currently one project document, `01-theme-safe-terminal-path-colour.md`, and the design doc `docs/agentdex-design.md`. The design doc command list is around line 706; its per-command behaviour notes follow.

## Requirements

1. Add a top-level command `agentdex providers [filter]` registered under `groupCore`. It takes at most one positional argument, the filter. It takes no agent argument.

2. With no filter, list every models.dev provider, sorted by id.

3. The positional argument is a filter, not a selector. It narrows the list to providers whose id or display name contains the filter as a case-insensitive substring. It does not apply the none/one/many selector rule: a filter that matches several providers lists all of them (it does not report ambiguity), and a filter that matches none produces an empty list and exit 0 (it is not "not found"). Do not route the filter through `internal/match`.

4. The default text output is a table with these columns, in order:
   - `ID`: the provider id (the value usable with `--provider`).
   - `NAME`: the provider display name.
   - `ENV`: the provider's API-key environment variable name(s), sorted and joined deterministically. A name whose variable is set in the current environment carries a `(set)` suffix; an unset variable renders as the bare name, so a bare name means unset. A provider with no declared env var renders blank. Presence folds into this column, so there is no separate `PRESENT` column; the structured `present` field (Requirement 5) carries per-variable presence for scripting.
   - `MODELS`: the count of models the provider offers, rendered in the table cell. The structured `models` field carries the provider's model list itself (Requirement 5); the cell renders its length, the same way `list` shows a count over a `models` array.

5. Provide a declared `fieldSet` for the provider record. The full field set includes at least `id`, `name`, `env`, `present`, `models`, and additionally exposes `doc`, `npm`, and `api` for scripting. `present` is the structured per-variable presence map (variable name to boolean); it is a selectable field but not a default table column, so scripts read presence without parsing the `(set)` suffix out of the `env` text. `models` is the provider's model list, carried in JSON as an array sourced from `provider.Models` and rendered in the table as its count, exactly as `list` and `get` carry a `models` array behind a count cell; the field stays array-typed across every command so a caller reads `.data[].models` uniformly rather than meeting a scalar under the same key. The default table columns are the four in Requirement 4: `id`, `name`, `env`, `models`. `--fields` overrides the table columns and drives the JSON payload exactly as it does for the `models` command.

6. Emit through the shared JSON envelope. `--json` carries the provider records as `data`; the same `status`/`error`/`warnings` contract as every other command applies. The `warnings` slice stays part of the envelope shape but is empty in practice for this command: `modelsdev.Client.Catalog` serves a stale cache silently and exposes no stale signal to warn on, and an unavailable-and-uncached models.dev is a transient failure (Requirement 8), not a warning. The command loads no agent catalog, so it inherits none of the agent-catalog staleness warnings the `list`, `get`, and `models` paths carry.

7. Environment-variable presence is read at the boundary through an injectable lookup that defaults to `os.LookupEnv`, so the record-building logic is testable from inputs without `t.Setenv`. Match the boundary discipline already used in `probe.go`.

8. Classify failures into the existing exit taxonomy. A models.dev outage with no usable cache leaves the command with no result and is transient (exit 75), matching `models`. A gross models.dev structural fault (`modelsdev.ErrModelsSchema`) is a data fault, config (exit 78), never transient. Invalid `--fields` or `--color` remains a usage fault (exit 2) through the shared usage path.

9. Update `docs/agentdex-design.md`: add `providers` to the command list and add a behaviour note describing the filter semantics, the columns, the boundary env read, and the transient classification. Amend the selector-matching section itself: the "Every positional selector … one rule applied everywhere" statement enumerates `<agent>`, model `[query]`, and skill `[name]`; carve `providers [filter]` out of it with an explicit exception clause, right where the none/one/many rule is stated, so the browse-and-narrow filter is named as the one positional that does not follow that rule (a no-match is exit 0, not exit 3) rather than only in a separate behaviour note. The same one-rule enumeration is restated in the design-decisions rollup near the end of the doc (the "CLI selector matching is one rule … applied to `<agent>`, model `[query]`, and skill `[name]`" bullet); amend that bullet too so both statements of the rule name `providers [filter]` as exempt and the doc does not contradict itself. State the `ENV` presence convention explicitly: a `(set)` suffix marks a set variable and a bare name means unset, a deliberate divergence from the symmetric `(set)/(unset)` markers `get` shows, chosen to keep the wide listing terse.

## Constraints

- Go 1.25, pure Go, `CGO_ENABLED=0`. No new dependencies; build the command from the standard library, cobra, and the already-carried `modelsdev` client and CLI machinery.
- The library is the primary artefact and the CLI is a thin layer. This command adds no behaviour to the root `agentdex` package; it composes `modelsdev.Client` and the existing CLI output machinery. Do not duplicate anything `modelsdev` already provides.
- Report only the outside of the system. Presence of an API-key variable in the environment is a boundary fact the command may report; do not read any agent's internal configuration, and do not read the values of the environment variables, only whether each is set.
- Keep nondeterministic inputs (the env lookup, the clock, the network) at the boundary so the record builder and the filter are testable from inputs.
- Match the conventions of the surrounding CLI code: the `fieldSet`/`record` machinery, the `a.ok` / `a.fail` / `a.usage` envelope helpers, and the `models` command's structure. Do not introduce a new output path.
- Agent-facing markdown edits to the design doc follow the repo markdown rules: no bold, italic, horizontal rules, or emojis; headings no deeper than `###`.

## Implementation Plan

1. Declare the provider `fieldSet` (full set and default columns per Requirements 4 and 5) alongside the existing agent and model field sets in `views.go`, and add a `providerRecord` builder that maps a `modelsdev.Provider` plus its computed env-presence map into a `record`. Render the `env` cell from the sorted env-var names, appending the `(set)` marker after each name whose variable is set (reuse the existing `plainState` state marker so the suffix matches `get`); keep the structured `present` map as its own field for JSON. Build the `models` field from `provider.Models` in a deterministic order and add it through the existing `withModels` pattern, so JSON carries the model array and the `MODELS` cell renders its count.

2. Add a boundary env-presence helper that takes a provider's `Env` slice and a lookup function (defaulting to `os.LookupEnv`) and returns the `map[string]bool` the record and the presence renderer consume. Keep the lookup injectable for tests.

3. Add `internal/cli/providers.go` with `newProvidersCmd`. It calls `a.requireConfig()` (for the models.dev client settings, not the agent catalog), builds the client with `cfg.ModelsClient()`, fetches the merged catalog with `Client.Catalog(ctx)`, applies the case-insensitive substring filter over provider id and name, sorts by id, builds records, tabulates, and emits through `a.ok`. Do not load the agent catalog.

4. Add a `providersCode` classifier (or reuse a shared one) mapping `modelsdev.ErrModelsSchema` to config (78) and any other no-result models.dev failure to transient (75), consistent with `modelsCode`.

5. Register the command in `NewRootCommand` under `groupCore`, and register the `--fields` flag and its help section the way `models` does.

6. Add tests in `internal/cli` following the existing harness: a full unfiltered listing, a filter that narrows to several providers, a filter that matches nothing (empty list, exit 0), env presence reported for a provider whose var is set versus unset (via the injected lookup, not `t.Setenv`), `--fields` selection and validation, `--json` envelope shape (asserting `models` is an array while the `MODELS` cell shows its count), and the transient classification when models.dev is unreachable with no cache. Prefer the frozen models.dev testdata already used by the models tests over the network.

7. Update `docs/agentdex-design.md` per Requirement 9.

## Implementation Guidance

- Use `Client.Catalog(ctx)` for the listing, not `Client.Provider` / `Client.Models`. Those apply per-model required-field validation, which is irrelevant to a provider listing and would let one malformed model make the whole listing fail. A provider's model list and env vars are meaningful regardless of any single model's field validity, so the `models` field carries `provider.Models` straight from the merged catalog without routing through the validating accessors. Gross structural drift still surfaces from `Catalog` and is classified as config; per-model validity is simply not this command's concern.
- The filter is deliberately not the selector rule. The design's one-selector-rule applies to `<agent>`, model `[query]`, and skill `[name]`, where selecting a single target is the point. `providers` is a browse-and-narrow surface over 167-plus entries, so the positional narrows a list and an empty result is a normal exit-0 outcome. Keep this distinction visible in the design doc note so the divergence is intentional and documented, not an oversight.
- Build the env-presence `map[string]bool` (var name to presence) once per provider. The default text output renders a single `ENV` column from its sorted keys, appending `(set)` to a name whose value is true and leaving an unset name bare; the same map is exposed unchanged as the structured `present` field for JSON and `--fields`. Folding presence into `ENV` keeps a wide browse listing terse — configured keys stand out rather than every row trailing a marker — and leaves `present` as the parse-free presence source for scripts.
- The command needs config only for the models.dev client settings (cache dir, TTL, URL overrides). It must not call `agentdex.LoadCatalog`; loading the agent catalog would couple a pure models.dev surface to catalog availability for no reason.

## Acceptance Criteria

- `agentdex providers` lists every models.dev provider sorted by id, with `ID`, `NAME`, `ENV`, and `MODELS` columns.
- `agentdex providers <substring>` lists exactly the providers whose id or name contains the substring case-insensitively; a substring matching several providers lists all of them, and one matching none prints an empty listing and exits 0.
- For a provider with an API-key variable set in the environment, the `ENV` cell carries the `(set)` suffix after that variable and the structured `present` field marks it true; for an unset variable the name renders bare and `present` marks it false; driven by the injected lookup in tests without `t.Setenv`.
- `agentdex providers --json` emits the shared envelope with the provider records as `data`; the `models` field is a JSON array of the provider's models, matching the `models` shape `list` and `get` emit, with the `MODELS` table cell rendering its count; `--fields` overrides the table columns and selects the same fields for text and JSON, and an unknown field is a usage error (exit 2).
- With models.dev unreachable and no cache, `agentdex providers` fails transient (exit 75); a gross models.dev structural fault classifies as config (exit 78).
- `agentdex providers` never loads the agent catalog and takes no agent argument.
- `docs/agentdex-design.md` lists `providers` in the command list and describes its filter semantics, columns, boundary env read, and transient classification; its selector-matching section names `providers [filter]` as the positional exempt from the none/one/many rule, and the note states the `ENV` set-suffix convention (bare name means unset).
