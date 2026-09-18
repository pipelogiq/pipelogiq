# Release Process

The release workflow publishes a reviewed tag after the complete CI workflow
passes for that exact commit. Preparing a release locally does not publish it.

## Prepare the release

1. Update `VERSION` and all current version references, including the Helm chart,
   OpenAPI version, README and quickstart. Preserve historical changelog entries
   and release notes.
2. Add the version's changelog entry and detailed notes at
   `docs/releases/RELEASE_NOTES_vX.Y.Z.md`. The first line must be
   `# Pipelogiq vX.Y.Z`; the body must describe changes, upgrade requirements,
   limitations and the SDK versions verified with this release.
3. Run the [source and secret checks](docs/source-hygiene.md), then
   `./scripts/check-versions.sh vX.Y.Z`, the tests and lint checks. Smoke-test
   the Compose stack and the Helm chart with the intended release settings.
4. Review and merge the release preparation into `main`, then wait for CI to pass.

Complete any [required credential rotation](docs/credential-rotation.md) before
reusing affected deployment configuration. See the current
[release readiness report](docs/release-preparation-2026-09-18.md) for external
prerequisites and the distinction between source preparation and publication.

Before publishing, configure the repository secret `LICENSE_PUBLIC_KEY` with
the base64 encoding of the 32-byte Ed25519 public verification key. The preflight
rejects missing, malformed and all-zero placeholder keys. The matching private
signing key is never part of this repository or its release artifacts.

## Publish one tag

Use a clean checkout of the reviewed commit on `main`. The following commands
publish only the tag corresponding to `VERSION`:

```bash
set -euo pipefail
test -z "$(git status --porcelain)"
git switch main
git pull --ff-only origin main
release_tag="v$(cat VERSION)"
./scripts/check-versions.sh "$release_tag"
git tag -a "$release_tag" -m "Release $release_tag"
git push origin "refs/tags/$release_tag"
```

Do not push all local tags, move an existing release tag or create the GitHub
Release manually. The workflow checks the tag format, version references, release
notes, public key and membership of the tagged commit in `main` before running CI.
For a transient failure, rerun the failed workflow jobs against the unchanged tag.

## Artifacts and publication order

The workflow runs the same hygiene, Go, PostgreSQL, web and image checks used by
normal CI. Once they pass, it prepares:

- API and worker binaries for Linux and macOS, each for amd64 and arm64, built
  with the Pro code and the licence verification key.
- `pipelogiq-X.Y.Z.tgz`, the packaged Helm chart with matching `appVersion`.
- The detailed release notes and `SHA256SUMS` covering these downloadable files.

It then publishes the versioned `pipelogiq-app`, `pipelogiq-worker` and
`pipelogiq-migrate` images to GHCR for `linux/amd64` and `linux/arm64`. App and
worker images receive the same version metadata and licence public key as the
binaries. After all three images succeed, a normal release updates their
`latest` tags. Preview and release-candidate tags leave `latest` unchanged.

The public GitHub Release is created last, after artifact preparation and image
publication succeed. Its body is the matching detailed release-notes file; the
binaries, Helm package, notes and checksums are attached as assets. Registry
uploads are separate operations: a failed run can leave some versioned images
uploaded, so an incomplete workflow is not a completed release.

## Verify the published release

- Confirm the GitHub Release contains all eight binaries, the Helm package,
  detailed notes and checksums; run `sha256sum --check SHA256SUMS` after downloading
  them together.
- Check each versioned GHCR image contains both supported Linux architectures.
- Smoke-test the registry Compose configuration using the exact version tag.
- Install or upgrade using the packaged Helm chart in a test namespace and verify
  the migration job, API and worker become healthy.

## Version scheme

`v0.5.0` is a normal release. Only explicit `-preview.N` and `-rc.N` suffixes mark
pre-releases in GitHub and prevent Docker `latest` updates. The workflow accepts
`vMAJOR.MINOR.PATCH` with either of those optional suffixes.

Versions before `1.0.0` may contain breaking changes between minor releases;
those changes must be documented. A normal `0.x` release does not promise the
compatibility guarantees of `1.x`. Build version, commit and date are embedded in
the binaries and Docker images.
