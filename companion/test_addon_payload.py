from pathlib import Path
import hashlib
import shutil
import tempfile
import unittest

from addon_payload import ROOT, generate


class AddonPayloadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.source = self.base / "SimforeverProbe"
        shutil.copytree(ROOT / "addon/SimforeverProbe", self.source)

    def test_runtime_identity_and_extra_files(self):
        (self.source / "DEVELOPER.md").write_text("never embed")
        (self.source / "private.env").write_text("never embed")
        output = self.base / "payload.h"
        version = generate(self.source, output)
        text = output.read_text()
        self.assertIn(f'"{version}"', text)
        self.assertNotIn("DEVELOPER.md", text)
        self.assertNotIn("private.env", text)
        toc = (self.source / "SimforeverProbe.toc").read_text()
        names = ["SimforeverProbe.toc", *[line for line in toc.splitlines() if line and not line.startswith("#")], "LICENSE"]
        for name in names:
            self.assertIn(hashlib.sha256((self.source / name).read_bytes()).hexdigest(), text)

    def test_bad_dependencies_and_mismatched_version_fail(self):
        toc = self.source / "SimforeverProbe.toc"
        original = toc.read_text()
        for extra in ["../outside.lua", "Core.lua", "README.md", "Missing.lua"]:
            with self.subTest(extra=extra):
                toc.write_text(original + extra + "\n")
                with self.assertRaises(ValueError):
                    generate(self.source, self.base / "bad.h")
        toc.write_text(original.replace("## Version:", "## Version: 999."))
        with self.assertRaises(ValueError):
            generate(self.source, self.base / "bad.h")


if __name__ == "__main__":
    unittest.main()
