# mihomo-tui

A lightweight English terminal interface following Clash Verge Rev's Home, Proxies and Profiles pages. The application owns its mihomo binaries and profile data.

The source mapping and pinned upstream revision are in [docs/source-reference.md](docs/source-reference.md).

Build with the globally installed official Go toolchain while keeping build data inside this project:

```sh
sh scripts/go.sh test ./...
sh scripts/go.sh build -trimpath -ldflags='-s -w' -o bin/mihomo-tui ./cmd/mihomo-tui
```

The first release supports Linux. Home, Proxies and Profiles use the upstream sidebar, card order, page labels and basic forms. Mouse clicks, wheel scrolling, keyboard focus and bracketed paste are supported. Use a terminal with Unicode and mouse reporting; 120 × 42 shows all six Home cards, and narrower terminals use a single column.

The interface inherits the terminal's foreground and background, so light and dark themes both work. Accents use the terminal's ANSI palette; keyboard focus uses reverse video. Latency labels use Clash Verge Rev's default colors: green below 250 ms, blue from 250 to 399 ms, orange from 400 ms, and red for timeout or error. When `COLORFGBG` reports a dark background, the dark palette is used; otherwise the light palette is used. Focused latency controls keep their color and use bold underline. No background color is forced.

Run the application after importing a core:

```sh
./bin/mihomo-tui --data-dir "$PWD/.local/dev" core import v1.19.32 /path/to/mihomo
./bin/mihomo-tui --data-dir "$PWD/.local/dev"
```

Select **Start Core**, then open **Profiles** to import a subscription URL or create a local profile. Select the profile before choosing a node on **Proxies**. New local profiles default to DIRECT until you provide proxy configuration. Switching profiles validates and reloads the core. Importing a profile does not select it automatically. Each profile's **Options** menu includes **Delete**, followed by a confirmation. Deleting the selected profile validates and reloads the built-in DIRECT configuration before removing its YAML; a failed switch preserves the profile.

- `1`, `2`, `3`: Home, Proxies, Profiles.
- `Tab` / `Shift+Tab` or arrows: move focus; `Enter` / `Space`: activate.
- `PageUp` / `PageDown`: scroll; `Home` / `End`: jump to page edges.
- `F1` / `?`: shortcut help; `Esc`: close a dialog or return from help.
- `r`: refresh; `n`: new profile; `i`: focus the import field; `q`: quit.
- Mouse: select cards and controls, scroll pages and choice dialogs. Wheel scrolling clears keyboard focus, including at scroll boundaries; click or press `Tab` to focus again.
- Text fields: paste, arrows and `Home` / `End` to move the cursor, `Backspace` / `Delete` to edit, `Ctrl+U` to clear, `Ctrl+W` to delete a word. `Ctrl+S` saves a dialog.

The installer creates `<prefix>/proxy.env` and adds three functions to your Bash or Zsh rc:

```sh
mihomo_on      # Load this installation's proxy environment into the current shell.
mihomo_off     # Clear proxy and bypass environment variables in the current shell.
mihomo_status  # Check the core, proxy listener and exported environment separately.
```

Open a new shell or source the rc shown by the installer to load the functions in an existing shell. User-mode `proxy.env` is private and includes proxy credentials; rc files only contain paths. The environment file is refreshed when `configure` changes ports or installation mode. Status output never prints credentials. Clearing environment variables leaves the core and TUN running.

For a system installation, each ordinary user can register or remove the functions in their own current Bash/Zsh rc without sudo:

```sh
/opt/mihomo-tui/mihomo-tui shell install
/opt/mihomo-tui/mihomo-tui shell remove
```

`shell install` prints the rc to source. Use `--rc-file /absolute/path` to override detection. Registrations are kept separately per user. The root installer still registers its own rc; uninstallation removes all registered users' blocks while preserving unrelated settings and file ownership. It runs each user's cleanup with that user's privileges.

The UI is event driven: there are no periodic API requests or redraws while idle. Delay checks use at most four concurrent requests and update each node as its result arrives, with a completed/total counter. Each request has a five-second core timeout. Timeout and error results replace previous latencies, including after the final snapshot reload. Quitting cancels outstanding requests and waits for the workers to exit. Core logs are capped at 2 MiB per start. Traffic charts, connections and monitoring are excluded from this release. Core resource usage still depends on your subscription and mihomo configuration.

**Network Settings** supports GNOME System Proxy through `gsettings` and `dconf`, with a private backup restored when disabled. In a shared system installation this switch affects the calling user's GNOME session and keeps a separate backup per user. These host tools and a GNOME session are only required for that switch. On headless servers, use the displayed proxy environment or `eval "$(./mihomo-tui env)"`. User mode requires proxy credentials and keeps the API token private. Both listeners bind to loopback. User-mode ports are randomly chosen from 20000–59999; fresh system installations default to mixed port 7890 and controller API port 9090. Requested ports are checked before creating the installation, and TCP/UDP ports are checked again before startup. Override fresh-install ports with `--mixed-port` and `--api-port`. Updates preserve existing ports.

The core runs either under the TUI or under a systemd service. Quitting the TUI stops a core it started; a service keeps running. User services need an available user systemd manager. Continuing after logout or starting at boot requires lingering enabled by the server administrator. System mode runs with root service privileges, permits TUN and shares the loopback proxy without proxy credentials; the control API remains authenticated. All local users can run the TUI and manage shared profiles, nodes, delay checks, proxy mode, TUN and managed core versions through a local Unix socket. The service keeps its API secret and private files inaccessible to other users. The service must be running for shared management; only root installs, removes, starts or stops that service. The system TUI's **System Service** button shows the administrative commands. There are no polling loops in the shared management server.

Use **Core** to install or select a managed version. For an offline replacement:

```sh
./mihomo-tui core import vNEXT /path/to/extracted/mihomo
./mihomo-tui core use vNEXT
```

In system mode, ordinary users can import and select versions while the service is running. The binary is uploaded from the calling user's file permissions, and selecting a new version restarts the core within the same supervisor. For user services, stop the service before selecting a different core and restart it afterward. Core downloads use HTTP(S) proxy environment variables and verify the release SHA-256. Subscription downloads use a direct connection. The application stores binaries under `data/cores/<version>/mihomo`; no system-wide mihomo is used.

The installer displays the installation mode and checks user systemd availability and lingering before asking for confirmation. It selects a service when a working manager is available. Rerun the same command from a new offline bundle to update an existing installation: its configuration, subscriptions, credentials, core selection and service configuration are preserved. A running service is restarted; a stopped service stays stopped. Failed updates restore the previous application. Use `--no-service` for fresh foreground installations or `--yes` for automation. The included `uninstall.sh` removes the application and its own service, preserves data by default, and supports `--purge` to remove data too.

Offline bundles include full Country.mmdb, GeoIP.dat and GeoSite.dat snapshots. Installation and updates provision them in `data/runtime` so GEOIP and GEOSITE profiles do not require a geography download to validate or switch. Valid existing databases are retained; missing or invalid files are replaced atomically. The snapshot and hashes are recorded in `data/geodata-bundle.json`. Remote subscriptions, providers, ASN databases and other custom resources can still require network access. Validation timeouts are reported separately from invalid configurations.

To uninstall from the extracted bundle, use the original mode and prefix, for example `sudo sh uninstall.sh --mode system`. The script shows the destination and data policy before asking for confirmation. Data is kept unless `--purge` is specified. Installation markers are required for both updates and uninstallation.

See [offline installation](docs/offline-installation.md) for self-contained packages, user services, system services and uninstallation. Online script installation is deferred. End users do not need Go.
