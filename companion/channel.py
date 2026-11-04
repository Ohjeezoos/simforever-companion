#!/usr/bin/env python3
"""Prepare public static files from checked companion release artifacts."""
import argparse
import hashlib
from pathlib import Path
import re
from build import TARGETS


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("dist", type=Path)
    p.add_argument("out", type=Path)
    p.add_argument("version")
    p.add_argument("--targets", nargs="+", choices=TARGETS, default=list(TARGETS))
    args = p.parse_args()
    if not re.fullmatch(r"(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})", args.version):
        p.error("version must be a numeric major.minor.patch")
    artifacts = []
    for target in args.targets:
        name = "simforever-companion-" + target + (".exe" if target.startswith("windows-") else "")
        data = (args.dist / name).read_bytes()
        if not data or len(data) > 128 * 1024 * 1024:
            p.error(f"invalid artifact size: {name}")
        digest = hashlib.sha256(data).hexdigest()
        artifacts.append((target, name, data, digest))
    sums = []
    for target, name, data, digest in artifacts:
        dest = args.out / args.version / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        if dest.exists() and dest.read_bytes() != data:
            p.error(f"refusing to replace an immutable release: {dest}")
        dest.write_bytes(data)
        manifest = f"SIMFOREVER-COMPANION-1\n{args.version}\n{len(data)}\n{digest}\n"
        channel = args.out / target / "latest.txt"
        channel.parent.mkdir(parents=True, exist_ok=True)
        channel.write_text(manifest, encoding="ascii")
        sums.append(f"{digest}  {name}\n")
    (args.out / args.version / "SHA256SUMS").write_text("".join(sums), encoding="ascii")


if __name__ == "__main__":
    main()
