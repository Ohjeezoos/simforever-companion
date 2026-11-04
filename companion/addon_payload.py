#!/usr/bin/env python3
"""Embed the TOC's runtime files and license in the companion."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def generate(source, output):
    toc = source / "SimforeverProbe.toc"
    if toc.is_symlink():
        raise ValueError("TOC must be a regular file")
    text = toc.read_text(encoding="utf-8")
    version = re.search(r"^## Version: ([0-9]+\.[0-9]+\.[0-9]+)$", text, re.M)
    if not version:
        raise ValueError("TOC release version is missing")
    names = [line.strip() for line in text.splitlines() if line.strip() and not line.startswith("#")]
    if not names or len(set(names)) != len(names):
        raise ValueError("TOC dependencies must be unique")
    if any(not re.fullmatch(r"[A-Za-z][A-Za-z0-9]*\.lua", name) for name in names):
        raise ValueError("TOC contains an unexpected path")
    names = [toc.name, *names, "LICENSE"]
    rows, payloads = [], []
    for index, name in enumerate(names):
        path = source / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"Missing or linked addon file: {name}")
        data = path.read_bytes()
        payloads.append(data)
        rows.append(f"static const unsigned char sf_probe_{index}[] = {{")
        for start in range(0, len(data), 24):
            rows.append("    " + ",".join(f"0x{value:02x}" for value in data[start:start + 24]) + ",")
        rows.append("};")
    if f'ns.VERSION = "{version[1]}"'.encode() not in (source / "Core.lua").read_bytes():
        raise ValueError("Core and TOC versions disagree")
    if b"MIT License" not in payloads[-1]:
        raise ValueError("Addon license is missing")
    rows.append(f'static const char sf_probe_version[] = "{version[1]}";')
    rows.append("static const AddonFile sf_probe_files[] = {")
    for index, (name, data) in enumerate(zip(names, payloads)):
        rows.append(f'    {{"{name}", sf_probe_{index}, {len(data)}, "{hashlib.sha256(data).hexdigest()}"}},')
    rows.extend(["};", ""])
    output.parent.mkdir(parents=True, exist_ok=True)
    content = "\n".join(rows)
    if not output.exists() or output.read_text(encoding="ascii") != content:
        with tempfile.NamedTemporaryFile(mode="w", encoding="ascii", dir=output.parent, delete=False) as staged:
            staged.write(content)
            path = Path(staged.name)
        try:
            os.replace(path, output)
        finally:
            path.unlink(missing_ok=True)
    return version[1]


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=ROOT / "addon/SimforeverProbe")
    parser.add_argument("--out", type=Path, default=ROOT / "companion/probe_payload.h")
    args = parser.parse_args()
    print(generate(args.source, args.out))
