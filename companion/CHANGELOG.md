# 0.40.0

- Linked Windows companions send a session's combat log after it has been
  unchanged for 15 minutes instead of 60 seconds, so a long session is no
  longer re-sent whole after every pause. The account service also forwards
  only the new lines of a re-sent log and charges the daily allowance for them.

# 0.39.0

- Install or update Simforever Probe only once the published addon gate clears
  that exact addon version for players. A held, changed or unreachable gate
  leaves the game folder untouched; a running companion checks again every five
  minutes and installs as soon as the version is cleared.

# 0.38.0

- Link the Windows companion to a Discord contributor account with a browser
  code. Show the account's points without changing automatic collection.
- Protect device credentials with Windows DPAPI. Keep them out of the embedded
  interface, command line and legacy uploader configuration.
- Send linked combat logs through the authenticated C service. Keep existing
  sharing preferences and support revocation from the website.

# 0.37.3

- Remove the redundant Close button. Settings shows only Cancel and Save,
  which return to the sync screen.

# 0.37.2

- Match the settings scrollbar to the navy/brass interface, with a narrow
  rounded thumb, transparent track and hover feedback. Fit settings in the
  normal window without scrolling.

# 0.37.1

- Avoid hashing growing combat logs for sync status. Wait for an upload receipt
  or the uploader's stability window before checking their contents.

# 0.37.0

- Start sharing automatically on fresh Windows installations. Remove the
  separate activation button and keep pause controls in settings.
- Show Ready to sync, Syncing or Synced based on current files and upload
  receipts. Reuse file hashes while their metadata is unchanged.
- Preserve existing sharing choices, including paused sharing and complete
  recordings kept locally.
- Prepare the matching download-page disclosure for the published 0.37 release.

# 0.36.0

- Install the companion and addon automatically when a valid Forever client is
  detected. Show folder selection only when detection fails.
- Restore the original navy/brass HTML interface with a C WebView2 host.
- Keep upload permission separate from installation and preserve prior consent.
- Create the GUI browser only when the window is opened. Closing it releases
  the browser while the background worker keeps running.

# 0.35.1

- Check and update the bundled addon once at companion startup. Remove periodic
  addon checks; log collection and uploads continue normally in the background.

# 0.35.0

- Add Windows setup with common-path detection across mounted drives, recursive
  folder selection and cancellable full-disk search.
- Validate Blizzard's actual Forever product metadata before installing.
- Install a per-user companion copy and Start menu shortcut; start silently at
  sign-in through a Startup shortcut that supports long command arguments.
- Keep tracking when the window closes or minimizes. Add a tray menu to reopen
  the window, change startup behavior or quit.
- Use a compact setup screen with sharing and startup preferences in settings.
- Preserve separately recorded complete-recording consent without prompting
  during background startup.

# 0.34.0

- Bundle Simforever Probe 0.35.0 and install it automatically in the Forever
  client's addon folder, including while offline.
- Keep newer addon versions and back up the previous runtime folder when
  updating or repairing an installation.
- Follow an installed `_forever_` client from an older saved beta path.
- Add `--no-addon`, `--install-addon` and `--addon-version` commands.

Upload consent, existing recordings and watch history are preserved.
