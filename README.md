# verge-tui

A lightweight English terminal interface following Clash Verge Rev's Home, Proxies, Profiles, Connections, Rules, Logs and Settings pages. The application owns its mihomo binaries and profile data.

The source mapping and pinned upstream revision are in [docs/source-reference.md](docs/source-reference.md).

Build with the globally installed official Go toolchain while keeping build data inside this project:

```sh
sh scripts/go.sh test ./...
sh scripts/go.sh build -trimpath -ldflags='-s -w' -o bin/verge-tui ./cmd/verge-tui
cp -R packaging/shell bin/
```

The application supports Linux. All seven pages use the upstream sidebar, card order, page labels and basic forms. Mouse clicks, wheel scrolling, keyboard focus and bracketed paste are supported. Use a terminal with Unicode and mouse reporting; 120 × 42 shows all six Home cards, and narrower terminals use a single column.

The default Theme is Default Follow Terminal, preserving the terminal's foreground/background and ANSI accents. Default Dark, Default Light, Mist Dark/Light, Modern Dark/Light and Vivid Dark/Light apply complete interface palettes. Mist palettes are restrained; Vivid palettes have saturated accents and high contrast; Modern palettes take inspiration from VS Code. Theme selection never changes latency colors: green below 250 ms, blue from 250 to 399 ms, orange from 400 ms, and red for timeout or error. The latency palette follows COLORFGBG independently of Theme; focused latency controls keep their color and use bold underline.

Run the application after importing a core:

```sh
./bin/verge-tui --data-dir "$PWD/.local/dev" core import v1.19.32 /path/to/mihomo
./bin/verge-tui --data-dir "$PWD/.local/dev"
```

Select **Start Core**, then open **Profiles** to import a subscription URL or create a local profile. Select the profile before choosing a node on **Proxies**. New local profiles default to DIRECT until you provide proxy configuration. Switching profiles validates and reloads the core. Importing a profile does not select it automatically. Each profile's **Options** menu includes **Edit Info**, **Edit File** and **Delete**. Edit Info changes type, name, description, subscription URL and request timeout. Subscription downloads use the same User-Agent as the referenced Clash Verge Rev release. Edit File opens an internal YAML editor with line numbers, Unicode caret movement, mouse positioning, bracketed paste, indentation, undo/redo and save. `Ctrl+S` saves, `Ctrl+Z` undoes, `Ctrl+Y` redoes, Tab inserts two spaces and Shift+Tab moves focus between the editor and controls. Unsaved changes require a discard confirmation. Saving validates YAML and the selected managed core; an active profile reloads immediately. A failed save keeps the editor contents, and a changed file or profile revision requires reopening the editor. Delete asks for confirmation. Deleting the selected profile validates and reloads the built-in DIRECT configuration before removing its YAML; a failed switch preserves the profile.

- `1`–`7`: Home, Proxies, Profiles, Connections, Rules, Logs, Settings.
- `Tab` / `Shift+Tab` or arrows: move focus; `Enter` / `Space`: activate.
- `PageUp` / `PageDown`: scroll; `Home` / `End`: jump to page edges.
- `F1` / `?`: shortcut help; `Esc`: close a dialog or return from help.
- `r`: refresh; `n`: new profile; `i`: focus the import field; `q`: quit.
- Mouse: select cards and controls, scroll pages and choice dialogs. Wheel scrolling clears keyboard focus, including at scroll boundaries; click or press `Tab` to focus again.

The TUI requests mouse reporting and temporarily disables xterm alternate-scroll mode, which can translate the wheel into arrow keys. The previous mode is restored on exit. If a terminal still sends arrow keys for the wheel, enable its mouse reporting: identical arrow-key input cannot reliably be distinguished from a physical keyboard. In MobaXterm, check the terminal mouse reporting settings.
- Text fields: paste, arrows and `Home` / `End` to move the cursor, `Backspace` / `Delete` to edit, `Ctrl+U` to clear, `Ctrl+W` to delete a word. `Ctrl+S` saves a dialog.

The installer creates `<prefix>/proxy.env` and registers three functions for Bash, Zsh or fish:

```sh
verge_on      # Load this installation's proxy environment into the current shell.
verge_off     # Clear proxy and bypass environment variables in the current shell.
verge_status  # Check the core, proxy listener and exported environment separately.
```

Open a new shell or source the rc shown by the installer to load the functions in an existing shell. User-mode `proxy.env` is private and includes proxy credentials; rc files only contain paths. The environment file is refreshed when `configure` changes ports or installation mode. Status output never prints credentials. In a terminal, only state keywords are colored: enabled/true/healthy states use green, disabled/false/unavailable states use red. Labels and explanatory text keep the terminal colors. Redirected output, `TERM=dumb` and `NO_COLOR` disable colors. Clearing environment variables leaves the core and TUN running.

For a system installation, each ordinary user can register or remove the functions in their own Bash, Zsh or fish configuration without sudo:

```sh
/opt/verge-tui/verge-tui shell install
/opt/verge-tui/verge-tui shell uninstall
```

`shell install` detects the current Bash, Zsh or fish session, falling back to the login shell. Use `--shell bash|zsh|fish` and `--rc-file /absolute/path` to override detection. All function implementations ship as static files in `<prefix>/shell/` and are copied with the application. Bash/Zsh rc files only contain a short marked reference to `shell/proxy.sh`. fish registers a `conf.d/verge-tui.fish` reference to `shell/fish/init.fish`, which adds the bundled functions directory to `fish_function_path`; `config.fish` is not changed. No function implementations are generated in the user configuration directory. It respects `ZDOTDIR` and `XDG_CONFIG_HOME`. Functions load without starting the program or enabling the proxy. Existing unrelated files are never overwritten.

Registrations belong to the account that starts installation. Internal sudo elevation preserves that account and its chosen configuration path; registration runs with its UID/GID, rather than editing its files as root. An external sudo system installation also uses its initiating account. A direct root invocation uses root's configuration, including root user-mode installations. Uninstallation removes every registered user's owned loader and files while preserving unrelated content, ownership and permissions; cleanup runs with that user's privileges.

The UI is event driven. Home, Proxies, Profiles and Settings have no periodic API requests or redraws while idle. Connections polls once per second only while visible. Logs subscribes only while visible and resumed, batches display updates every 200 ms and retains the latest 1000 entries. Closed connection history is capped at 500 entries, matching Verge. Leaving a live page cancels its requests and timer; rules load on entry and explicit refresh. Only visible rows are drawn, and rule filters are cached. No new dependencies are required. Delay checks use at most four concurrent requests and update each node as its result arrives, with a completed/total counter. Each request defaults to a five-second core timeout, configurable in Settings. Timeout and error results replace previous latencies and survive subsequent snapshot reloads. Quitting cancels outstanding requests and waits for the workers to exit. Core logs are capped at 2 MiB per start. Traffic charts and background monitoring are excluded from this release. Core resource usage still depends on your subscription and mihomo configuration.

**Connections** follows Verge's downloaded/uploaded totals, Active/Closed tabs, list/table views, default/upload-speed/download-speed ordering, search and per-connection details. Close All affects active connections; Clear removes locally collected closed history. Tables support sortable headers, column visibility and horizontal controls; details include host, chains, rule, process, source, destination, type, counters and speeds. The history covers connections observed while the page is open.

**Rules** shows original line numbers, payload, type and target policy. Rule Provider opens provider details with behavior, transport, rule count and update time; individual providers or all providers can be updated. Updating all continues after individual failures and reports them together.

**Logs** provides Pause/Resume, oldest/newest ordering, exact severity filtering, search, complete wrapped entry details and Clear. Its feed respects the running core's log level; refresh reconnects the stream after an external change. All three page searches expose Verge's Match Case, Match Whole Word and Use Regular Expression options; invalid expressions show an error. Go's regular expressions use RE2 syntax. All pages work through the private management socket in system mode with the TCP External Controller disabled.

Delay testing runs independently of normal commands, so nodes, proxy mode, settings and profile editors remain usable while a test is running. Configuration mutations execute in order; private UI preferences use a separate queue, so Theme and Language can change during a subscription download. Queues are bounded to 32 pending actions and repeated pending actions are rejected without closing an open form. Selecting a profile or core, editing the active profile, or restarting the core cancels the old test. Old completions cannot restore earlier settings or overwrite newer latency results. Delay check failures appear on the affected nodes and in the status bar without opening an error dialog or interrupting an open form.

**Network Settings** supports GNOME System Proxy through `gsettings` and `dconf`, with a private backup restored when disabled. In a shared system installation this switch affects the calling user's GNOME session and keeps a separate backup per user. These host tools and a GNOME session are only required for that switch. On headless servers, use the displayed proxy environment or `eval "$(./verge-tui env)"`. User mode requires proxy credentials and keeps the API token private. All listeners initially bind to loopback; Allow LAN exposes only proxy listeners, while the authenticated controller stays on loopback. User-mode ports are randomly chosen from 20000–59999; fresh system installations default to mixed port 7890 and controller API port 9090. Requested ports are checked before creating the installation, and TCP/UDP ports are checked again before startup. Override fresh-install ports with `--mixed-port` and `--api-port`. Updates preserve existing ports.

The core runs either under the TUI or under a systemd service. Quitting the TUI stops a core it started; a service keeps running. User services need an available user systemd manager. Continuing after logout or starting at boot requires lingering enabled by the server administrator. System mode runs with root service privileges, permits TUN and shares the loopback proxy without proxy credentials; the control API remains authenticated. All local users can run the TUI and manage shared profiles, nodes, delay checks, proxy mode, TUN and managed core versions through a local Unix socket. The service keeps its API secret and private files inaccessible to other users. The service must be running for shared management; only root installs, removes, starts or stops that service. The system TUI's **System Service** button shows the administrative commands. The shared management server adds no background polling. Connections are requested by the visible client page; logs stream on demand without holding the service action lock.

Use **Core** to install or select a managed version. **Install Core** reads compatible stable and Alpha releases from the official `MetaCubeX/mihomo` GitHub releases API and displays a scrollable version list. Pages load only on entry and explicit Load More/Refresh; closing the dialog prevents old responses from reopening it. `verge-tui core available` lists the official compatible versions in the CLI. Alpha builds use their commit identifier so different builds can coexist. For an offline replacement:

```sh
./verge-tui core import vNEXT /path/to/extracted/mihomo
./verge-tui core use vNEXT
```

**Settings** follows the upstream two-column arrangement: System Setting and Clash Setting on the left, Verge Basic Setting and Verge Advanced Setting on the right. Narrow terminals stack the sections, with keyboard and mouse scrolling. Supported controls include:

- System Proxy and TUN each use one row with separate `Configure | Disabled` (or `Enabled`) controls. System Proxy configures the proxy host, default bypass list and custom bypass addresses for the calling user. Saving reconfigures an enabled managed desktop proxy immediately, preserving its original backup; failed writes restore the previous managed configuration. It uses the existing mixed port and does not change core listeners or TUN. TUN stack (system, gVisor, mixed), device, routing, MTU, DNS hijack and excluded routes. TUN requires system mode. Auto Launch shows the systemd commands; system service administration requires root.
- Allow LAN, IPv6, Unified Delay, Log Level, mixed/HTTP/SOCKS ports, managed core selection, online core installation and local core-file import. Update GeoData explicitly asks the running core to download updated databases; the offline bundle remains self-contained without using this action. Optional HTTP/SOCKS ports use 0 to disable them. Port changes check conflicts, reload the running core and refresh `proxy.env`; disable this user's GNOME System Proxy before changing ports, then enable it again afterward. Other users with an enabled GNOME proxy must re-enable it too. Existing shells need `verge_on` again to reload the new environment.
- DNS Overwrite with per-profile nameservers, bootstrap resolvers, fallback resolvers, IPv6, enhanced mode and fake-IP range. Saving enables the overwrite for that profile. Disabling restores the profile's DNS configuration; an editor opened for another active profile cannot apply its changes. Original subscription YAML is preserved.
- Start Page, proxy grid columns and default latency URL/timeout (100–60000 ms). A per-group Test URL takes precedence over the default URL. System-mode UI preferences are private to each user. Theme controls the complete interface palette, independently of latency colors. Language uses Verge's 13 choices and upstream translations; Theme, Language, Start Page and Log Level use scrollable lists with a current-selection indicator; click or press Enter to apply a choice immediately. Enum fields in forms (including profile type, TUN stack, DNS mode and proxy layout columns) open the same list; after choosing, save the form to apply its changes. English remains the default. TUI-specific wording uses bundled translations with English fallback.
- On-demand network interface and runtime configuration viewers, directory paths, diagnostic information, keyboard help and version information. Runtime configuration hides credentials and provider URLs. External Controller follows Verge: disabled by default, with editable address and required secret. Internal control uses a private Unix socket, so disabling the TCP controller keeps the TUI working. Controller changes restart the managed core.

Desktop tray/window controls, PAC mode, proxy guard, Web UI, tunnel management, backup/restore, automatic application updates, startup scripts and background monitoring are not included. Directory controls show paths inside the terminal instead of launching a file manager. Viewers load only when opened; settings add no background polling or new dependencies.

In system mode, ordinary users can import and select versions while the service is running. The binary is uploaded from the calling user's file permissions, and selecting a new version restarts the core within the same supervisor. For user services, stop the service before selecting a different core and restart it afterward. Official release queries and core downloads use the running managed proxy automatically, including user-mode proxy credentials. The authenticated core API and proxy listener must be reachable; otherwise HTTP(S) proxy environment variables apply. Once the managed proxy is selected, a failed request is reported without retrying directly. Downloads retain official-origin checks and release SHA-256 verification. Subscription downloads use a direct connection. The application stores binaries under `data/cores/<version>/mihomo`; no system-wide mihomo is used.

Run `./install.sh` without arguments to see account/platform/service information and choose User or System. Each option indicates Install, Update or Reinstall when data was retained. Public `installation.json` contains only discovery information; private state is verified after selecting a mode. Root may choose User, installing under root's local data directory. An ordinary user choosing System continues through sudo, which requires that account's sudo authorization. The script displays the final mode, service, paths and user-service lingering status before asking for confirmation. It selects a service when a working manager is available. Rerun the same command from a new offline bundle to update an existing installation: its configuration, subscriptions, credentials, core selection and service configuration are preserved. A running service is restarted; a stopped service stays stopped. Failed updates restore the previous application. Use `--no-service` for fresh foreground installations, or `--mode user|system --yes` for automation. System installations create `/usr/local/bin/verge-tui` pointing at the installed executable; `--bin-dir DIR` chooses a different directory for a fresh system installation. Updates and reinstalls preserve it. An occupied command path stops installation without overwriting it; uninstall removes the link only if it still targets this installation. The included `uninstall.sh` removes the application and its own service, preserves data by default, and supports `--purge` to remove data too.

Offline bundles include full Country.mmdb, GeoIP.dat and GeoSite.dat snapshots. Installation and updates provision them in `data/runtime` so GEOIP and GEOSITE profiles do not require a geography download to validate or switch. Valid existing databases are retained; missing or invalid files are replaced atomically. The snapshot and hashes are recorded in `data/geodata-bundle.json`. Remote subscriptions, providers, ASN databases and other custom resources can still require network access. Validation timeouts are reported separately from invalid configurations.

To uninstall from the extracted bundle, use the original mode and prefix, for example `sudo sh uninstall.sh --mode system`. The script shows the destination and data policy before asking for confirmation. Data is kept unless `--purge` is specified. Installation markers are required for both updates and uninstallation.

After uninstalling with data retained, rerun the installer with the same mode and prefix to restore the application and service. Reinstallation preserves subscriptions, selected core, credentials and ports, checks port availability, and asks for confirmation before making changes.

See [offline installation](docs/offline-installation.md) for self-contained packages, user services, system services and uninstallation. Online script installation is deferred. End users do not need Go.

`verge-tui shell install` only registers references to the bundled scripts. User mode adds the executable directory to PATH without duplicate entries. System mode uses its system command link and leaves PATH unchanged. Bash/Zsh source the printed rc; fish sources the printed `conf.d` loader and autoloads its functions from the installation directory. New shells load the integration automatically. `shell uninstall` removes only the calling user's references; `uninstall.sh` also removes the bundled scripts after removing all user registrations. fish universal variables are not modified.
