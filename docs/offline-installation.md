# Offline installation

The Linux amd64 bundle contains the TUI executable, an amd64-compatible mihomo core, documentation, licenses and SHA-256 checksums. Installing and starting the default DIRECT configuration requires no network access, Go toolchain or separately installed mihomo. Subscription URLs, remote proxy providers and geography databases referenced by custom profiles may still require network access or locally supplied files. Optional GNOME integration uses the host's `gsettings` and `dconf`; services use the host's systemd.

Extract the archive, enter its directory and install:

```sh
tar -xzf mihomo-tui-0.1.0-linux-amd64.tar.gz
cd mihomo-tui-0.1.0-linux-amd64
sh install.sh --prefix "$HOME/.local/share/mihomo-tui" --mode user
"$HOME/.local/share/mihomo-tui/mihomo-tui"
```

Before installation, the script displays the selected mode, destination, bundled core, service availability and proxy access policy, then asks for confirmation. Answering no, or providing no input, cancels without creating the destination. For automation, `--yes` explicitly skips the confirmation.

User mode always checks for a working user systemd manager and reports whether lingering is enabled. When the manager is available, the default is to enable a user service; otherwise, the installer selects foreground operation. A working user manager without lingering does not guarantee operation after logout. Use `--no-service` to choose foreground operation even when systemd is available, or `--service` to require a service and fail if its manager is unavailable. System mode requires a working systemd manager and always uses a service.

After confirmation, the installer checks the bundle's hashes, imports its bundled core into the application's own version directory, initializes private credentials and creates a selected local DIRECT profile. The destination must not exist. Configuration is stored in `<prefix>/data`, with private directory and file permissions. Existing installations can import another core with `core import`, rather than rerunning the installer into the same directory.

For a user service, add `--service`:

```sh
sh install.sh --prefix "$HOME/.local/share/mihomo-tui" --mode user --service
systemctl --user status mihomo-tui.service
```

The installer requires a working user systemd manager before creating the unit in `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/`. It enables and starts the service. If the server does not provide a user manager, omit `--service` and keep the TUI running, or run `<prefix>/mihomo-tui run` under an available session manager. Background startup after logout requires the administrator to allow lingering (`loginctl enable-linger USER`); the installer does not change that policy.

For system mode and TUN:

```sh
sudo sh install.sh --prefix /opt/mihomo-tui --mode system --service
sudo /opt/mihomo-tui/mihomo-tui
```

System mode requires root and a system service. Its unit is written to `/etc/systemd/system/mihomo-tui.service`. Enable **TUN Mode** on Home after selecting your subscription. The unit grants network administration capabilities; actual TUN availability depends on the host kernel and container permissions. All local users can connect to `127.0.0.1:<mixed-port>` without proxy credentials, while only root manages this installation and its authenticated API. Run `sudo /opt/mihomo-tui/mihomo-tui status` to see the selected ports without exposing the API secret.

For a user-mode shell, export authenticated proxy variables with:

```sh
eval "$("$HOME/.local/share/mihomo-tui/mihomo-tui" env)"
```

These variables include the proxy password. The application does not print them during installation or include credentials in `status` output. Both installation modes use loopback listeners and select uncommon ports; startup checks TCP and UDP availability. A detected collision stops startup. To choose new ports while the core is stopped:

```sh
./mihomo-tui configure --install-mode user --mixed-port 43128 --api-port 43129
```

To replace a service's core, stop the service, import an extracted binary with a new version identifier, select it and restart the service:

```sh
systemctl --user stop mihomo-tui.service
./mihomo-tui core import vNEXT /path/to/mihomo
./mihomo-tui core use vNEXT
systemctl --user start mihomo-tui.service
```

Use `sudo` and omit `--user` for system mode. Imported binaries are checked by executing their version command and validating the current configuration; only import binaries you intend to run.

## Uninstallation

The installer copies `uninstall.sh` into the installation directory. Run it as the installing user, or with `sudo` for system mode:

```sh
sh "$HOME/.local/share/mihomo-tui/uninstall.sh"
sudo sh /opt/mihomo-tui/uninstall.sh
```

It checks the installation marker and verifies that any service unit belongs to this directory before disabling, stopping and removing it. A foreground core must be stopped by quitting the TUI or terminating its supervisor first. Any GNOME proxy backup is restored before files are removed; run from the same GNOME session when such a backup exists. Missing session access or externally changed proxy settings stops uninstallation and preserves files.

By default, the program and installed service are removed, while profiles, credentials and managed cores remain in `<prefix>/data`. Add `--purge` to remove the entire installation directory, including this data:

```sh
sh "$HOME/.local/share/mihomo-tui/uninstall.sh" --purge
sudo sh /opt/mihomo-tui/uninstall.sh --purge
```

After a data-preserving uninstall, use the script in the extracted offline bundle to remove retained data later:

```sh
sh uninstall.sh --prefix "$HOME/.local/share/mihomo-tui" --purge
```

The script refuses unmarked destinations, mode mismatches and service units that were modified or replaced. It does not alter administrator-managed lingering settings. Proxy variables already exported into your shell remain there; unset them when you no longer need them:

```sh
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY no_proxy
```

Packages installed before the uninstaller was introduced do not have an installation marker. To uninstall one, use `--legacy` with the script from this updated offline bundle and an explicit prefix. The script verifies the older installation layout and reads its mode through the bundled helper; service ownership, running-core and desktop-backup checks still apply:

```sh
sh uninstall.sh --prefix "$HOME/.local/share/mihomo-tui" --legacy
```

Add `--purge` to remove the legacy installation's data too. An arbitrary unmarked directory is still refused.

## Building an offline package

Developers can create a package using an already downloaded, extracted core:

```sh
sh scripts/package.sh /absolute/path/to/mihomo v1.19.32
```

The script builds a static Linux amd64 TUI using the project Go wrapper. It makes `dist/mihomo-tui-0.1.0-linux-amd64.tar.gz` and an archive checksum, without downloading a core or installing a service. The online GitHub installer and publication workflow are deferred.
