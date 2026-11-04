# Building and testing

Build on the target operating system. The packager executes the native
launcher and its tests, so setting a foreign target is not a cross-build.

Supported build targets:

| Target | Toolchain |
| --- | --- |
| `linux-amd64` | C11 compiler, libcurl, OpenSSL |
| `windows-amd64` | MSVC x64 developer terminal, Windows SDK |
| `windows-arm64` | MSVC ARM64 developer terminal, Windows SDK |
| `darwin-amd64` | macOS Intel, Xcode command-line tools |
| `darwin-arm64` | macOS Apple silicon, Xcode command-line tools |

Every target also needs Python 3.10+ and Go 1.25+. No private files, credentials
or access to the hosted backend are needed to build.

Addon packaging and harness checks need Lua 5.4, including `luac5.4`.
On Debian or Ubuntu, install it with `sudo apt-get install lua5.4` before
running the checks below.

```sh
go test ./platform/companion/... ./platform/probe/... ./platform/ingest/...
python3 companion/test_addon_payload.py
python3 companion/test_publish.py
python3 addon/test-package.py
(cd addon && lua5.4 test-harness.lua)
python3 companion/build.py --version 0.38.0 --target linux-amd64 --out dist
```

`build.py` regenerates `probe_payload.h` from the addon's TOC and `ui_payload.h`
from the local interface and dependency notices. Neither generated header is
checked in. It builds the existing upload worker, compiles the C launcher,
runs fixtures in temporary directories, and packages the executable.

Tests use synthetic recordings and isolated settings. Do not run an ordinary
companion session as a test: a normal Windows install shares recordings.
`-version`, `-dry-run`, `-show` and `--check-update` do not upload game data.

The update channel uses HTTPS plus expected size and SHA-256 checks. Published
binaries are not independently signed. Building locally does not publish an
update. Release versions are immutable, and changed binaries need a new
version before publication.

The shared parser retains its existing protocol definitions and measurement
types. `platform/store/measurement_types.go` contains only the existing data
structure needed to compile the parser; there is no database connection code.

The macOS packager ad hoc signs its Mach-O output after embedding the worker.
Developer ID signing and notarization are not configured. The workflow checks
native targets separately; a Linux build does not validate Windows or macOS.
