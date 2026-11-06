#!/usr/bin/env python3
"""Build the C launcher around the unchanged legacy upload worker."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

from addon_payload import generate
from ui_payload import generate as generate_ui

ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("windows-amd64", "windows-arm64", "darwin-amd64", "darwin-arm64", "linux-amd64")


def c_command(source, out, target, version, test=False, resource=None):
    defines = [f'SF_VERSION="{version}"', f'SF_TARGET="{target}"']
    if target.startswith("windows-"):
        args = ["cl", "/nologo", "/std:c11", "/utf-8", "/W4", "/O2"]
        args += ["/D" + d for d in defines]
        args += [str(source), "/Fe:" + str(out)]
        if resource:
            args += [str(resource)]
        sdk = ROOT / "companion/vendor/webview2" / ("arm64" if target.endswith("arm64") else "x64")
        args += [str(sdk / "WebView2LoaderStatic.lib")]
        args += ["/link", "winhttp.lib", "bcrypt.lib", "crypt32.lib", "user32.lib", "advapi32.lib", "shell32.lib", "ole32.lib", "uuid.lib", "gdi32.lib", "comctl32.lib", "version.lib", "shlwapi.lib"]
        if not test:
            args += ["/SUBSYSTEM:WINDOWS", "/ENTRY:wmainCRTStartup"]
        return args
    args = ["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-Wpedantic", "-O2"]
    args += ["-D" + d for d in defines]
    if target.startswith("darwin-"):
        args += ["-Wno-deprecated-declarations", "-arch", "arm64" if target.endswith("arm64") else "x86_64"]
    args += [str(source), "-o", str(out), "-lcurl"]
    if target.startswith("linux-"):
        args += ["-lcrypto", "-lm"]
    return args


def bundle(launcher, worker, out, target, version, tmp):
    if not target.startswith("darwin-"):
        subprocess.run([str(launcher), "--package", str(launcher), str(worker), str(out)], check=True)
        return
    empty, payload = tmp / "empty", tmp / "payload"
    empty.write_bytes(b"")
    if payload.exists():
        payload.unlink()
    subprocess.run([str(launcher), "--package", str(empty), str(worker), str(payload)], check=True)
    command = c_command(ROOT / "companion/launcher.c", out, target, version)
    command += ["-Xlinker", "-sectcreate", "-Xlinker", "__DATA", "-Xlinker", "__sfworker", "-Xlinker", str(payload)]
    subprocess.run(command, cwd=tmp, check=True)
    subprocess.run(["codesign", "--force", "--sign", "-", str(out)], check=True)
    subprocess.run(["codesign", "--verify", "--strict", str(out)], check=True)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--version", required=True)
    p.add_argument("--target", choices=TARGETS, required=True)
    p.add_argument("--out", type=Path, default=ROOT / "dist")
    args = p.parse_args()
    if not re.fullmatch(r"(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})", args.version):
        p.error("version must be a numeric major.minor.patch")
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=True)
    generate_ui()
    addon_version = generate(ROOT / "addon/SimforeverProbe", ROOT / "companion/probe_payload.h")
    suffix = ".exe" if args.target.startswith("windows-") else ""
    with tempfile.TemporaryDirectory(prefix="simforever-build-") as tmp:
        tmp = Path(tmp)
        worker, launcher, test = [tmp / (s + suffix) for s in ("worker", "launcher", "test")]
        env = os.environ.copy()
        env.update(CGO_ENABLED="0", GOOS=args.target.split("-")[0], GOARCH=args.target.split("-")[1])
        # The Windows worker is a console program started with CREATE_NO_WINDOW,
        # so the console tools it runs (reg.exe at startup) share its hidden
        # console instead of each opening a window that takes focus.
        flags = f"-s -w -X github.com/Ohjeezoos/simforever-companion/platform/companion.Version={args.version}"
        subprocess.run(["go", "build", "-trimpath", "-ldflags", flags, "-o", str(worker), "./platform/cmd/simforever-companion"], cwd=ROOT, env=env, check=True)
        if suffix:
            image = worker.read_bytes(); pe = int.from_bytes(image[0x3C:0x40], "little")
            if image[pe:pe + 4] != b"PE\0\0" or int.from_bytes(image[pe + 0x5C:pe + 0x5E], "little") != 3:
                raise RuntimeError("the Windows worker must be a console program, so its reg.exe lookups open no window")
        resource = None
        if suffix:
            resource = tmp / "companion.res"
            subprocess.run(["rc", "/nologo", "/I", str(ROOT / "companion"), "/fo", str(resource),
                            "/d", "SF_VERSION_COMMAS=" + args.version.replace(".", ",") + ",0",
                            "/d", 'SF_VERSION="' + args.version + '"', str(ROOT / "companion/companion.rc")], cwd=tmp, check=True)
        subprocess.run(c_command(ROOT / "companion/launcher.c", launcher, args.target, args.version, resource=resource), cwd=tmp, check=True)
        subprocess.run(c_command(ROOT / "companion/test_launcher.c", test, args.target, args.version, test=True), cwd=tmp, check=True)
        test_dir = tmp / "checks"
        subprocess.run([str(test), str(test_dir)], check=True)
        capture_test = tmp / ("capture-test" + suffix)
        subprocess.run(c_command(ROOT / "companion/test_capture.c", capture_test, args.target, args.version, test=True), cwd=tmp, check=True)
        subprocess.run([str(capture_test), str(tmp / "capture-checks")], check=True)
        addon_test = tmp / ("addon-test" + suffix)
        subprocess.run(c_command(ROOT / "companion/test_addon.c", addon_test, args.target, args.version, test=True), cwd=tmp, check=True)
        subprocess.run([str(addon_test), str(tmp / "addon-checks")], check=True)
        discovery_test = tmp / ("discovery-test" + suffix)
        subprocess.run(c_command(ROOT / "companion/test_discovery.c", discovery_test, args.target, args.version, test=True), cwd=tmp, check=True)
        subprocess.run([str(discovery_test), str(tmp / "discovery-checks")], check=True)
        if not suffix:
            logs_test = tmp / "logs-test"
            subprocess.run(c_command(ROOT / "companion/test_logs.c", logs_test, args.target, args.version, test=True), cwd=tmp, check=True)
            subprocess.run([str(logs_test), str(tmp / "logs-checks")], check=True)
        if suffix:
            contributor_test = tmp / "contributor-test.exe"
            subprocess.run(c_command(ROOT / "companion/test_contributor.c", contributor_test, args.target, args.version, test=True), cwd=tmp, check=True)
            subprocess.run([str(contributor_test), str(tmp / "contributor-checks")], check=True)
            sync_test = tmp / "sync-test.exe"
            subprocess.run(c_command(ROOT / "companion/test_sync.c", sync_test, args.target, args.version, test=True), cwd=tmp, check=True)
            subprocess.run([str(sync_test), str(tmp / "sync-checks")], check=True)
            desktop_test, desktop_worker = tmp / "desktop-test.exe", tmp / "desktop-worker.exe"
            for source, output in (("test_desktop.c", desktop_test), ("test_desktop_worker.c", desktop_worker)):
                subprocess.run(c_command(ROOT / "companion" / source, output, args.target, args.version, test=True), cwd=tmp, check=True)
            subprocess.run([str(desktop_test), str(tmp / "desktop-checks"), str(desktop_worker)], check=True)
        smoke_env = os.environ.copy()
        smoke_env["LOCALAPPDATA" if suffix else "XDG_CACHE_HOME"] = str(tmp / "cache")
        helper, args_bundle = tmp / ("args-worker" + suffix), tmp / ("args-bundle" + suffix)
        subprocess.run(c_command(ROOT / "companion/test_args.c", helper, args.target, args.version, test=True), cwd=tmp, check=True)
        bundle(launcher, helper, args_bundle, args.target, args.version, tmp)
        desktop_flags = ["--no-desktop"] if suffix else []
        cases = [
            ["-name", "--check-update", "-wow", 'C:\\Games with spaces\\', "-logs=false"],
            ["-name", 'a "quoted" name', "-config", "Παιχνίδι", ""],
            ["--name", "--from-update", "--", "--check-update", "x\\\\\"y"],
            ["-name", "--no-addon", "-logs=false"],
        ]
        for values in cases:
            result = subprocess.run([str(args_bundle), *desktop_flags, "--no-update", "--no-collect", "--no-addon", "--no-logs", *values], env=smoke_env, capture_output=True, check=True)
            got = [bytes.fromhex(line.decode("ascii")).decode("utf-8") for line in result.stdout.splitlines()]
            if got != values:
                raise RuntimeError(f"argument forwarding failed: {got!r} != {values!r}")
        # Exercise the parent/worker boundary, not just collector functions.
        # Explicit refusal plus an isolated cache prevents all fixture uploads.
        raw = b"SimforeverProbeDB={version='0.33.0',unknownFixture={answer=42}}"
        source = tmp / "probe.lua"
        source.write_bytes(raw)
        for name, extra, expect_capture in (
            ("normal", [], True),
            ("disabled", ["--no-collect"], False),
            ("diagnostic", ["-dry-run"], False),
        ):
            config = tmp / ("collector-" + name)
            config.mkdir()
            (config / "config.json").write_text(json.dumps({
                "consentGranted": True, "game": "forever",
                "apiBase": "https://forever.howtoguides.online", "watch": [str(source)],
            }))
            (config / "capture-consent.json").write_text(json.dumps({
                "approved": False,
                "endpoint": "https://forever.howtoguides.online/v1/games/forever/probe",
            }))
            subprocess.run([str(args_bundle), *desktop_flags, "--no-update", "--no-addon", "-config", str(config), *extra],
                           env=smoke_env, capture_output=True, check=True)
            captures = list((config / "recordings").glob("*.json"))
            if len(captures) != int(expect_capture):
                raise RuntimeError("collector launch/disable/diagnostic boundary failed")
            if captures:
                payload = json.loads(captures[0].read_bytes())
                if (base64.b64decode(payload["evidence"]["data"], validate=True) != raw or
                        payload["evidence"]["sha256"] != hashlib.sha256(raw).hexdigest() or
                        payload["capture"]["unknownFixture"]["answer"] != 42 or
                        list((config / "recordings").glob("*.ack"))):
                    raise RuntimeError("local evidence or refusal did not survive worker launch")
        gate_open, gate_hold = tmp / "addon-gate-open.txt", tmp / "addon-gate-hold.txt"
        gate_open.write_text(f"SIMFOREVER-ADDON-GATE-1\n{addon_version}\n")
        gate_hold.write_text("SIMFOREVER-ADDON-GATE-1\nhold\n")
        for mode, flags, expected, gate in (
            ("normal", [], True, gate_open),
            ("held", [], False, gate_hold),
            ("disabled", ["--no-addon"], False, gate_open),
            ("diagnostic", ["-dry-run"], False, gate_open),
        ):
            config = tmp / ("addon-" + mode)
            config.mkdir()
            game = config / "Game with spaces" / "_forever_"
            game.mkdir(parents=True)
            (game / ".flavor.info").write_text("Product Flavor!STRING:0\nwowf\n")
            (game / "Wow.exe").write_bytes(b"fixture")
            config_file = config / "config.json"
            content = json.dumps({"wowPath": str(game), "consentGranted": False, "watch": [], "watchLogs": []})
            config_file.write_text(content)
            subprocess.run([str(args_bundle), *desktop_flags, "--no-update", "--no-collect", "-config", str(config), *flags],
                           env={**smoke_env, "SIMFOREVER_ADDON_GATE_FILE": str(gate)}, capture_output=True, check=True)
            addon_dir = game / "Interface/AddOns/SimforeverProbe"
            if addon_dir.is_dir() != expected or config_file.read_text() != content:
                raise RuntimeError("packaged addon startup/gate/disable/diagnostic boundary failed")
            if expected:
                for source in (ROOT / "addon/SimforeverProbe").iterdir():
                    if source.is_file() and (addon_dir / source.name).read_bytes() != source.read_bytes():
                        raise RuntimeError("installed addon disagrees with the bundled source")
        out = args.out / ("simforever-companion-" + args.target + suffix)
        bundle(launcher, worker, out, args.target, args.version, tmp)
        if not suffix:
            out.chmod(0o755)
        for flags in (["-version"], ["--no-update", "-version"]):
            result = subprocess.run([str(out), *flags], env=smoke_env, text=True, capture_output=True, check=True)
            if result.stdout.strip() != "simforever-companion " + args.version:
                raise RuntimeError(f"version check failed: {result.stdout!r} {result.stderr!r}")
        got = subprocess.run([str(out), "--addon-version"], text=True, capture_output=True, check=True)
        if got.stdout.strip() != addon_version:
            raise RuntimeError("bundled addon version disagrees with its source")
        print(out)


if __name__ == "__main__":
    main()
