#!/usr/bin/env python3
"""Embed the local companion interface in the C executable."""
from pathlib import Path
import os
import tempfile

def generate(source=None, output=None):
    root = Path(__file__).resolve().parent
    source, output = Path(source or root / "ui.html"), Path(output or root / "ui_payload.h")
    notices = "Microsoft WebView2 SDK\n\n" + (root / "vendor/webview2/LICENSE.txt").read_text(encoding="utf-8")
    notices += "\n\ncJSON\n\n" + (root / "vendor/LICENSE").read_text(encoding="utf-8")
    for license_file in sorted((root / "licenses").glob("*.txt")):
        notices += "\n\n" + license_file.stem + "\n\n" + license_file.read_text(encoding="utf-8")
    text = "/* Generated from ui.html and third-party notices. */\n"
    for name, data in (("sf_ui_html", source.read_bytes()), ("sf_ui_notices", notices.encode())):
        data += b"\0"
        text += "static const unsigned char " + name + "[] = {\n"
        text += "\n".join("    " + ",".join(str(b) for b in data[i:i + 32]) + "," for i in range(0, len(data), 32))
        text += "\n};\n"
    if output.exists() and output.read_text() == text:
        return
    with tempfile.NamedTemporaryFile(mode="w", dir=output.parent, delete=False) as tmp:
        tmp.write(text)
    os.replace(tmp.name, output)

if __name__ == "__main__":
    generate()
