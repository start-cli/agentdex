# 05 Release and Publish

## Goal

Take the built agentdex from documents 01 to 04 and make it real for users: publish the agent catalog CUE module to the CUE Central Registry, cut a tagged binary release, and complete the homebrew formula so `brew` and `go install` both deliver a working agentdex against the live registry and models.dev.

## Scope

In scope:

- Publishing the catalog CUE module as `github.com/start-cli/agentdex/catalog@v1`.
- Tagging the agentdex Go binary release and producing the release artefacts.
- Completing the org-tap homebrew formula (`url`, `sha256`, `commit`, version) and verifying installation.
- End-to-end verification against the live registry and live models.dev.

Out of scope:

- Any code or schema changes. If a defect surfaces, it is fixed back in the relevant document's scope (01 to 04), not patched here.
- The comprehensive catalog authoring, the skills command, and migrating downstream consumers onto agentdex. All are separate future projects.

## Current State

Documents 01 to 04 are complete and verified against fixtures and locally-overridden modules:

- The catalog CUE module exists under `catalog/` and vets clean, but has not been published to the registry.
- The agentdex library and CLI build and pass tests, but no version tag has been cut.
- The org-tap homebrew formula for agentdex exists with placeholder release fields (`url`, `sha256`, `commit`, version), authored in document 04.

This document is a release runbook. Every step here is a gated, outward-facing action — publishing to a public registry, tagging a release, editing tap release fields. None may be performed without the owner's explicit go-ahead for that specific step. Run the steps in order; each is a checkpoint at which the session can stop and resume.

## References

- `docs/agentdex-design.md` — sections: Agent catalog (Home and publishing, Delivery), Build and distribution.
- CUE Central Registry: https://registry.cuelang.org — and `cue login` / `CUE_REGISTRY` for publishing.
- The standard org release pattern: a git tag, a release tarball, and a homebrew formula whose `url`/`sha256`/`commit` fields this release fills in.

## Requirements

1. Catalog module published to the CUE Central Registry as `github.com/start-cli/agentdex/catalog@v1`, fetchable by an agentdex install using the default `catalog.module`.
2. A tagged agentdex binary release with the release tarball and the ldflag-injected version, commit, and build date correct in the published binary.
3. The org-tap homebrew formula completed with the real `url`, `sha256`, and `commit`, building the tagged release with `CGO_ENABLED=0`.
4. End-to-end verification: a clean machine (or clean caches) can install agentdex via both `brew` and `go install` and run `list`, `get`, and `models` successfully against the live registry and live models.dev.

## Constraints

- Each gated action requires explicit owner authorisation for that specific step. Do not publish, tag, or edit tap release fields ahead of that authorisation.
- The published catalog schema and module path must match what documents 01 and 04 build and consume (`github.com/start-cli/agentdex/catalog@v1`). Do not alter the schema at release time.
- No code or schema changes in this document. A defect is fixed in its owning document's scope and re-verified before release resumes.
- Versioning: the catalog module is versioned independently of the binary. Publishing a new catalog version must not require a binary rebuild, and cutting a binary release must not require a catalog republish.
- Follow the repo `AGENTS.md` and the org release conventions; do not add `Co-Authored-By` trailers to commits or tags.

## Implementation Plan

Each numbered step is a stop/resume checkpoint and a separately-authorised gated action.

1. Pre-flight. Confirm documents 01 to 04 are merged and green: full build, `go vet`, `golangci-lint`, tests, and `cue vet` on the catalog module all pass on the release commit. Confirm the seed catalog is the intended content for the first published version.
2. Publish the catalog module. With owner authorisation, `cue login` as required and publish `catalog/` to the CUE Central Registry as `github.com/start-cli/agentdex/catalog@v1`, honouring `CUE_REGISTRY`. Verify it resolves from a clean CUE module cache.
3. Verify default-path consumption. With the catalog published, run agentdex with no `catalog.module` override and confirm `LoadCatalog`, `list`, and `get` resolve the published catalog, populate the version-resolution cache, and keep working within the TTL.
4. Tag the binary release. With owner authorisation, cut the version tag and produce the release tarball, ensuring ldflags inject the matching version, commit, and build date. Confirm `agentdex version` reports them.
5. Complete the homebrew formula. With owner authorisation, fill the org-tap formula `url`, `sha256`, and `commit` for the tagged tarball and confirm it builds agentdex with `CGO_ENABLED=0`.
6. End-to-end verification. On clean caches, install via `brew tap start-cli/tap` and `go install github.com/start-cli/agentdex/cmd/agentdex@latest`, then run `list`, `get <agent>`, and `models <agent> <query>` against the live registry and live models.dev. Confirm the offline-first behaviour: after a populated cache, core detection works without network, and a cold first run with no network fails clearly per the design.

## Acceptance Criteria

- `github.com/start-cli/agentdex/catalog@v1` is published and resolves from a clean CUE module cache; agentdex with the default `catalog.module` loads it.
- The agentdex binary release is tagged, and `agentdex version` reports the matching version, commit, and build date.
- The org-tap formula carries the real `url`, `sha256`, and `commit`, and `brew` installs a working agentdex built with `CGO_ENABLED=0`.
- Both `brew` and `go install` installs run `list`, `get`, and `models` successfully against the live registry and live models.dev.
- A subsequent catalog version can be published without rebuilding the binary, and reaches an existing install within the cache TTL.
