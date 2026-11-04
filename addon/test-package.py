#!/usr/bin/env python3
"""Exercise release boundaries using an addon tree with unwanted extra files."""
from pathlib import Path
import re
import shutil
import tempfile
import unittest
import zipfile

from package import ROOT, package


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "SimforeverProbe"
        shutil.copytree(ROOT / "SimforeverProbe", self.source)

    def test_only_runtime_and_license_are_shipped(self):
        for name in ("INTERNAL.md", "DEVELOPER.md", "README.md", "test.lua", "secret.env"):
            (self.source / name).write_text("must never ship")
        first = package(self.source, self.root / "first")
        second = package(self.source, self.root / "second")
        self.assertEqual(first.read_bytes(), second.read_bytes())
        with zipfile.ZipFile(first) as archive:
            names = archive.namelist()
            self.assertEqual(len(names), 10)
            self.assertTrue(all(name.startswith("SimforeverProbe/") for name in names))
            self.assertFalse(any(name.endswith((".md", ".env")) for name in names))
            self.assertNotIn("SimforeverProbe/test.lua", names)
            self.assertIn("SimforeverProbe/LICENSE", names)

    def test_invalid_paths_and_missing_dependencies_fail(self):
        toc = self.source / "SimforeverProbe.toc"
        initial = toc.read_text()
        for extra in ("../secret.lua", "README.md", "Absent.lua", "Core.lua"):
            with self.subTest(extra=extra):
                toc.write_text(initial + extra + "\n")
                with self.assertRaises(ValueError):
                    package(self.source, self.root / "out")

    def test_mismatched_version_fails(self):
        core = self.source / "Core.lua"
        core.write_text(re.sub(r'ns.VERSION = "[^"]+"', 'ns.VERSION = "999.0.0"', core.read_text()))
        with self.assertRaises(ValueError):
            package(self.source, self.root / "out")


if __name__ == "__main__":
    unittest.main()
