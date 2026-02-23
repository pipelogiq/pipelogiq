# Release Process

This document describes how to cut a new Pipelogiq release.

## Prerequisites

- Push access to the `main` branch
- GitHub CLI (`gh`) installed
- Docker (to verify compose stack)

## Steps

### 1. Verify the build

```bash
make test
make lint
make build
make compose-up   # smoke-test the stack, then Ctrl-C
```

### 2. Update the changelog

Edit `CHANGELOG.md`:

- Add a new `## [x.y.z] - YYYY-MM-DD` section above the previous release
- Move items from `## [Unreleased]` (if present) into the new section
- Add a comparison link at the bottom of the file

### 3. Commit the changelog

```bash
git add CHANGELOG.md
git commit -m "chore: prepare release vX.Y.Z"
```

### 4. Tag the release

```bash
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin main --tags
```

### 5. Create the GitHub Release

Use the tag to create a release. The `release.yml` workflow will run automatically if configured. Otherwise, create manually:

```bash
gh release create vX.Y.Z \
  --title "vX.Y.Z" \
  --notes-file - <<'EOF'
## What's Changed

<!-- Copy the relevant CHANGELOG.md section here -->

## Upgrade Notes

<!-- Any breaking changes or migration steps -->

## Known Limitations

<!-- Carry forward from CHANGELOG.md -->

**Full Changelog**: https://github.com/pipelogiq/pipelogiq/compare/vPREV...vX.Y.Z
EOF
```

### 6. Post-release verification

- Verify the GitHub Release page looks correct
- Verify the Docker Compose quickstart works with the tagged commit:
  ```bash
  git checkout vX.Y.Z
  make compose-up
  ```

## Version Scheme

This project uses [Semantic Versioning](https://semver.org/):

- **v0.x.y** — preview releases; breaking changes may occur between minor versions
- **v1.0.0** — first stable release; breaking changes only in major versions

Build metadata (version, commit, date) is injected via `ldflags` at build time. See `Makefile` for details.
