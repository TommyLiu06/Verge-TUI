# Offline installation

The Linux amd64 bundle contains the TUI executable, an amd64-compatible mihomo core, documentation, licenses and SHA-256 checksums. Installing and starting the default DIRECT configuration requires no network access, Go toolchain or separately installed mihomo. Subscription URLs, remote proxy providers and geography databases referenced by custom profiles may still require network access or locally supplied files. Optional GNOME integration uses the host's `gsettings` and `dconf`; services use the host's systemd.

Extract the archive, enter its directory and install:

```sh
tar -xzf mihomo-tui-0.1.0-linux-amd64.tar.gz
cd mihomo-tui-0.1.0-linux-amd64
sh install.sh --prefix "$HOME/.local/share/mihomo-tui" --mode user
"$HOME/.local/share/mihomo-tui/mihomo-tui"
```

The installer checks the bundle's hashes, imports its bundled core into the application's own version directory, initializes private credentials and creates a selected local DIRECT profile. The destination must not exist. Configuration is stored in `<prefix>/data`, with private directory and file permissions. Existing installations can import another core with `core import`, rather than rerunning the installer into the same directory.

For a user service, add `--service`:

```sh
sh install.sh --prefix "$HOME/.local/share/mihomo-tui" --mode user --service
systemctl --user status mihomo-tui.service
```

The installer requires a working user systemd manager before creating the unit in `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/`. It enables and starts the service. If the server does not provide a user manager, install without `--service` and keep the TUI running, or run `<prefix>/mihomo-tui run` under an available session manager. Background startup after logout requires the administrator to allow lingering (`loginctl enable-linger USER`); the installer does not change that policy.

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

Developers can create a package using an already downloaded, extracted core:

```sh
sh scripts/package.sh /absolute/path/to/mihomo v1.19.32
```

The script builds a static Linux amd64 TUI using the project Go wrapper. It makes `dist/mihomo-tui-0.1.0-linux-amd64.tar.gz` and an archive checksum, without downloading a core or installing a service. The online GitHub installer and publication workflow are deferred.
