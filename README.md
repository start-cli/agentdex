# agentdex

agentdex indexes AI coding agents, the [models.dev](https://models.dev) providers that power them, and the models those providers offer, and serves all three as browsable data. For an agent it reports the outside facts — where its binary lives, its installed version, its configuration and skills directories, the model provider(s) it uses, and (enriched from models.dev) the models available to it — and whether it is installed on the local machine; providers and models are queryable in their own right. It answers the "outside" questions about an agent — does it exist, where is it, where does its config and skills live, what can it run — and deliberately never reads or interprets an agent's internal configuration. agentdex ships as a Go library (the primary artefact) plus a thin CLI.

The agent index is data-driven from a published catalog: agentdex never guesses that an arbitrary executable is an agent, and reports one only when the catalog knows it. The catalog is the single source of truth for agent metadata and is fetched from the CUE Central Registry at runtime and cached, so updating the set of known agents does not require an agentdex release.

## Dual module layout

The repository hosts two independent module systems:

- A Go module at the repository root (`github.com/start-cli/agentdex`): the index library and CLI.
- A CUE module under `catalog/` (`github.com/start-cli/agentdex/catalog@v1`): the `#KnownAgent` schema and the agent catalog data, published to the CUE Central Registry and fetched at runtime.

They do not interfere: the Go build ignores `catalog/`, and the CUE module is versioned and published independently of the Go binary.

## Library

The library is the primary artefact; the CLI is a thin shell over it. `Open` returns an `*Index`, the entry point and facade, exposing the three data nouns as services:

```go
type Index struct {
	Agents    AgentService
	Providers ProviderService
	Models    ModelService
}
```

Each service has exactly two operations: a browse `List`, returning a `Result[T]` of items and warnings, and an exact `Get`. Detection is a property of an agent, reported on `Agent.Detection`, not a top-level verb. The `Index` also carries the cache-level operations `Refresh` and `CatalogStale`.

`Open` performs no network I/O. The agent catalog and the models.dev catalog are resolved lazily on the first operation that needs each, once, behind a guard, so the `Index` is safe for concurrent use. Options configure the catalog source (`WithCatalogModule`, `WithCatalogDir`, `WithCatalogTTL`), the caches (`WithCacheDir`, `WithModelsURL`, `WithModelsTTL`), detection (`WithSearchDirs`, `WithBinPaths`), the boundary inputs (`WithEnvLookup`, `WithWorkingDir`, `WithHTTPClient`), and structured debug logging (`WithLogger`, silent by default).

An agent operation takes an `Enrich` level, the single demand axis, each level a superset of the one below: `EnrichNone` (catalog and detection facts only, silent and offline), `EnrichProviders` (adds the resolved provider set), `EnrichCount` (adds provider-env presence, a model count, and coverage on `Agents.Get`), and `EnrichFull` (adds the full models list). Installation status gates none of it, so a caller can ask what an agent offers before installing it. Each returned `Agent` records the outcome in `EnrichmentState` — applied, not-requested, not-applicable (an agnostic agent with no providers), or degraded (models.dev could not fill it).

Warnings are structured: each carries a `Kind` a caller can branch on and a `Msg` it emits verbatim, and they ride on both the success and the error return. Errors are sentinels matched with `errors.Is` — `ErrCatalogUnavailable`, `ErrCatalogInvalid`, `ErrModelsUnavailable`, `ErrAgentUnknown`, `ErrUnknownProvider`, `ErrProvidersRequired`, `ErrProvidersNotAllowed`, `ErrMalformedModelID`, and `ErrNotFound` — with recognisable models.dev schema drift wrapping `modelsdev.ErrModelsSchema` wherever it surfaces.

A worked example, from `Open` through a query to a result:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/start-cli/agentdex"
)

func main() {
	ctx := context.Background()

	idx, err := agentdex.Open(ctx)
	if err != nil {
		log.Fatal(err)
	}

	res, err := idx.Agents.List(ctx, agentdex.AgentQuery{Enrich: agentdex.EnrichCount})
	if err != nil {
		log.Fatal(err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w.Msg)
	}
	for _, a := range res.Items {
		fmt.Printf("%-14s installed=%t models=%d\n", a.ID, a.Detection.Found, a.ModelCount)
	}
}
```

The full surface — every option, service method, query and result type, enrichment level, and error — is documented on the [package](https://pkg.go.dev/github.com/start-cli/agentdex).

## CLI

agentdex ships a thin command-line interface over the library.

The CLI is organised as noun groups (`agents`, `models`, `providers`, each aliased to its singular) with two shared verbs, `list` and `get`.

```
agentdex agents list [filter]     catalogued agents with detection; --installed narrows
agentdex agents get <id>          detail for one agent (aliases: view, show)
agentdex models list [filter]     models across providers, newest release first
agentdex models get <id>          detail for one model, by provider-id/model-id
agentdex providers list [filter]  model providers from models.dev and their API-key status
agentdex providers get <id>       detail for one provider
agentdex refresh [target]         force refresh caches: catalog | models.dev | all
agentdex version
agentdex completion               shell completion script
```

`agents list` lists the whole catalog with each agent's local detection status — the resolved binary in the `BIN` column, or `missing` when the binary was not found on `PATH` — and its models.dev model count, served from the warm cache and degrading to zero (with a warning) when models.dev is unreachable; `--installed` narrows the listing to the agents detected on this machine. `agents get` reports provider-env presence by default; model fill is opt-in via `--models` or a `--fields` selection that includes `models`. `models list` scopes with `--provider` (models.dev provider ids) or `--agent` (a catalogued agent's providers).

Every `list` verb orders by `id` (`models list` by newest release date) and accepts `--order-by <field>` to sort by any field — for example `models list --order-by total` for combined price — with `--reverse` to flip the direction; the sort column is pulled leftmost so the ordering is legible. `--fields` selects output fields on any `list` or `get` verb. Global flags include `--json` (a `status`/`data`/`error`/`warnings` envelope), `--color auto|always|never`, `--search-dir`, and `--bin-path id=path`.

Configuration is optional and lives at `$XDG_CONFIG_HOME/agentdex/config.cue`. See `internal/config/schema.cue` for the full schema.

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
