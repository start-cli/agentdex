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
- CUE Central Registry: https://registry.cue.works (the built-in `CUE_REGISTRY` default) — and `cue login` / `CUE_REGISTRY` for publishing.
- The standard org release pattern: a git tag, a release tarball, and a homebrew formula whose `url`/`sha256`/`commit` fields this release fills in.

## Requirements

1. Catalog module published to the CUE Central Registry as `github.com/start-cli/agentdex/catalog@v1`, fetchable by an agentdex install using the default `catalog.module`.
2. A tagged agentdex release: a git tag and the GitHub source tarball that tag produces. There is no prebuilt compiled binary artefact; brew and `go install` build from source. The ldflag-injected version, commit, and build date are produced by the homebrew formula's build, so `agentdex version` reports the matching values from a brew-built (or equivalent ldflag) binary. A `go install` or plain `go build` reports the baked-in defaults (`dev`, `none`, `unknown`) because it applies no ldflags; that is expected, not a defect.
3. The org-tap homebrew formula completed with the real `url`, `version`, `sha256`, and `commit`, building the tagged release with `CGO_ENABLED=0`, and published to the tap repository (`start-cli/homebrew-tap`) so `brew tap start-cli/tap` serves it. The `version` field is not cosmetic: it is injected into the binary via the formula's `-X …cli.Version=#{version}` ldflag, so leaving it as a placeholder makes `agentdex version` report the placeholder string.
4. End-to-end verification: a clean machine (or clean caches) can install agentdex via both `brew` and `go install` and run `list`, `get`, and `models` successfully against the live registry and live models.dev.

## Constraints

- Each gated action requires explicit owner authorisation for that specific step. Do not publish, tag, or edit tap release fields ahead of that authorisation.
- The published catalog schema and module path must match what documents 01 and 04 build and consume (`github.com/start-cli/agentdex/catalog@v1`). Do not alter the schema at release time.
- No code or schema changes in this document. A defect is fixed in its owning document's scope and re-verified before release resumes.
- Versioning: the catalog module is versioned independently of the binary. Publishing a new catalog version must not require a binary rebuild, and cutting a binary release must not require a catalog republish.
- Follow the repo `AGENTS.md` and the org release conventions; do not add `Co-Authored-By` trailers to commits or tags.

## Implementation Plan

Each numbered step is a stop/resume checkpoint and a separately-authorised gated action.

1. Pre-flight. Confirm documents 01 to 04 are merged and green: full build, `go vet`, `golangci-lint`, tests, and `cue vet` on the catalog module all pass on the release commit. Confirm the seed catalog is the intended content for the first published version; the catalog publish is immutable, so this content is final for that version. Confirm the publish prerequisites before any gated action: `cue login` is authenticated, `catalog/cue.mod/module.cue` declares `source: { kind: "git" }`, the `catalog/` tree is committed clean at the release commit (with `source: git` set, `cue mod publish` publishes from the committed git state), and the CUE Central Registry authorises publishing under `github.com/start-cli/agentdex`. Without the `source` field `cue mod publish` fails before reaching the registry; if it is absent that is a defect fixed in the catalog module's owning document (01) via `cue mod edit --source=git`, committed and re-verified, before release resumes.
2. Publish the catalog module. With owner authorisation, `cue login` as required and publish the committed `catalog/` tree to the CUE Central Registry with `cue mod publish v1.0.0`, honouring `CUE_REGISTRY`. The `@v1` in `module.cue` is the major-version module path; `v1.0.0` is the concrete first version it carries. The published version is immutable: a later seed fix is published as `v1.0.1`, never an overwrite of `v1.0.0`. Verify it resolves under `github.com/start-cli/agentdex/catalog@v1` from a clean CUE module cache.
3. Verify default-path consumption. With the catalog published, run agentdex with no `catalog.module` override and confirm `LoadCatalog`, `list`, and `get` resolve the published catalog, populate the version-resolution cache, and keep working within the TTL.
4. Tag the release. With owner authorisation, cut the version tag; the GitHub source tarball it produces is the release artefact (no separate compiled binary is published). The ldflags that inject version, commit, and build date live in the homebrew formula's build, so version reporting is confirmed once the formula is completed (step 5): build with the formula's ldflags and confirm `agentdex version` reports the matching values. A local `go build` carrying the same `-X` ldflags is the equivalent check before the tap is wired up.
5. Complete the homebrew formula. With owner authorisation, fill the in-repo source-of-truth formula `dist/homebrew/agentdex.rb` (`url`, `version`, `sha256`, and `commit`) for the tagged tarball and confirm it builds agentdex with `CGO_ENABLED=0` via a local `brew install --build-from-source ./dist/homebrew/agentdex.rb`. Confirm the build reports the real values: `agentdex version` must show the tagged version and commit, not a leftover `REPLACE_ME` (the `version` field feeds the `-X …cli.Version=#{version}` ldflag). This edits the repo copy only; the tap is not updated until step 6.
6. Publish the formula to the tap. With owner authorisation, copy the completed `dist/homebrew/agentdex.rb` into the org tap repository (`start-cli/homebrew-tap`) and push. This is a separate gated push to a public repo: until it lands, `brew tap start-cli/tap` still serves the placeholder formula and the step 7 install fails.
7. End-to-end verification. On clean caches, install via `brew tap start-cli/tap` and `go install github.com/start-cli/agentdex/cmd/agentdex@latest`, then run `list`, `get <agent>`, and `models <agent> <query>` against the live registry and live models.dev on both. Confirm `agentdex version` reports the matching version, commit, and build date on the brew install; the `go install` build reports `dev`/`none`/`unknown` (no ldflags applied), so verify only that its `list`/`get`/`models` succeed, not its version string. Confirm the offline-first behaviour: after a populated cache, core detection works without network, and a cold first run with no network fails clearly per the design.

## Acceptance Criteria

- `github.com/start-cli/agentdex/catalog@v1` is published and resolves from a clean CUE module cache; agentdex with the default `catalog.module` loads it.
- The agentdex release is tagged, and a brew-built (or equivalent ldflag) binary reports the matching version, commit, and build date from `agentdex version`. A `go install` build is expected to report `dev`/`none`/`unknown` and is not held to the version-string check.
- The org-tap formula carries the real `url`, `version`, `sha256`, and `commit`, is published to `start-cli/homebrew-tap`, and `brew tap start-cli/tap` installs a working agentdex built with `CGO_ENABLED=0`.
- Both `brew` and `go install` installs run `list`, `get`, and `models` successfully against the live registry and live models.dev.
- A subsequent catalog version can be published without rebuilding the binary, and reaches an existing install within the cache TTL.

## Open Questions

Interactive space for questions raised while preparing this release, and their resolutions. Add each question under a new `###` heading. Record the decision inline: set `Status` to `resolved` and fill `Answer` once decided. When a resolution changes the runbook, fold it into the relevant section above and note that here.

### Q1: Catalog module is not publishable — missing `source` field

Running the step 2 command against the current catalog module fails before it reaches the registry:

```
$ cd catalog && cue mod publish v1.0.0 --dry-run
publishing a module requires a source field in cue.mod/module.cue; choose a source with 'cue mod edit --source'
```

`catalog/cue.mod/module.cue` declares `module` and `language` but no `source`. `cue mod edit --source=git` adds `source: { kind: "git" }`, which clears the error; publish then proceeds to its clean-git-tree check — the state step 1 already establishes. The runbook's "publishes from the committed git state" wording only holds once `source: git` is set.

Per this document's scope, the schema fix belongs to the catalog module's owning document (01), not an inline patch here. The question: confirm the fix is `cue mod edit --source=git` applied in doc 01's scope (committed and re-verified), and that step 1 gains a pre-flight check that `catalog/cue.mod/module.cue` declares `source: { kind: "git" }` so this is caught at the gate rather than at the publish command.

Status: resolved
Answer: Confirmed. The `source: { kind: "git" }` field is added to `catalog/cue.mod/module.cue` in the catalog module's owning document (01) via `cue mod edit --source=git`, committed and re-verified before release resumes — no inline patch in this document. Step 1 now verifies the field as a publish prerequisite so a missing `source` is caught at the gate rather than at the step 2 publish command.
