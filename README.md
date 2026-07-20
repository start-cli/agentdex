# agentdex

agentdex detects the AI coding agents installed on the local machine and reports, for each known agent, where its binary lives, its installed version, its configuration and skills directories, the model provider(s) it uses, and (enriched from [models.dev](https://models.dev)) the models those providers offer. It answers the "outside" questions about an agent — does it exist, where is it, where does its config and skills live, what can it run — and deliberately never reads or interprets an agent's internal configuration. agentdex ships as a Go library (the primary artefact) plus a thin CLI.

Detection is data-driven from a published catalog: agentdex never guesses that an arbitrary executable is an agent. The catalog is the single source of truth for agent-discovery metadata and is fetched from the CUE Central Registry at runtime and cached, so updating the set of known agents does not require an agentdex release.

## Dual module layout

The repository hosts two independent module systems:

- A Go module at the repository root (`github.com/start-cli/agentdex`): the detection library and CLI.
- A CUE module under `catalog/` (`github.com/start-cli/agentdex/catalog@v1`): the `#KnownAgent` schema and the agent catalog data, published to the CUE Central Registry and fetched at runtime.

They do not interfere: the Go build ignores `catalog/`, and the CUE module is versioned and published independently of the Go binary.

## CLI

agentdex ships a thin command-line interface over the library.

The CLI is organised as noun groups (`agents`, `models`, `providers`, each aliased to its singular) with two shared verbs, `list` and `get`.

```
agentdex agents list [filter]     catalogued agents with detection; --installed narrows
agentdex agents get <id>          detail for one agent (aliases: view, show)
agentdex models list [filter]     models across providers, newest release first
agentdex models get <id>          detail for one model, by provider-id/model-id
agentdex providers list [filter]  models.dev providers agentdex can enrich against
agentdex providers get <id>       detail for one provider
agentdex refresh [target]         force refresh caches: catalog | models | all
agentdex version
agentdex completion               shell completion script
```

`agents list` lists the whole catalog with each agent's local detection status — the resolved binary in the `BIN` column, or `missing` when the binary was not found on `PATH` — and its models.dev model count, served from the warm cache and degrading to zero (with a warning) when models.dev is unreachable; `--installed` narrows the listing to the agents detected on this machine. `agents get` reports provider-env presence by default; model fill is opt-in via `--models` or a `--fields` selection that includes `models`. `models list` scopes with `--provider` (models.dev provider ids) or `--agent` (a catalogued agent's providers).

Every `list` verb orders by `id` (`models list` by newest release date) and accepts `--order-by <field>` to sort by any field — for example `models list --order-by total` for combined price — with `--reverse` to flip the direction; the sort column is pulled leftmost so the ordering is legible. `--fields` selects output fields on any `list` or `get` verb. Global flags include `--json` (a `status`/`data`/`error`/`warnings` envelope), `--color auto|always|never`, `--search-dir`, and `--bin-path id=path`.

Configuration is optional and lives at `$XDG_CONFIG_HOME/agentdex/config.cue`. See `docs/agentdex-design.md` for the full schema.

## Install

Go:

```
go install github.com/start-cli/agentdex/cmd/agentdex@latest
```

Homebrew (once the tap release is published):

```
brew tap start-cli/tap
brew install agentdex
```

## License

[MPL-2.0](LICENSE).
