# agentdex

agentdex detects the AI coding agents installed on the local machine and reports, for each known agent, where its binary lives, its installed version, its configuration and skills directories, the model provider(s) it uses, and (enriched from [models.dev](https://models.dev)) the models those providers offer. It answers the "outside" questions about an agent — does it exist, where is it, where does its config and skills live, what can it run — and deliberately never reads or interprets an agent's internal configuration. agentdex ships as a Go library (the primary artefact) plus a thin CLI.

Detection is data-driven from a published catalog: agentdex never guesses that an arbitrary executable is an agent. The catalog is the single source of truth for agent-discovery metadata and is fetched from the CUE Central Registry at runtime and cached, so updating the set of known agents does not require an agentdex release.

## Dual module layout

The repository hosts two independent module systems:

- A Go module at the repository root (`github.com/start-cli/agentdex`): the detection library and CLI.
- A CUE module under `catalog/` (`github.com/start-cli/agentdex/catalog@v1`): the `#KnownAgent` schema and the agent catalog data, published to the CUE Central Registry and fetched at runtime.

They do not interfere: the Go build ignores `catalog/`, and the CUE module is versioned and published independently of the Go binary.

## License

[MPL-2.0](LICENSE).
