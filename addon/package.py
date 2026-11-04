#!/usr/bin/env python3
"""Build a deterministic addon ZIP containing only runtime files and the license."""
import argparse
import hashlib
from pathlib import Path
import re
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parent


def package(source: Path, destination: Path) -> Path:
    toc = source / "SimforeverProbe.toc"
    text = toc.read_text(encoding="utf-8")
    match = re.search(r"^## Version: ([0-9]+\.[0-9]+\.[0-9]+)$", text, re.MULTILINE)
    if not match:
        raise ValueError("TOC must contain a numeric release version")
    version = match[1]
    files = [line.strip() for line in text.splitlines() if line.strip() and not line.startswith("#")]
    if not files or len(files) != len(set(files)):
        raise ValueError("TOC must list unique runtime files")
    for name in files:
        if not re.fullmatch(r"[A-Za-z][A-Za-z0-9]*\.lua", name):
            raise ValueError(f"Unexpected runtime path: {name}")
        path = source / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"Missing or linked runtime file: {name}")
        subprocess.run(["luac5.4", "-p", str(path)], check=True)
    core = (source / "Core.lua").read_text(encoding="utf-8")
    if f'ns.VERSION = "{version}"' not in core:
        raise ValueError("Core.lua and TOC versions disagree")
    license_path = source / "LICENSE"
    if license_path.is_symlink() or not license_path.is_file() or "MIT License" not in license_path.read_text():
        raise ValueError("MIT license is required")
    destination.mkdir(parents=True, exist_ok=True)
    result = destination / f"SimforeverProbe-{version}.zip"
    with zipfile.ZipFile(result, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name in ["SimforeverProbe.toc", *files, "LICENSE"]:
            entry = zipfile.ZipInfo("SimforeverProbe/" + name, date_time=(1980, 1, 1, 0, 0, 0))
            entry.create_system = 3
            entry.external_attr = 0o100644 << 16
            entry.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(entry, (source / name).read_bytes(), compresslevel=9)
    with zipfile.ZipFile(result) as archive:
        if archive.testzip():
            raise ValueError("ZIP integrity check failed")
    digest = hashlib.sha256(result.read_bytes()).hexdigest()
    result.with_suffix(".zip.sha256").write_text(f"{digest}  {result.name}\n", encoding="ascii")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=ROOT / "SimforeverProbe")
    parser.add_argument("--out", type=Path, default=ROOT.parent / "dist/addon")
    args = parser.parse_args()
    print(package(args.source.resolve(), args.out.resolve()))
