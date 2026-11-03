# Application updates

Open **Settings → Check for Updates**. The entry follows Verge's advanced
settings and its version/release-notes/Update dialog. The three policy selectors
above the version information are specific to Verge-TUI:

- **Auto Check Updates**: Manual, a preset interval, or a custom interval of
  1–10080 minutes. Manual makes no scheduled GitHub requests.
- **Auto Download Updates**: Off or On. Requires automatic checking; downloads
  a detected newer release immediately, independently of automatic installation.
- **Auto Update**: Manual, or 1–1440 continuous minutes without active proxy
  connections. Requires automatic downloading.

Checking and installation default to Manual; automatic downloading defaults to
Off. A disabled parent disables its children, and the UI makes dependent controls
unavailable until their prerequisite is enabled. They are stored per installation in
`data/updater/options.json`, shared by all clients of a system installation.
Changing a system installation's policies requires administrator authorization.
The policies are not ordinary users' theme/language preferences.

The supervisor runs the scheduler while the service is running, even without
an open TUI. Without a service, the scheduler runs while the TUI or foreground
supervisor is open. A private lock elects one scheduler per installation.
Manual policies perform no network or connection polling. Automatic downloads
start after discovery even when automatic installation is Manual. Disabling the
download policy or its parent cancels an automatic transfer. Connection polling
starts at five-second intervals only when a verified package is ready and the
automatic installation policy is enabled.

Checking continues while a package is ready. If a newer version or changed
official digest supersedes it, the old archive is deleted and its ready state is
cleared. An enabled automatic download policy then downloads the current version.
Manual **Update** also rechecks GitHub before reusing a prepared archive.

Available-version indicators appear only in Settings → Check for Updates. The
settings page refreshes cached state without making additional GitHub requests;
Home displays no available-version badge and automatic discovery opens no dialog.

Active connections, changed traffic counters between samples, unavailable
controller responses, and gaps in polling restart the idle window. Completed
metadata requests establish fresh traffic baselines so their own proxy traffic
does not keep restarting the window; other active connections still do. The
latest release is revalidated near installation, without querying more than once
per minute when installation is postponed by pending edits. Saved policy
and connections are checked again before replacement. Pending TUI tasks and
unsaved editor/form input postpone a TUI-owned automatic installation. Network
and installation failures remain visible in the update dialog and impose a
15-minute automatic retry delay; manual checks can retry immediately.

## Downloads and verification

The updater reads the latest stable release from
`https://api.github.com/repos/TommyLiu06/Verge-TUI/releases/latest` and selects
`verge-tui-VERSION-OS-ARCH.tar.gz` with its `.sha256` file. Drafts, prereleases,
downgrades, unrelated asset URLs and missing GitHub SHA256 digests are rejected.
Checks and downloads use the running managed proxy, including user-mode proxy
authentication; otherwise the proxy environment is used. GitHub requests and
download redirects must use HTTPS.

The archive is streamed into a private staging directory. Its expected size,
GitHub asset digest, published checksum file, every `SHA256SUMS` entry, required
files, and executable version are checked before execution. Extraction rejects
absolute or parent paths, duplicate entries, links and oversized contents.
Before applying, the archive is verified and extracted again into a fresh private
directory. SHA256 verifies release consistency; this is not an independent
publisher signature.

## Applying an update

**Update** downloads and applies the release within the application flow. The
terminal is restored before sudo requests administrator authorization for a
system installation. A privileged downloader/apply endpoint is not exposed to
ordinary shared clients. They can request metadata and a verified staged download
from the system service; applying and changing policies require root.

Service-triggered updates run in an independent systemd transient unit, so the
proxy service's read-only filesystem sandbox and cgroup shutdown cannot terminate
the updater. This uses the system's existing `systemd-run` and service manager.
No permanent extra update service or timer is installed.

The verified bundle's installer uses the existing atomic replacement and rollback
transaction. It preserves installation mode, service configuration and original
service state, subscription files, credentials, preferences, shell registrations,
and the independently selected mihomo version. The TUI restarts after a manual
update; a TUI-owned foreground core is restarted with it. A completed update
refreshes public installation version metadata and removes the downloaded archive.

Useful commands:

```sh
verge-tui update status
verge-tui update check
verge-tui update configure --check-minutes 60 --auto-download --idle-minutes 10
verge-tui update configure --auto-download=false
verge-tui update configure --check-minutes 0
```

`update download` stages without installing. `update apply --version VERSION`
applies a staged version and expects any foreground core to have been stopped;
the TUI handles that ownership and restart sequence automatically.
