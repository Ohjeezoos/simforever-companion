#!/usr/bin/env bash
set -euo pipefail
if [ "$#" -ne 2 ]; then
    echo 'usage: publish.sh STAGED_CHANNEL COMPANION_DIST' >&2
    exit 2
fi
mkdir -p -- "$2"
exec 9>"$2/.publish.lock"
flock 9
python3 - "$1" "$2" <<'PY'
import hashlib
import os
from pathlib import Path
import re
import sys
import tempfile

stage, root = (Path(p).resolve() for p in sys.argv[1:])
targets = ('windows-amd64', 'windows-arm64', 'darwin-amd64', 'darwin-arm64', 'linux-amd64')
version_re = re.compile(r'(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})')
releases = []
version = None
for target in targets:
    manifest = stage / target / 'latest.txt'
    if not manifest.exists():
        continue
    raw = manifest.read_bytes()
    if len(raw) > 255 or not raw.endswith(b'\n'):
        raise SystemExit('invalid manifest length or terminator')
    lines = raw.decode('ascii').splitlines()
    if len(lines) != 4 or lines[0] != 'SIMFOREVER-COMPANION-1' or not version_re.fullmatch(lines[1]):
        raise SystemExit('invalid manifest version or schema')
    v, size, digest = lines[1:]
    if not re.fullmatch(r'[0-9]+', size) or not 0 < int(size) <= 128 * 1024 * 1024 or not re.fullmatch(r'[0-9a-f]{64}', digest):
        raise SystemExit('invalid size or digest')
    if version is not None and version != v:
        raise SystemExit('mixed release versions')
    version = v
    name = 'simforever-companion-' + target + ('.exe' if target.startswith('windows-') else '')
    data = (stage / v / name).read_bytes()
    if len(data) != int(size) or hashlib.sha256(data).hexdigest() != digest:
        raise SystemExit('artifact does not match manifest: ' + name)
    payload = data
    if target.startswith('darwin-'):
        if len(data) < 32 or data[:4] != b'\xcf\xfa\xed\xfe':
            raise SystemExit('artifact is not a thin 64-bit Mach-O: ' + name)
        commands = int.from_bytes(data[16:20], 'little')
        if commands > 128:
            raise SystemExit('too many Mach-O commands')
        position, payload = 32, None
        for _ in range(commands):
            if position + 8 > len(data):
                raise SystemExit('truncated Mach-O command')
            kind = int.from_bytes(data[position:position+4], 'little')
            length = int.from_bytes(data[position+4:position+8], 'little')
            if length < 8 or position + length > len(data):
                raise SystemExit('invalid Mach-O command length')
            if kind == 0x19:
                if length < 72:
                    raise SystemExit('invalid Mach-O segment')
                count = int.from_bytes(data[position+64:position+68], 'little')
                if 72 + count * 80 > length:
                    raise SystemExit('invalid Mach-O sections')
                for i in range(count):
                    section = position + 72 + i * 80
                    if data[section:section+16].rstrip(b'\0') != b'__sfworker' or data[section+16:section+32].rstrip(b'\0') != b'__DATA':
                        continue
                    size = int.from_bytes(data[section+40:section+48], 'little')
                    offset = int.from_bytes(data[section+48:section+52], 'little')
                    if payload is not None or offset + size > len(data):
                        raise SystemExit('invalid or duplicate worker section')
                    payload = data[offset:offset+size]
            position += length
        if payload is None:
            raise SystemExit('Mach-O has no worker section')
    if len(payload) < 46 or payload[-46:-40] != b'SFCP1\n':
        raise SystemExit('artifact has no worker footer: ' + name)
    worker_size = int.from_bytes(payload[-40:-32], 'little')
    if not 0 < worker_size <= len(payload) - 46 or hashlib.sha256(payload[-46-worker_size:-46]).digest() != payload[-32:]:
        raise SystemExit('artifact has an invalid bundled worker: ' + name)
    old = root / target / 'latest.txt'
    if old.exists():
        prior = old.read_text(encoding='ascii').splitlines()
        if len(prior) != 4 or not version_re.fullmatch(prior[1]):
            raise SystemExit('existing channel is invalid; review before replacing it')
        if tuple(map(int, v.split('.'))) < tuple(map(int, prior[1].split('.'))):
            raise SystemExit('refusing a channel downgrade')
    dest = root / v / name
    if dest.exists() and dest.read_bytes() != data:
        raise SystemExit('refusing to overwrite immutable artifact: ' + name)
    releases.append((target, name, data, raw))
if not releases:
    raise SystemExit('no target manifests found')

root.mkdir(parents=True, exist_ok=True)

def atomic_write(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix='.publish-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp, 0o644)
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)

for target, name, data, raw in releases:
    dest = root / version / name
    if not dest.exists():
        atomic_write(dest, data)
# A partial release does not remove another target's checksum.
sum_file = root / version / 'SHA256SUMS'
entries = {}
if sum_file.exists():
    for line in sum_file.read_text(encoding='ascii').splitlines():
        digest, name = line.split('  ', 1)
        entries[name] = digest
for _, name, data, _ in releases:
    entries[name] = hashlib.sha256(data).hexdigest()
atomic_write(sum_file, ''.join(digest + '  ' + name + '\n' for name, digest in sorted(entries.items())).encode('ascii'))
for target, name, data, raw in releases:
    atomic_write(root / target / 'latest.txt', raw)
print(f'published companion {version}: ' + ', '.join(r[0] for r in releases))
PY
