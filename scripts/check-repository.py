#!/usr/bin/env python3
"""Reject credentials and generated executables in source, index or new commits.

Diagnostics contain paths and reasons only, never file contents.
"""
from __future__ import annotations
import argparse
import json
from pathlib import Path, PurePosixPath
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
LIMIT = 5 * 1024 * 1024
GENERATED = {"node_modules", "bin", "obj", "target", "dist", "artifacts", ".tools", ".cache", "__pycache__"}
EXECUTABLE_MAGICS = (b"\x7fELF", b"MZ", b"\xfe\xed\xfa\xce", b"\xce\xfa\xed\xfe", b"\xfe\xed\xfa\xcf", b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbe", b"\xbe\xba\xfe\xca")


def git(root, *args):
    return subprocess.run(["git", *args], cwd=root, check=True, capture_output=True).stdout


def policy(root):
    path = root / ".repository-policy.json"
    return json.loads(path.read_text()) if path.exists() else {}


def problem(path, data, config, symlink=False):
    parts = PurePosixPath(path).parts
    name = parts[-1]
    if symlink:
        return "symlinks are not permitted in published source"
    if any(part in GENERATED for part in parts):
        return "generated output must not be tracked"
    allowed_env = config.get("public_environment_files", [])
    template = name == ".env.example" or (name.startswith(".env.") and name.endswith(".example"))
    if name.startswith(".env") and not template and path not in allowed_env:
        return "local environment or backup file must not be tracked"
    if name.endswith((".p12", ".pfx", ".key", ".pem")):
        return "credential/key files must not be tracked"
    if len(data) > LIMIT:
        return "source file exceeds the 5 MiB limit"
    if data.startswith(EXECUTABLE_MAGICS):
        return "compiled executable must be a release artifact, not source"
    if name.endswith((".nupkg", ".snupkg", ".jar", ".class", ".dll", ".exe", ".so", ".dylib")):
        return "compiled package must not be tracked"
    private_markers = tuple(b"-----BEGIN " + kind + b"-----" for kind in (b"PRIVATE KEY", b"RSA PRIVATE KEY", b"PGP PRIVATE KEY BLOCK"))
    if any(line.strip() in private_markers for line in data.splitlines()):
        return "private signing key material must not be tracked"
    return None


def entries(root, staged=False):
    if staged:
        for entry in git(root, "ls-files", "--stage", "-z").split(b"\0"):
            if not entry:
                continue
            metadata, raw = entry.split(b"\t", 1)
            mode, oid, stage = metadata.split()
            path = raw.decode("utf-8", "surrogateescape")
            if stage != b"0":
                raise ValueError("unmerged index entry: " + path)
            yield path, git(root, "cat-file", "blob", oid.decode()), mode == b"120000"
    else:
        paths = set(git(root, "ls-files", "--cached", "--others", "--exclude-standard", "-z").split(b"\0"))
        for raw in sorted(paths - {b""}):
            path = raw.decode("utf-8", "surrogateescape")
            source = root / path
            if source.is_symlink():
                yield path, b"", True
            elif source.is_file():
                yield path, source.read_bytes(), False


def check(root, staged=False, history=None):
    config = policy(root)
    errors = []
    if history is None:
        for path, data, symlink in entries(root, staged):
            reason = problem(path, data, config, symlink)
            if reason:
                errors.append(f"{path}: {reason}")
    else:
        for commit in git(root, "rev-list", "--reverse", history).decode().splitlines():
            changed = git(root, "diff-tree", "--root", "--no-commit-id", "--name-only", "--diff-filter=ACMR", "-r", "-z", commit).split(b"\0")
            for raw in set(changed) - {b""}:
                path = raw.decode("utf-8", "surrogateescape")
                meta = git(root, "ls-tree", commit, "--", path).split(b" ", 1)[0]
                data = git(root, "show", f"{commit}:{path}")
                reason = problem(path, data, config, meta == b"120000")
                if reason:
                    errors.append(f"{commit[:12]} {path}: {reason}")
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--staged", action="store_true")
    group.add_argument("--history", metavar="REVISION_RANGE")
    args = parser.parse_args()
    try:
        errors = check(args.root, args.staged, args.history)
    except (OSError, ValueError, subprocess.CalledProcessError):
        print("Repository hygiene could not read repository metadata", file=sys.stderr)
        return 2
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print("Repository hygiene passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
