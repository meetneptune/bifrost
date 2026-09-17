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

- Watches upstream stable `transports/v*` tags and opens **one reviewable PR per tag into `dev`**.
- Conflicts and build failures are surfaced *in that PR* for a human to resolve.
- Never force-pushes, never auto-merges. If a sync PR is red, the fork stays on its current baseline until a human fixes it.

## What NOT to do

- **Don't push to upstream namespaces.** `release-pipeline.yml`, `npx-publish.yml`, and `helm-release.yml` are upstream release machinery (guarded to `maximhq/bifrost`). Do not trigger, "fix", or unguard them from the fork; do not reuse their secrets or registries for fork builds.
- **Don't run candidate builds from untrusted PR code.** Candidate builds push to the fork registry and may access secrets; only build commits on `dev` or PRs from trusted authors after review.
- **Don't auto-deploy.** No workflow may deploy or retag-to-release on merge. Promotion is the manual, digest-based gate above.
- **Don't build fork images from `transports/Dockerfile`.** It resolves upstream published modules (`GOWORK=off`) and ships upstream code — use `Dockerfile.local`.
