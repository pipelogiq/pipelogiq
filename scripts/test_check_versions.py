"""Regression coverage for release guards, using isolated copies of metadata."""
import importlib.util
from pathlib import Path
import re
import shutil
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("check_versions", Path(__file__).with_name("check-versions.py"))
VERSIONS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERSIONS)


class ReleaseVersionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.version = (VERSIONS.ROOT / "VERSION").read_text().strip()
        self.tag = f"v{self.version}"
        self.notes = f"docs/releases/RELEASE_NOTES_{self.tag}.md"
        for name in (*VERSIONS.CHECKED_FILES, self.notes):
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(VERSIONS.ROOT / name, target)

    def replace(self, name, old, new):
        path = self.root / name
        text = path.read_text()
        self.assertIn(old, text)
        path.write_text(text.replace(old, new))

    def test_current_release(self):
        self.assertEqual(VERSIONS.check(self.root, self.tag), [])

    def test_sdk_versions_can_differ_from_server_and_each_other(self):
        path = self.root / self.notes
        notes = path.read_text()
        for name, version in ((".NET SDK packages", "0.4.1"), ("Java SDK modules", "0.5.0")):
            notes = re.sub(rf"(\| {re.escape(name)} \| )`[^`]+`", rf"\g<1>`{version}`", notes)
        path.write_text(notes)
        self.assertEqual(VERSIONS.check(self.root, self.tag), [])

    def test_sdk_pairing_requires_one_explicit_release_version(self):
        path = self.root / self.notes
        original = path.read_text()
        for name in (".NET SDK packages", "Java SDK modules"):
            row = re.search(rf"^\| {re.escape(name)} \| `[^`]+` \|$", original, re.MULTILINE).group()
            for replacement in ("", row + "\n" + row, f"| {name} | `latest` |"):
                with self.subTest(name=name, replacement=replacement):
                    path.write_text(original.replace(row, replacement))
                    self.assertTrue(any(name in error for error in VERSIONS.check(self.root)))

    def test_independent_sdk_versions_do_not_allow_server_drift(self):
        self.replace(self.notes, f"| Server and dashboard | `{self.version}` |",
                     "| Server and dashboard | `9.9.9` |")
        self.assertTrue(any("Server and dashboard" in error for error in VERSIONS.check(self.root)))

    def test_wrong_or_preview_tag(self):
        for tag in ("v9.9.9", f"{self.tag}-preview.1", self.version, "v.*"):
            with self.subTest(tag=tag):
                self.assertTrue(any("tag " in error for error in VERSIONS.check(self.root, tag)))

    def test_missing_notes(self):
        (self.root / self.notes).unlink()
        self.assertTrue(VERSIONS.check(self.root))

    def test_empty_notes(self):
        (self.root / self.notes).write_text(f"# Pipelogiq {self.tag}\n")
        self.assertTrue(any("incomplete" in error for error in VERSIONS.check(self.root)))

    def test_old_image_pin_after_first_valid_pin(self):
        self.replace("docs/quickstart.md", f"PIPELOGIQ_VERSION={self.tag}\n", "PIPELOGIQ_VERSION=v0.1.0\n")
        self.assertTrue(any("quickstart" in error for error in VERSIONS.check(self.root)))

    def test_image_and_chart_drift(self):
        for name, old, new in (
            ("infra/docker/Dockerfile.worker", f"PIPELOGIQ_VERSION={self.tag}", "PIPELOGIQ_VERSION=dev"),
            ("infra/compose/docker-compose.registry.yml", f"PIPELOGIQ_VERSION:-{self.tag}", "PIPELOGIQ_VERSION:-latest"),
            ("infra/helm/pipelogiq/Chart.yaml", f"version: {self.version}", "version: 0.1.0"),
        ):
            with self.subTest(name=name):
                self.replace(name, old, new)
                self.assertTrue(any(name in error for error in VERSIONS.check(self.root)))
                self.replace(name, new, old)

    def test_lock_root_package_drift(self):
        path = self.root / "apps/web/package-lock.json"
        data = VERSIONS.json.loads(path.read_text())
        data["packages"][""]["version"] = "1.0.0"
        path.write_text(VERSIONS.json.dumps(data))
        self.assertTrue(any("root package" in error for error in VERSIONS.check(self.root)))

    def test_empty_changelog(self):
        (self.root / "CHANGELOG.md").write_text(f"# Changelog\n\n## [Unreleased]\n\n## [{self.version}]\n\n## [0.1.0]\n- old change\n")
        self.assertTrue(any("no change entries" in error for error in VERSIONS.check(self.root)))

    def test_version_file_rejects_non_release_values(self):
        for value in ("dev", "0.5.0-SNAPSHOT", "01.5.0", "v0.5.0", "0.5.0\n0.6.0"):
            with self.subTest(value=value):
                (self.root / "VERSION").write_text(value)
                self.assertTrue(VERSIONS.check(self.root))


if __name__ == "__main__":
    unittest.main()
