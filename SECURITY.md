# Security reports

Do not put access tokens, device credentials or player recordings in public
issues. For a suspected vulnerability, use this repository's private security
reporting when available. Otherwise ask the maintainer for a private contact
without posting exploit details or credentials.

Account credentials are stored with Windows DPAPI and are not passed to the
embedded page or upload worker. HTTPS checks remain enabled in release builds.
The fixture CA supported by collector tests is limited to test binaries.
