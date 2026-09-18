#!/usr/bin/env python3
"""Scan proposed source or exact staged/history content; redact every diagnostic."""
import argparse
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / f"{name}.py")
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--staged", action="store_true")
    group.add_argument("--history", nargs="?", const="--all", metavar="REVISION_RANGE")
    args = parser.parse_args()
    scanner = module("install-gitleaks").install(ROOT)
    common = ["--config", str(ROOT / ".gitleaks.toml"), "--redact", "--no-banner", "--no-color", "--log-level", "warn"]
    if args.staged:
        return subprocess.run([str(scanner), "git", "--pre-commit", "--staged", *common, str(ROOT)], cwd=ROOT).returncode
    if args.history is not None:
        return subprocess.run([str(scanner), "git", "--log-opts=" + args.history, *common, str(ROOT)], cwd=ROOT).returncode
    hygiene = module("check-repository")
    errors = hygiene.check(ROOT)
    if errors:
        print("Fix repository hygiene before scanning proposed source", file=sys.stderr)
        return 1
    # Scan only files eligible for Git, without ignored .env files, caches or artifacts.
    with tempfile.TemporaryDirectory(prefix="pipelogiq-secret-scan-") as directory:
        target = Path(directory)
        for path, data, symlink in hygiene.entries(ROOT):
            if symlink:
                raise ValueError("Refusing to follow source symlink")
            dest = target / path
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(data)
        return subprocess.run([str(scanner), "dir", *common, str(target)], cwd=ROOT).returncode


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError):
        print("Secret scanning failed before completion", file=sys.stderr)
        sys.exit(2)
