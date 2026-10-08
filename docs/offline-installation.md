# Offline installation

The Linux amd64 bundle contains the TUI executable, an amd64-compatible mihomo core, full Country.mmdb, GeoIP.dat and GeoSite.dat databases, documentation, licenses and SHA-256 checksums. Installing, starting the default DIRECT configuration and validating GEOIP/GEOSITE rules requires no geography downloads, Go toolchain or separately installed mihomo. Subscription URLs, remote providers, ASN databases and other custom resources may still require network access or locally supplied files. Optional GNOME integration uses the host's `gsettings` and `dconf`; services use the host's systemd.

Extract the archive, enter its directory and install:

```sh
tar -xzf verge-tui-0.1.12-linux-amd64.tar.gz
cd verge-tui-0.1.12-linux-amd64
sh install.sh --prefix "$HOME/.local/share/verge-tui" --mode user
"$HOME/.local/share/verge-tui/verge-tui"
```

Before installation, the script displays the selected mode, destination, bundled core, service availability, proxy access policy and the shell rc to update, then asks for confirmation. Answering no, or providing no input, cancels without creating the destination or changing the rc. For automation, `--yes` explicitly skips the confirmation.

User mode always checks for a working user systemd manager and reports whether lingering is enabled. When the manager is available, the default is to enable a user service; otherwise, the installer selects foreground operation. A working user manager without lingering does not guarantee operation after logout. Use `--no-service` to choose foreground operation even when systemd is available, or `--service` to require a service and fail if its manager is unavailable. System mode requires a working systemd manager and always uses a service.

For a fresh installation, after confirmation the installer checks the bundle's hashes, imports its bundled core into the application's own version directory, initializes private credentials and creates a selected local DIRECT profile. Configuration is stored in `<prefix>/data`, with private directory and file permissions. An existing installation is updated instead of reinitialized.

For a user service, add `--service`:

```sh
sh install.sh --prefix "$HOME/.local/share/verge-tui" --mode user --service
systemctl --user status verge-tui.service
```

The installer requires a working user systemd manager before creating the unit in `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/`. It enables and starts the service. If the server does not provide a user manager, omit `--service` and keep the TUI running, or run `<prefix>/verge-tui run` under an available session manager. Background startup after logout requires the administrator to allow lingering (`loginctl enable-linger USER`); the installer does not change that policy.

For system mode and TUN:

```sh
sudo sh install.sh --prefix /opt/verge-tui --mode system --service
/opt/verge-tui/verge-tui
```

System installation and service management require root. The unit is written to `/etc/systemd/system/verge-tui.service`. Ordinary users can launch the TUI and manage shared subscriptions, profiles, nodes, delay tests, proxy mode, TUN and core versions while the service is running. These operations use a local Unix socket owned by the root service, without exposing its controller API token. Closing an ordinary user's TUI leaves the service running. Enable **TUN Mode** on Home after selecting your subscription. The unit grants network administration capabilities; actual TUN availability depends on the host kernel and container permissions. All local users can connect to `127.0.0.1:<mixed-port>` without proxy credentials. `verge-tui status` reports ports without printing the API secret.

Fresh system installations default to **7890** for HTTP/SOCKS proxy traffic and **9090** as the proposed external-controller port. External Controller is disabled by default, so that TCP port is not reserved or listened on; internal control uses a Unix socket in the private runtime directory. Settings provides the Verge-style switch, address and required secret, and restarts the core when they change. The shared TUI interface does not reserve another TCP port. Active proxy ports are checked before creating the installation; conflicts stop installation and leave the destination untouched. Use `--mixed-port PORT --api-port PORT` to override them. Fresh user installations retain random high ports unless explicitly overridden. Updating an installation preserves its current ports. Changing existing ports requires stopping the service first, then running `configure` and restarting it.

System settings, subscriptions and core files remain private. The data directory permits search access to its deliberately shared subdirectory, with a Unix socket and sticky directories containing private per-user shell registrations and desktop backups. The socket pathname must fit Linux's 107-byte Unix socket limit; use a shorter installation prefix if the service reports this limit. This limit also applies to the private core-control socket in user mode.

## Updating an existing installation

Extract the new offline bundle and rerun the original install command. For the default system installation:

```sh
sudo sh install.sh --mode system
```

For a custom directory, keep the original `--prefix`. Its recorded mode is detected automatically:

```sh
sudo sh install.sh --prefix /your/installation/directory
```

The installer displays **verge-tui update**, the detected mode, current core, destination and shell rc, then asks for confirmation. No installation files, shell rc or service state change before confirmation. The bundled helper is checksum-verified before it reads installation metadata. Declining or providing no input cancels the update. `--yes` skips confirmation explicitly.

Updates replace the application, uninstaller, documentation and licenses, while preserving subscriptions, selected profile and core, every managed core version, credentials, ports, proxy mode, TUN settings and any GNOME proxy backup. Missing or invalid geography databases are provisioned from the offline bundle; valid existing databases are retained. Geography changes are included in the update transaction and rolled back if the update fails. The bundled core is used for fresh installations; updating the application does not replace or select it. Use `core import` and `core use` separately to change the core.

Existing service configuration and enablement are preserved. A running service is stopped for replacement and restarted afterward; a stopped service stays stopped. Foreground cores must be stopped first, and an open TUI should be closed before updating. Updates inherit the registered shell rc and refresh the proxy env and function block without duplicates. `--shell none` skips rc changes and keeps any existing registration. Service or installation mode changes are refused during updates.

Failed replacement or service startup restores the previous application files, generated env and shell integration, and restarts a previously running service. If rollback itself fails, the installer reports the error and retains recovery files in the installation directory. Externally modified service units, moved installations, symlinked installation files and unrelated directories are rejected. Updates and uninstallation require the formal installation marker written by the installer; directories without this marker are rejected.

## Bundled geography databases

The installer places Country.mmdb, GeoIP.dat and GeoSite.dat in `<prefix>/data/runtime`, the same directory passed to the core during validation and operation. MMDB mode and `geodata-mode: true` are both supported. Full datasets are used to avoid removing countries or domain categories from subscriptions. These are data files rather than content embedded in the TUI executable; the core loads only the databases needed by the selected configuration.

The bundle includes a dated manifest with upstream SHA-256 digests, copied to `data/geodata-bundle.json` for reference. Valid custom databases are kept, including mihomo's alternative MMDB filenames `geoip.db` and `geoip.metadb`; this manifest describes the bundled snapshot, not any retained custom files. Invalid MMDB downloads are repaired in their original location. All runtime database files are private to the installation. Verification uses temporary copies and disabled download URLs, so checking a damaged file cannot delete or overwrite its live copy.

If a profile still needs other resources, such as an ASN database or remote providers, supply those files separately or allow the required download. A validation deadline reports **core validation timed out**, while a core parse failure reports **core rejected profile**; failed validation keeps the existing profile selection.

## Shell proxy functions

The installer prepares `<prefix>/proxy.env` and adds a marked block defining `verge_on`, `verge_off` and `verge_status` to the invoking shell's rc. It detects Bash or Zsh from the parent process, falling back to `$SHELL`. Zsh uses `${ZDOTDIR:-$HOME}/.zshrc`; Bash uses `$HOME/.bashrc`. The displayed rc path lets you check this choice before confirming. Override it with `--shell bash|zsh --rc-file /absolute/path`, or use `--shell none` to create the env file without changing an rc.

Open a new shell, or source the rc printed after installation, then use:

```sh
verge_on
verge_status
verge_off
```

`verge_on` sources the prepared file in the current shell. It sets lower- and upper-case HTTP, HTTPS, ALL_PROXY and NO_PROXY variables. `verge_off` clears them. The functions do not start or stop the core; clearing variables does not disable TUN. Proxy variables are not loaded automatically on shell startup.

`verge_status` queries the core API, checks the loopback proxy listener and compares each exported environment variable with the current installation. It reports unset, matching, empty and different variables without printing passwords or foreign proxy values. It does not contact an external test site or collect traffic statistics. Unexported shell variables are not visible to child processes.

User-mode env files have mode `0600`; passwords never appear in the rc block. System mode permits all users to read the credential-free `proxy.env`, execute the TUI and query shared status. Under `sudo`, the installer still registers the installing account's rc. Each ordinary user can register their own current Bash/Zsh rc without sudo:

```sh
/opt/verge-tui/verge-tui shell install
```

The command detects the current shell (including `ZDOTDIR`) and prints the rc to source. Override detection with `--rc-file /absolute/path`. Open a new shell or source the printed file before invoking the functions. To remove the calling user's registered functions:

```sh
/opt/verge-tui/verge-tui shell uninstall
```

Registrations are separate for each user; removing your functions leaves other users' rc files alone. Uninstallation removes all registered blocks, running ordinary users' cleanup with their own UID/GID. Existing rc ownership and permissions are preserved, including symlinked rc files. Loaded functions in already-open shells remain until explicitly unset or the shell exits.

Rerunning the installer also adds shell functions to early installations that did not have them. To register functions manually, for example in Zsh:

```sh
"$HOME/.local/share/verge-tui/verge-tui" env-file
"$HOME/.local/share/verge-tui/verge-tui" shell install --rc-file "${ZDOTDIR:-$HOME}/.zshrc"
. "${ZDOTDIR:-$HOME}/.zshrc"
```

Registering the same installation twice updates its block instead of duplicating functions. Uninstall removes only that installation's marked block, preserving other rc content. For a one-time export without registering functions:

```sh
eval "$("$HOME/.local/share/verge-tui/verge-tui" env)"
```

User-mode variables include the proxy password. The application does not print them during installation or include credentials in status output. Both installation modes use loopback listeners; startup checks TCP and UDP availability. A detected collision stops startup. The prepared env file is regenerated when you choose new ports while the core is stopped:

```sh
./verge-tui configure --install-mode user --mixed-port 43128 --api-port 43129
```

To replace a service's core, stop the service, import an extracted binary with a new version identifier, select it and restart the service:

```sh
systemctl --user stop verge-tui.service
./verge-tui core import vNEXT /path/to/mihomo
./verge-tui core use vNEXT
systemctl --user start verge-tui.service
```

For a running system service, ordinary users can instead import an offline core and select it without stopping the service:

```sh
/opt/verge-tui/verge-tui core import vNEXT /path/to/mihomo
/opt/verge-tui/verge-tui core use vNEXT
```

The client uploads the file using the calling user's permissions. The service imports it into its managed directory and restarts the core within the same supervisor when selected. The systemd service stays running. Service start/stop and installation/uninstallation still require root. Imported binaries are checked by executing their version command and validating the current configuration; only import binaries you intend to run.

## Uninstallation

From the extracted offline bundle, use the same mode and prefix as the installation command:

```sh
sh uninstall.sh --mode user
sudo sh uninstall.sh --mode system
sudo sh uninstall.sh --mode system --prefix /your/installation/directory
```

User mode defaults to `${XDG_DATA_HOME:-$HOME/.local/share}/verge-tui`; system mode defaults to `/opt/verge-tui`. With no explicit mode or prefix, root detects an existing default system installation; otherwise the user location is selected. Mode is verified against the installation marker. The destination never defaults to the extracted bundle itself.

The installer also copies `uninstall.sh` into the installation directory. Running this installed copy uses its own marked directory, including custom locations:

```sh
sh "$HOME/.local/share/verge-tui/uninstall.sh"
sudo sh /opt/verge-tui/uninstall.sh
```

The uninstaller displays the recorded mode, destination, service and data retention policy, then asks for confirmation. Answering no or providing no input cancels without stopping services or changing installation files or rc content. `--yes` skips confirmation for automation. It checks the installation marker and verifies that any service unit belongs to this directory before disabling, stopping and removing it. A foreground core must be stopped by quitting the TUI or terminating its supervisor first. GNOME proxy backups are restored and all registered users' shell rc blocks are removed before application files are deleted. Each ordinary user's desktop switch affects their own GNOME session and stores a private backup; cleanup runs with that user's UID/GID and the saved session address. Disable System Proxy in the user's TUI first if that GNOME session is no longer available. Missing session access, incomplete rc markers or externally changed proxy settings stops uninstallation and preserves application files.

By default, the program and installed service are removed, while profiles, credentials, managed cores and geography databases remain in `<prefix>/data`. Add `--purge` to remove the entire installation directory, including this data:

```sh
sh "$HOME/.local/share/verge-tui/uninstall.sh" --purge
sudo sh /opt/verge-tui/uninstall.sh --purge
```

To reinstall after keeping data, run the installer again using the same mode and prefix, for example `sudo sh install.sh --mode system`. It detects the retained installation, shows a reinstallation confirmation and restores the application, shell integration and service. Subscriptions, selected core, credentials, ports, proxy mode and TUN settings are preserved; a new default profile is not created. Retained ports must be available, and an unrelated service unit is never overwritten.

After a data-preserving uninstall, use the script in the extracted offline bundle to remove retained data later:

```sh
sh uninstall.sh --prefix "$HOME/.local/share/verge-tui" --purge
```

The script refuses unmarked destinations, mode mismatches and service units that were modified or replaced. It does not alter administrator-managed lingering settings. Functions and proxy variables already loaded into existing shells remain there; clear them with:

```sh
verge_off
unset -f verge_on verge_off verge_status
```

## Building an offline package

Developers can create a package using an already downloaded, extracted core:

```sh
sh scripts/package.sh /absolute/path/to/mihomo v1.19.32 /absolute/path/to/geodata
```

The geodata directory must contain Country.mmdb, GeoIP.dat and GeoSite.dat matching the hashes in `packaging/geodata/manifest.json`. The default directory is `.local/geodata-source`. The source release URLs are moving `latest` URLs; keep the verified snapshot files, or explicitly update the manifest when choosing a newer snapshot. Packaging verifies all hashes and tests all three formats with the supplied core before creating an archive.

The script builds a static Linux amd64 TUI using the project Go wrapper. It makes `dist/verge-tui-0.1.12-linux-amd64.tar.gz` and an archive checksum, without downloading assets or installing a service. The online GitHub installer and publication workflow are deferred.

Shell installation adds the executable directory to PATH in the same managed rc block as the proxy functions. It avoids duplicate entries; shell removal and uninstallation remove the entire block. Source the rc printed by the installer to activate it in the current shell.

In Settings, System Proxy and TUN each have separate `Configure | Disabled` / `Enabled` controls on one row. System Proxy configuration saves the proxy host and bypass choices privately for the calling user. On GNOME, saving updates an already-enabled managed proxy and keeps the original desktop backup for disabling or uninstalling. Changing only these settings does not change listening ports or TUN; the proxy address uses the managed mixed port. With no desktop session, settings are saved for later use in that user's supported desktop session. PAC and proxy guard are not provided.
