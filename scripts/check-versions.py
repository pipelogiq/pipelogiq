#!/usr/bin/env python3
"""Fail closed on release version drift; no external packages required."""
from __future__ import annotations

import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parent.parent
SEMVER = r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:preview|rc)\.[1-9]\d*)?"
CHECKED_FILES = (
    "VERSION", "README.md", "CHANGELOG.md", "Makefile",
    "apps/web/package.json", "apps/web/package-lock.json",
    "apps/go/internal/version/version.go", "docs/openapi.external.yaml",
    "docs/index.md", "docs/quickstart.md", "docs/operations.md",
    "docs/versioning.md", "infra/helm/pipelogiq/Chart.yaml",
    "infra/docker/Dockerfile.app", "infra/docker/Dockerfile.worker",
    "infra/docker/Dockerfile.migrate",
    "infra/compose/docker-compose.app.yml", "infra/compose/docker-compose.worker.yml",
    "infra/compose/docker-compose.build.yml", "infra/compose/docker-compose.registry.yml",
)


def check(root: Path, tag: str | None = None) -> list[str]:
    errors = []

    def read(name):
        try:
            return (root / name).read_text()
        except (OSError, UnicodeError) as error:
            errors.append(f"{name}: cannot read ({error})")
            return ""

    version = read("VERSION").strip()
    if not re.fullmatch(SEMVER, version):
        return errors + ["VERSION must contain a release version such as 0.5.0 or 0.5.0-rc.1"]
    expected_tag = f"v{version}"
    if tag is not None and tag != expected_tag:
        errors.append(f"tag {tag!r} does not match {expected_tag}")

    def equal(name, actual, expected=version):
        if actual != expected:
            errors.append(f"{name}: expected {expected!r}, found {actual!r}")

    def matches(name, pattern, expected=version, minimum=1):
        values = re.findall(pattern, read(name), re.MULTILINE)
        if len(values) < minimum:
            errors.append(f"{name}: missing version metadata ({pattern})")
        for value in values:
            equal(name, value, expected)

    readme = read("README.md")
    matches("README.md", r"\[!\[Version: v([^]]+)\]")
    if f"https://img.shields.io/badge/version-v{version.replace('-', '--')}-blue" not in readme:
        errors.append("README.md: badge URL must match VERSION")
    matches("README.md", r"\*\*Version ([^*]+)\.\*\*")
    for name in ("README.md", "docs/quickstart.md", "infra/compose/docker-compose.registry.yml"):
        matches(name, r"PIPELOGIQ_VERSION=v([^\s`]+)", minimum=2 if name == "docs/quickstart.md" else 1)
    matches("docs/quickstart.md", r"defaults to `v([^`]+)`")
    matches("docs/index.md", r"current release target is Pipelogiq `v([^`]+)`")
    matches("docs/versioning.md", r"coordinated current release is \*\*([^ ]+) for all three\*\*")
    matches("docs/operations.md", r"ghcr\.io/pipelogiq/pipelogiq-migrate:v([^\s`]+)")
    matches("docs/openapi.external.yaml", r'^  version: "([^"]+)"')
    matches("docs/openapi.external.yaml", r'version: \{ type: string, example: v([^\s}]+)')
    matches("apps/go/internal/version/version.go", r'const DefaultVersion = "v([^"]+)"')

    for name in ("apps/web/package.json", "apps/web/package-lock.json"):
        try:
            data = json.loads(read(name))
            equal(name, data.get("version"))
            if name.endswith("lock.json"):
                equal(f"{name} root package", data.get("packages", {}).get("", {}).get("version"))
        except (ValueError, TypeError, AttributeError) as error:
            errors.append(f"{name}: invalid manifest ({error})")
    matches("infra/helm/pipelogiq/Chart.yaml", r'^version: (.+)$')
    matches("infra/helm/pipelogiq/Chart.yaml", r'^appVersion: "v([^"]+)"')
    for component in ("app", "worker", "migrate"):
        matches(f"infra/docker/Dockerfile.{component}", r"^ARG PIPELOGIQ_VERSION=(.+)$", expected_tag,
                minimum=1 if component == "migrate" else 2)
    for component in ("app", "worker", "build", "registry"):
        matches(f"infra/compose/docker-compose.{component}.yml", r"\$\{PIPELOGIQ_VERSION:-([^}]+)\}", expected_tag)
    if "VERSION ?= v$(shell cat VERSION)" not in read("Makefile"):
        errors.append("Makefile: default version must read VERSION")

    changelog = read("CHANGELOG.md")
    headings = list(re.finditer(r"^## \[([^]]+)\].*$", changelog, re.MULTILINE))
    releases = [(i, heading) for i, heading in enumerate(headings) if heading.group(1) != "Unreleased"]
    if not releases:
        errors.append("CHANGELOG.md: missing release section")
    else:
        i, heading = releases[0]
        equal("CHANGELOG.md first release", heading.group(1))
        end = headings[i + 1].start() if i + 1 < len(headings) else len(changelog)
        body = changelog[heading.end():end].strip()
        if not re.search(r"^- \S", body, re.MULTILINE):
            errors.append("CHANGELOG.md: release section has no change entries")

    notes_name = f"docs/releases/RELEASE_NOTES_{expected_tag}.md"
    notes = read(notes_name)
    if not notes.startswith(f"# Pipelogiq {expected_tag}\n"):
        errors.append(f"{notes_name}: missing exact version heading")
    for section in ("Highlights", "Breaking changes and upgrade requirements", "Validation and limitations"):
        if not re.search(rf"^## {re.escape(section)}\n\n\S", notes, re.MULTILINE):
            errors.append(f"{notes_name}: missing or empty {section} section")
    if len(notes.strip()) < 500:
        errors.append(f"{notes_name}: release notes are empty or incomplete")
    for name, value in (("Server and dashboard", version), (".NET SDK packages", version),
                        ("Java SDK modules", version), ("App, worker and migration image tags", expected_tag)):
        if f"| {name} | `{value}` |" not in notes:
            errors.append(f"{notes_name}: missing {name} version {value}")
    if f"](docs/releases/RELEASE_NOTES_{expected_tag}.md)" not in readme:
        errors.append("README.md: current release notes link is missing")
    return errors


def main():
    if len(sys.argv) > 2:
        print("usage: check-versions.sh [vVERSION]", file=sys.stderr)
        return 2
    errors = check(ROOT, sys.argv[1] if len(sys.argv) == 2 else None)
    if errors:
        print("\n".join(f"version check: {error}" for error in errors), file=sys.stderr)
        return 1
    print(f"versions agree: {ROOT.joinpath('VERSION').read_text().strip()}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
