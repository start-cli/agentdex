# CLI field name review

Source: design refinement on 2026-07-06
Category: Standards / API clarity
Location: internal/cli/views.go, internal/cli/get.go, and the CLI output contract

## Goal

Review every user-facing field name in the agentdex CLI for honesty and clarity, decide a canonical name for each, and migrate the codebase, tests, and docs to the decided names while the tool is still pre-1.0 and the output contract is cheap to change.

## Scope

In scope:

- Every field name in `agentFieldSet` (the detected-agent fields) and `modelFieldSet` (the model fields).
- All three surfaces each name drives: the JSON `.data` key, the `--fields` selector token, and the text detail label. In the current design a single record key drives all three at once.
- List and models table column headers, which derive from the same keys.
- Docs and tests that reference the affected names.

Out of scope:

- Adding, removing, or re-typing fields. This is a naming review, not a schema change. A field's value and presence rules stay as they are; only its name is under review.
- A positional `get <agent> <key>` accessor. That was considered and rejected: it overloads the second positional, which everywhere else in the CLI means a fuzzy selector query, and `--fields <key>` already returns a single value bare. Do not add it.
- The `--tree` flag. It has been removed already; do not reintroduce it.
- The `--field` singular alias for `--fields`. Already added; leave it in place.

## Current State

The CLI exposes a fixed set of selectable fields per record type, declared in `internal/cli/views.go`:

- `agentFieldSet` (detected agent): `id`, `name`, `version`, `bin`, `found`, `config`, `config_local`, `skills`, `providers`, `homepage`, `provider_env`, `models`. Default list columns: `id`, `name`, `version`, `providers`, `bin`.
- `modelFieldSet` (model): `id`, `provider`, `name`, `family`, `context`, `input`, `output`, `reasoning`, `tool_call`, `attachment`, `canonical_id`. Default model-table columns: `id`, `name`, `context`, `input`, `output`.

A record key is a single source that drives three surfaces simultaneously:

- The JSON `.data` object key under `--json`.
- The `--fields` selector token (validated against the field set; an unknown token is a usage error listing the valid set).
- The text detail label. `renderDetail` and `renderFields` print the key itself as the label, and list table headers are the uppercased key (`upper()`).

Because of this coupling, renaming a key moves the JSON key, the selector token, the detail label, and the table header together. There is no current mechanism to give a field a machine name distinct from its display label.

Four agent fields hold filesystem paths and are wrapped in `tui.Path` via the `pathFields` map in `internal/cli/get.go`: `bin`, `config`, `config_local`, `skills`.

The naming concern that prompted this review: several field names describe a different thing from what they return. agentdex owns the outside of an agent and never reads an agent's internal configuration (see AGENTS.md), yet:

- `config` returns the path to the global config directory, not any config content. A user selecting `config` may reasonably expect configuration, and receives a directory path.
- `config_local` returns the local config directory name, again a path, not content.
- `skills` returns the skills directory path, not a list of skills. The `skills` subcommand does list skills, which sharpens the mismatch.
- `bin` returns the binary path and reads acceptably as such, but should be judged by the same rule as the rest.

Field names appear beyond the two field-set declarations. `pathFields`, `detailSections`, and `agentVerboseFields` in `internal/cli` reference keys by literal string. The design doc (`docs/agentdex-design.md`) and `README.md` reference names including `canonical_id` and `--fields`. Tests in `internal/cli/*_test.go` assert on specific keys. A rename must reach all of these to leave the tree consistent.

## Requirements

1. Produce a decision for every field in both field sets: keep the current name, or rename it, with a one-line rationale per field. The rationale judges each name against one rule: the name states what the value is. A field whose value is a filesystem path reads as a location, not as the content a reader might infer from a bare noun.
2. For every field that is renamed, migrate all four coupled surfaces to the new name: JSON `.data` key, `--fields` token, text detail label, and table header. Keep the field's value, presence rules, and ordering unchanged.
3. Update the string-keyed maps and slices that reference field names (`pathFields`, `detailSections`, `agentVerboseFields`, and any similar literal references) to the decided names, so no stale key silently drops a field from path styling, section handling, or verbose columns.
4. Update the design doc and README to the decided names wherever a field name or `--fields` example appears.
5. Update tests to assert on the decided names. Tests must continue to prove the same behaviour under the new names, not merely be deleted.
6. Decide whether the machine name (JSON key and `--fields` token) and the human display label should remain coupled or be split. If a field reads best with a terse machine name and a fuller label, or the reverse, record the decision. If the coupling is kept, the display label equals the machine name and no split mechanism is added. If a split is chosen, introduce the minimum mechanism to carry a separate label and apply it only where it earns its place.

## Constraints

- Go 1.25, pure Go, `CGO_ENABLED=0`. No new dependencies for this work.
- The tool is pre-1.0 (`v0.0.1`). A field rename is a breaking change to the CLI output contract, and doing it now is the point: it is cheap before release and expensive after. Do not add compatibility aliases or dual-name support for old field names; migrate cleanly to the decided names.
- Preserve the single-source design principle unless requirement 6 explicitly decides to split name from label. The field set is the one authority that keeps JSON, `--fields`, and the text surfaces in step; do not fork it into parallel lists that can drift.
- Field ordering in each set is deliberate (JSON key order and text detail order follow it). Renames must not reorder fields.
- Names stay lowercase snake_case, consistent with the existing tokens and with the `--fields` CSV grammar.

## Implementation Plan

1. Enumerate both field sets and, for each field, record what its value actually is (consult `agentRecord` and `modelRecord` for the source value). Produce the per-field decision table required by requirement 1.
2. For the path-valued agent fields (`config`, `config_local`, `skills`, `bin`), decide names that read as locations. Candidate direction: name the directory-valued fields so they read as directories rather than as content or collections. Keep the four path fields consistent with each other in whatever convention is chosen.
3. Review the model fields against the same rule. They are largely already honest (`context`, `input`, `output`, `tool_call`, `canonical_id`); confirm each or rename it, and record the outcome even when the decision is to keep.
4. Resolve requirement 6 (coupled name-and-label versus split) before editing, because it determines whether the migration touches one authority or introduces a label mechanism.
5. Apply the renames at the field-set declarations and every string-keyed reference to a renamed field. Run a repository-wide search for each old name to find stale literals in maps, slices, docs, and tests.
6. Update `docs/agentdex-design.md` and `README.md` to the decided names.
7. Update tests to the decided names, preserving the behaviour each test proves.
8. Run the finalisation sweep from AGENTS.md and exercise the renamed surfaces end to end: `get <agent>` detail, `get <agent> --fields <renamed>` for single and multiple selection, `--json` key inspection, and the `list` and `models` tables including `--verbose`.

## Implementation Guidance

Frame the review as a rule applied uniformly, not a special case for the three flagged fields. The value of the project is a field set where every name states what its value is, so a reader can predict `--fields <name>` output from the name alone. The three flagged fields are the trigger, not the whole scope.

Verify each renamed field on the machine surface as well as the text surface. `agentdex get <agent> --json` reveals the `.data` keys directly; confirm the renamed key appears there and that `--fields <renamed>` selects it, since both must move together with the label.

## Acceptance Criteria

- A per-field decision table exists covering every field in `agentFieldSet` and `modelFieldSet`, each with a keep-or-rename outcome and a one-line rationale.
- No path-valued field carries a name a reader would mistake for content or a collection; the directory-valued agent fields read as locations.
- For every renamed field, `agentdex get <agent> --json`, `agentdex get <agent> --fields <name>`, and the text detail and table surfaces all use the new name, with no occurrence of the old name remaining in `internal/`, `docs/`, `README.md`, or tests.
- The design doc and README show only the decided names in every field and `--fields` reference.
- The field set remains a single authority for JSON, `--fields`, and the text surfaces, unless a name-versus-label split was explicitly decided under requirement 6 and implemented with a documented mechanism.
