"""Security boundary tests using disposable Git repositories and synthetic data."""
import importlib.util
from pathlib import Path
import secrets
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


GUARD = load("check-repository")


class RepositoryGuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="repo-guard-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "--quiet")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.root, check=True, capture_output=True).stdout

    def test_extensionless_executables(self):
        for magic in (b"\x7fELF", b"\xcf\xfa\xed\xfe", b"MZ"):
            with self.subTest(magic=magic):
                self.assertIn("executable", GUARD.problem("apps/go/worker", magic + b"fixture", {}))

    def test_environment_backup_blocked_but_template_allowed(self):
        for name in (".env", ".env.bak-112440", "nested/.env.production"):
            self.assertIsNotNone(GUARD.problem(name, b"value", {}))
        self.assertIsNone(GUARD.problem(".env.example", b"placeholder", {}))

    def test_explicit_public_environment_allowlist(self):
        config = {"public_environment_files": ["apps/web/.env.production"]}
        self.assertIsNone(GUARD.problem("apps/web/.env.production", b"VITE_API_BASE=/api", config))
        self.assertIsNotNone(GUARD.problem("apps/web/.env.production.bak", b"value", config))

    def test_generated_and_large_files(self):
        self.assertIsNotNone(GUARD.problem("sdk/target/small.txt", b"fixture", {}))
        self.assertIsNotNone(GUARD.problem(".cache/go-build/trim.txt", b"fixture", {}))
        self.assertIsNotNone(GUARD.problem("notes.txt", b"a" * (GUARD.LIMIT + 1), {}))

    def test_private_key_and_symlink(self):
        marker = b"-----BEGIN " + b"PRIVATE KEY-----\n"
        self.assertIsNotNone(GUARD.problem("notes.txt", marker, {}))
        self.assertIsNotNone(GUARD.problem("safe.txt", b"", {}, symlink=True))

    def test_index_content_cannot_be_hidden_by_clean_worktree(self):
        path = self.root / "worker"
        path.write_bytes(b"\x7fELFfixture")
        self.git("add", "worker")
        path.write_text("innocent working copy")
        self.assertEqual(GUARD.check(self.root), [])
        self.assertTrue(GUARD.check(self.root, staged=True))

    def test_ignored_but_tracked_backup_still_fails(self):
        (self.root / ".env.bak").write_text("placeholder")
        self.git("add", ".env.bak")
        (self.root / ".gitignore").write_text(".env*\n")
        self.assertTrue(GUARD.check(self.root))

    def test_deleted_secret_path_still_fails_history(self):
        (self.root / ".env.bak").write_text("fixture")
        self.git("add", ".env.bak")
        self.git("commit", "--quiet", "-m", "fixture")
        self.git("rm", ".env.bak")
        self.git("commit", "--quiet", "-m", "remove fixture")
        self.assertEqual(GUARD.check(self.root), [])
        self.assertTrue(GUARD.check(self.root, history="HEAD"))

    def test_scanner_detects_random_runtime_secret_without_printing_it(self):
        scanner = load("install-gitleaks").install(ROOT)
        credential = secrets.token_urlsafe(40)
        (self.root / "config.txt").write_text("JWT_SECRET=" + credential + "\n")
        result = subprocess.run([str(scanner), "dir", "--config", str(ROOT / ".gitleaks.toml"), "--redact", "--no-banner", str(self.root)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(credential, result.stdout + result.stderr)

    def test_fixture_allowlist_does_not_hide_real_secret(self):
        scanner = load("install-gitleaks").install(ROOT)
        path = self.root / ".env.example"
        path.write_text("JWT_SECRET=change-me-to-a-long-random-secret-at-least-32-characters\n")
        cmd = [str(scanner), "dir", "--config", str(ROOT / ".gitleaks.toml"), "--redact", "--no-banner", str(self.root)]
        self.assertEqual(subprocess.run(cmd, capture_output=True).returncode, 0)
        path.write_text("JWT_SECRET=" + secrets.token_urlsafe(40) + "\n")
        self.assertEqual(subprocess.run(cmd, capture_output=True).returncode, 1)


if __name__ == "__main__":
    unittest.main()
