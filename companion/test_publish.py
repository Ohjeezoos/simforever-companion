#!/usr/bin/env python3
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("publish.sh")


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.stage, self.dest = self.root / "stage", self.root / "published"

    def stage_release(self, version="0.32.0", target="linux-amd64", macho=False):
        worker = b"worker fixture"
        blob = worker + b"SFCP1\n" + len(worker).to_bytes(8, "little") + hashlib.sha256(worker).digest()
        if macho:
            header = bytearray(32)
            header[:4] = b"\xcf\xfa\xed\xfe"
            header[16:20] = (1).to_bytes(4, "little")
            command = bytearray(152)
            command[:4] = (0x19).to_bytes(4, "little")
            command[4:8] = (152).to_bytes(4, "little")
            command[64:68] = (1).to_bytes(4, "little")
            command[72:82] = b"__sfworker"
            command[88:94] = b"__DATA"
            command[112:120] = len(blob).to_bytes(8, "little")
            command[120:124] = (184).to_bytes(4, "little")
            data = bytes(header + command) + blob + b"signature fixture"
        else:
            data = b"launcher fixture" + blob
        name = "simforever-companion-" + target + (".exe" if target.startswith("windows-") else "")
        binary = self.stage / version / name
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(data)
        self.manifest(version, target, data)
        return binary

    def manifest(self, version, target, data):
        path = self.stage / target / "latest.txt"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(f"SIMFOREVER-COMPANION-1\n{version}\n{len(data)}\n{hashlib.sha256(data).hexdigest()}\n")

    def publish(self, success):
        result = subprocess.run(["bash", str(SCRIPT), str(self.stage), str(self.dest)], capture_output=True, text=True)
        self.assertEqual(result.returncode == 0, success, result.stderr)
        return result

    def test_corruption_never_advertises_a_partial_release(self):
        self.stage_release(target="linux-amd64")
        broken = self.stage_release(target="windows-amd64")
        broken.write_bytes(b"corrupt")
        self.publish(False)
        self.assertFalse((self.dest / "linux-amd64/latest.txt").exists())

    def test_downgrade_keeps_current_channel(self):
        self.stage_release()
        self.publish(True)
        before = (self.dest / "linux-amd64/latest.txt").read_bytes()
        self.stage_release(version="0.31.0")
        self.assertIn("downgrade", self.publish(False).stderr)
        self.assertEqual((self.dest / "linux-amd64/latest.txt").read_bytes(), before)

    def test_immutable_release_cannot_be_replaced(self):
        binary = self.stage_release()
        self.publish(True)
        data = b"a different launcher" + binary.read_bytes()
        binary.write_bytes(data)
        self.manifest("0.32.0", "linux-amd64", data)
        self.assertIn("immutable", self.publish(False).stderr)

    def test_macho_section_and_invalid_offset(self):
        binary = self.stage_release(target="darwin-arm64", macho=True)
        self.publish(True)
        data = bytearray(binary.read_bytes())
        data[152:156] = (2**32 - 1).to_bytes(4, "little")
        binary.write_bytes(data)
        self.manifest("0.32.0", "darwin-arm64", data)
        self.assertIn("worker section", self.publish(False).stderr)


if __name__ == "__main__":
    unittest.main()
