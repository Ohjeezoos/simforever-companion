# Simforever Companion

The desktop companion for [Simforever](https://simforever.ohjeez.eu/).
It installs the WoW Forever logging addon and uploads recordings while you play.

Download the Windows companion from the website. The current Windows x64 and
Linux x64 downloads are version 0.38.0. Other targets can be built from source.

## Windows

Open the downloaded executable. It finds WoW Forever, installs the addon and
starts sharing recorded logs. If detection fails, choose your game folder or
search all disks. Closing the window leaves collection running in the tray.
Choose Quit from the tray menu to stop it.

Data and startup settings lets you pause sharing, keep complete addon
recordings local, and turn off startup with Windows. Account linking is
optional. It opens the website to link uploads to your Discord account.

The companion checks for an update at startup. Addon updates are bundled with
companion releases and installed at startup. A running session uses its
current version until the next start.

## Recorded data

Uploads contain game combat logs and addon recordings, including recorded
player names, character stats, gear and combat events. Complete addon
recordings can include group members and inspected players. The companion
does not read Battle.net passwords. See [what we collect](https://simforever.ohjeez.eu/privacy/).

Windows stores settings in `%APPDATA%\simforever-companion` and installed
executables in `%LOCALAPPDATA%\simforever-companion`. Account credentials use
Windows DPAPI. Logs, upload receipts and sharing preferences survive updates.

## Build from source

Requires Python 3.10+, Go 1.25+ for the existing upload worker, and a C compiler.
The launcher, setup, updater, complete-recording collector and Windows account
integration are C. The shared parser and upload worker are Go; new development
belongs in C.

On Debian or Ubuntu, install a C compiler, `libcurl4-openssl-dev` and `libssl-dev`,
then build with:

```sh
python3 companion/build.py --version 0.38.0 --target linux-amd64 --out dist
```

On Windows, use an MSVC developer terminal with Python and Go on PATH:

```powershell
python companion/build.py --version 0.38.0 --target windows-amd64 --out dist
```

The build includes its native tests and packaged-worker checks. Windows needs
the Edge WebView2 Runtime to display the interface. Its SDK headers and loader
are vendored with their license. macOS uses the system Clang, libcurl and
CommonCrypto. See [BUILDING.md](BUILDING.md) for other targets and checks.

This source tree contains the companion, bundled addon, shared parsers and
their tests. It does not contain the hosted services or simulation engine.
The module path differs from the original platform checkout; these builds
are not expected to be byte-identical to the published 0.38.0 executables.

## License

MIT. See [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
