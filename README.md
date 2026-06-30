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

```
agentdex list                    detected agents, table by default
agentdex get <agent>             detail for one agent (aliases: view, show)
agentdex models <agent> [query]  models available to the agent; query fuzzy-matches
agentdex refresh [target]        force refresh caches: catalog | models | all
agentdex version
agentdex completion              shell completion script
```

`list` is offline-fast once cached and does not enrich models; pass `--models` to opt in. `get` enriches models and reports provider-env presence by default; `--no-models` skips per-model enrichment while keeping provider-env, and `--tree` prints the config directory tree. Global flags include `--json` (a `status`/`data`/`error`/`warnings` envelope), `--color auto|always|never`, `--search-dir`, and `--bin-path id=path`. The `--fields` flag (field selection) is per-command, available on `list`, `get`, and `models`.

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
