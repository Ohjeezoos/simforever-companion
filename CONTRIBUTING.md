# Contributing

Report a bug with the companion version, operating system and steps to
reproduce it. Remove account names, player names, device credentials and local
paths from screenshots and logs before posting them publicly.

Keep changes focused. Preserve existing sharing preferences, upload history,
startup behaviour and revocation support. Use C for new runtime features;
Python is used for build and packaging helpers. The Go worker is retained
while its functionality moves to C.

Run checks for the affected platform. Windows changes need a native Windows
build and its setup, DPAPI, discovery and tray lifecycle tests. Use temporary
settings and synthetic data; do not upload real recordings during tests.

Third-party notices and copyright statements must be preserved. Generated
payload headers, binaries, private settings and recordings do not belong in
source commits.
