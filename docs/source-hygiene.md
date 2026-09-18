# Source hygiene and secret checks

Enable the checked-in hook once per clone:

```sh
git config core.hooksPath .githooks
```

The hook checks the exact Git index before committing. It rejects local environment
files and backups, private key files, generated directories, compiled packages,
files larger than 5 MiB and executable binaries even when they have no extension.
It also scans staged changes for credentials with redacted diagnostics. Only
explicit public environment templates/settings and exact public test fixtures
have narrow exceptions; a real credential added to a template still fails.

Run the same checks before reviewing a commit:

```sh
python3 scripts/check-repository.py
python3 scripts/check-secrets.py
python3 scripts/check-secrets.py --history
python3 -m unittest discover -s scripts -p 'test_repository_guards.py' -v
git diff --check
```

After staging, run `.githooks/pre-commit` directly to check the exact proposed
commit. `check-repository.py --history REVISION_RANGE` can also inspect files
introduced by every commit in a reviewed range, including subsequently deleted
files. Existing public history is not automatically rewritten by these tools.

Python 3 and Git are required. The secret scanner installer downloads the pinned
Gitleaks 8.30.1 archive from its official GitHub release, verifies its SHA-256,
and compares the installed executable with that archive before each use.
The cache is local and ignored under `.tools/`; the first run needs network
access and a failed verification stops the check. Both macOS and Linux on
x86-64/arm64 are supported. No source files are sent to a scanning service.

CI runs the repository guard, its regressions, and source/history secret scans.
Keep build output in ignored output directories and publish it as release
artifacts. Do not bypass a failing check by broadening exclusions. Rotate an
exposed credential in every environment using it; deleting the file or rewriting
Git history does not revoke that credential.
