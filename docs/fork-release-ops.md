# Fork Release Ops

Operational runbook for shipping the meetneptune/bifrost fork. The fork runs its own CI and publishes its own images; nothing here touches upstream namespaces.

## Mental model

`dev` is the fork's working branch: all fork changes land here via PR (key-free tests in `fork-pr-tests.yml`). The baseline is upstream's stable `transports/v*` releases from maximhq/bifrost, pulled in weekly by `fork-upstream-sync.yml`. Images are built from `dev`, verified by hand, and promoted by hand. There is no automatic path from commit to production.

## Build a candidate

Workflow: `.github/workflows/fork-candidate-build.yml`

- Builds from `transports/Dockerfile.local` and pushes to `ghcr.io/meetneptune/bifrost`.
- Tags: `:<commit-sha>` (immutable pointer to this build) and `:dev-candidate` (rolling "latest from dev").

**`Dockerfile.local` is mandatory for fork images.** `transports/Dockerfile` (upstream release build) sets `GOWORK=off` and resolves `core`/`framework`/plugins from the Go module proxy — i.e. upstream's *published* modules. Built from a fork commit, it would silently ship upstream code without any fork changes. `Dockerfile.local` runs `go work use` over the local module dirs, so the binary carries the code you actually built.

Local reproduction:

```bash
docker build -f transports/Dockerfile.local -t ghcr.io/meetneptune/bifrost:<sha> .
```

## Verify a candidate

Tags are mutable; digests are not. Always verify by digest.

```bash
# Resolve tag -> digest
docker buildx imagetools inspect ghcr.io/meetneptune/bifrost:<sha>

# Pull by immutable digest
docker pull ghcr.io/meetneptune/bifrost@sha256:<digest>

# Run and health-check
docker run -d --name bifrost-candidate -p 8080:8080 ghcr.io/meetneptune/bifrost@sha256:<digest>
curl -sf http://localhost:8080/health
```

- Confirm the digest matches the CI build output for the intended commit.
- Exercise the fork-specific change, not just `/health`.
- `docker stop bifrost-candidate && docker rm bifrost-candidate` when done.

## Promote

Promotion is **manual-only**: `workflow_dispatch`, by immutable digest, retag the verified image to the release tag. Never automatic, never straight to production.

```bash
# Equivalent manual retag (if not using the dispatch workflow)
docker buildx imagetools create \
  -t ghcr.io/meetneptune/bifrost:<release-tag> \
  ghcr.io/meetneptune/bifrost@sha256:<verified-digest>
```

Human-review gate, no exceptions: the digest being promoted must be one a human has verified (previous section) and explicitly approved. If it wasn't verified as a candidate, it does not get promoted.

## Upstream sync

Workflow: `.github/workflows/fork-upstream-sync.yml` (weekly)

- Watches upstream stable `transports/v*` tags (never pre-releases, never upstream `dev`) and opens **one reviewable PR per tag into `dev`**.
- On conflicts it commits the merge with the markers in place on `upstream-sync/<tag>` and opens a **draft** PR that lists each conflicting file. Resolve on that branch, push, and mark the PR ready.
- Files under `.github/workflows` keep `dev`'s version (a `GITHUB_TOKEN` push cannot change them, and the upstream release workflows are guarded off in the fork). The PR body lists the upstream changes to them.
- An existing sync branch that already has commits beyond `dev` is never rebuilt or force-pushed.
- `workflow_dispatch` accepts `tag` (pin a release) and `dry_run` (print the PR body in the run summary; push nothing).
- PR creation needs the repo setting "Allow GitHub Actions to create and approve pull requests" or the secret `SYNC_PR_TOKEN` (fine-grained PAT, contents and pull-requests write). Without either, the job pushes the branch, prints the compare URL in the run summary, and fails with an error that names this fix. A PR opened with `GITHUB_TOKEN` starts no workflows, and `dependency-review` has no manual trigger, so such a PR stays blocked on required checks. Set `SYNC_PR_TOKEN` (preferred; PRs it opens trigger workflows), or close and reopen the PR to start the PR workflows.
- Never auto-merges. If a sync PR is red, the fork stays on its current baseline until a human fixes it.

## CI checks

- Required on every PR to `dev`: `Build and key-free tests` (fork-pr-tests.yml) and `dependency-review` (dependency-review.yml). Both run on every PR without a path filter.
- Upstream-owned workflows are kept on `dev`'s version during sync. A fork guard (`if: github.repository == 'maximhq/bifrost'`) therefore survives later syncs. Never delete an upstream workflow file; guard it.
- Guarded in the fork: all release and publish jobs, CodSpeed, Live Tests, Snyk, OpenSSF Scorecard, and Dependabot Alerts to Issues. `Workflow Lint` runs the egress-allowlist lint only on the fork-owned workflows (`fork-*.yml`, `dependency-review.yml`).
- CodeQL uses GitHub's default setup (a repository setting, not a workflow file). `Analyze (go)` takes about 20 minutes, and the aggregate `CodeQL` check can report a missing Go configuration if it posts before the Go analysis uploads. Do not require `CodeQL` or `Analyze (*)`. Existing alerts are in upstream code.

## What NOT to do

- **Don't push to upstream namespaces.** `release-pipeline.yml`, `npx-publish.yml`, and `helm-release.yml` are upstream release machinery (guarded to `maximhq/bifrost`). Do not trigger, "fix", or unguard them from the fork; do not reuse their secrets or registries for fork builds.
- **Don't run candidate builds from untrusted PR code.** Candidate builds push to the fork registry and may access secrets; only build commits on `dev` or PRs from trusted authors after review.
- **Don't auto-deploy.** No workflow may deploy or retag-to-release on merge. Promotion is the manual, digest-based gate above.
- **Don't build fork images from `transports/Dockerfile`.** It resolves upstream published modules (`GOWORK=off`) and ships upstream code — use `Dockerfile.local`.
