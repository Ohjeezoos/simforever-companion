# Third-party notices

The project uses the MIT license in `LICENSE`. Existing copyright notices
remain in place. Simforever-specific source is also distributed under MIT.

| Component | License and source |
| --- | --- |
| Shared protocol definitions | WoWSims, MIT; original notice in `LICENSE`, schema in `proto/common.proto` |
| cJSON 1.7.19 | MIT, `companion/vendor/LICENSE`; https://github.com/DaveGamble/cJSON |
| Microsoft WebView2 SDK 1.0.3537.50 | Redistribution terms in `companion/vendor/webview2/LICENSE.txt`; exact package URL and SHA-256 in `SOURCE.json` alongside it |
| Go WebView2 binding | MIT; https://github.com/jchv/go-webview2 |
| Go Windows loader | ISC; https://github.com/jchv/go-winloader |
| Go protobuf runtime | BSD 3-Clause; https://github.com/protocolbuffers/protobuf-go |
| Go system packages | BSD 3-Clause; https://cs.opensource.google/go/x/sys |

Go dependency versions and checksums are recorded in `go.mod` and `go.sum`.
Their complete license texts are retained in `licenses/`.
The Windows interface embeds the cJSON and WebView2 SDK notices. The Edge
WebView2 browser runtime is a separate Microsoft component installed on Windows.

World of Warcraft is a trademark of Blizzard Entertainment. This project is
not affiliated with or endorsed by Blizzard Entertainment.
