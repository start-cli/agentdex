# Theme-safe terminal path colour

Source: pre-commit review on 2026-07-04
Severity: low
Category: Standards / Readability
Location: internal/tui/color.go:49, consumed at internal/cli/get.go:308-313

## Goal

Make filesystem paths in agentdex's text detail view legible on both dark and light terminal backgrounds, and resolve the underlying colour choice in the shared start terminal colour standard so sibling start-cli tools inherit a theme-safe path style rather than each repeating the same mistake.

## Scope

In scope:

- The path style used by agentdex's `get` detail view.
- The path-colour decision in the shared start terminal colour standard, since the choice is not agentdex-specific.

Out of scope:

- Any other style in the palette (headers, labels, state markers, URLs). They are not part of this finding.
- Table cell and `--fields` / JSON output. These already render paths plain and must stay plain.

## Current State

agentdex renders human-facing text through `internal/tui/color.go`, a set of `fatih/color` styles gated by a process-wide `NoColor` toggle. `Configure` sets that toggle from the `--color` mode, `NO_COLOR`, and TTY detection, so `--color never` and non-TTY output already emit plain text.

The detail view for `agentdex get <agent>` (`internal/cli/get.go`, `renderAgentDetail`) prints aligned `label  value` lines. Labels use `tui.Label` (`FgCyan`). Most scalar values (`id`, `name`, `version`, `providers`) use the terminal default foreground. Four path fields — `bin`, `config`, `config_local`, `skills` — are singled out and wrapped in `tui.Path` via the `pathFields` map.

`tui.Path` is currently `color.New(color.FgHiWhite)`. Its doc comment records that `HiWhite` was chosen over the start standard's `HiCyan` specifically to avoid two adjacent cyans (cyan label beside a cyan path reading as one run).

The problem: `FgHiWhite` is a fixed bright white. On a light-background terminal it has little or no contrast, so the path — the most load-bearing "where is it" data in the view — becomes hard to read or invisible. The prior behaviour (paths in the default foreground) was legible on any background because the terminal maps the default foreground to its theme. The cyan label already separates label from value, so a path does not need its own colour to be distinguishable from its label.

The same path-colour question applies to every start-cli tool that adopts the shared terminal colour standard, which is why the standard specifies a path colour at all. Resolving it only inside agentdex would leave the standard pointing every other tool at the same light-terminal failure.

## Requirements

1. Paths in the `get` detail view are legible on both dark and light terminal backgrounds. No fixed bright colour that assumes a dark background.
2. The path/label distinction is preserved without relying on a colour that fails on light terminals. Two adjacent cyans must not read as one run.
3. Table cells, `--fields` output, and JSON continue to carry no colour codes. This finding does not extend colour into those surfaces.
4. `--color never`, `NO_COLOR`, and non-TTY output remain plain, unchanged.
5. The resolved path-colour decision is reflected in the shared start terminal colour standard, not only in agentdex, so sibling tools inherit the theme-safe choice. If agentdex intentionally diverges from the standard, the divergence and its rationale are documented at the agentdex style rather than silently encoded.

## Constraints

- Go 1.25, pure Go, `CGO_ENABLED=0`. No new dependencies; styling stays on the already-carried `fatih/color`.
- Colour must remain routed through the `tui` package's shared `NoColor` toggle. Do not bypass `Configure`.
- Target terminals include both dark and light background themes. Do not assume a dark background.

## Implementation Plan

1. Decide the theme-safe path treatment. The safe default is the terminal default foreground (drop the dedicated path colour and let paths render like the other scalar values); the cyan label already carries the label/value distinction. If a distinct path emphasis is still wanted, choose an attribute that survives both backgrounds (for example a non-colour attribute such as underline, or a colour verified legible on light and dark) rather than a fixed bright foreground.
2. Apply the decision to `tui.Path` (or remove `Path` and its `pathFields` application if paths revert to the default foreground). Keep the four path fields consistent with each other.
3. Update the shared start terminal colour standard to match, so the path colour recommendation there is theme-safe. If agentdex diverges, record the divergence and reason at the agentdex style.
4. Verify the rendered output on a light and a dark background: `agentdex --color always get <agent>` and `agentdex --color always --verbose get <agent>`. Confirm paths are legible in both and that state markers, labels, and headers are unchanged.
5. Confirm table, `--fields`, and JSON output for `list`, `models`, and `get` still carry no colour in the path columns.

## Acceptance Criteria

- Path lines in `agentdex get <agent>` are readable against a white terminal background and a black terminal background.
- The `bin`, `config`, `config_local`, and `skills` path fields share one consistent treatment.
- `agentdex --json get <agent>` and `agentdex get <agent> --fields bin,config` emit paths with no ANSI codes.
- The shared start terminal colour standard's path guidance is theme-safe, or agentdex's deviation from it is documented at the style.
