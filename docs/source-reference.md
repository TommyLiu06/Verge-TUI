# Clash Verge Rev source reference

Reference checkout: `.reference/clash-verge-rev` (shallow clone, excluded from this repository).
Upstream: https://github.com/clash-verge-rev/clash-verge-rev
Commit: `d01f43cc783a9dbd05f064b79ea3ce39dcbfce16`.

| Requested feature | Upstream source | TUI behavior |
| --- | --- | --- |
| Navigation | `src/pages/_navigation.tsx`, `src/components/layout/layout-sidebar.tsx` | Left sidebar; Home, Proxies, Profiles, Settings in upstream order |
| Home layout | `src/pages/home.tsx` | Profile / Current Node, Network Settings / Proxy Mode, Clash Info / System Info; two columns, one on narrow screens |
| Subscription information | `src/components/home/home-profile-card.tsx` | Selected profile, source host, update time, subscription usage and expiry |
| Current node | `src/components/home/current-proxy-card.tsx` | Profile-scoped group selection, selected member, delay check; GLOBAL in global mode, DIRECT in direct mode |
| Network settings | `src/components/home/proxy-tun-card.tsx` | System Proxy / TUN Mode tabs, observed status, switch |
| Proxy mode | `src/components/home/clash-mode-card.tsx` | Rule / Global / Direct choices, persisted core configuration |
| Clash information | `src/components/home/clash-info-card.tsx` | Core version, proxy address, mixed port, uptime, rules count |
| System information | `src/components/home/system-info-card.tsx` | OS info, auto launch, running mode, update check, app version |
| Proxy selection | `src/pages/proxies.tsx`, `src/components/proxy/proxy-groups.tsx`, `proxy-render.tsx`, `proxy-item.tsx`, `use-render-list.ts` | Mode selector, collapsible groups, node grid, selected indicator |
| Delay testing | `src/components/proxy/proxy-group-tools.tsx`, `src/services/delay.ts`, `src/utils/delay.ts`, `src/pages/_theme.tsx` | Group and per-node checks, configurable URL, four concurrent requests, immediate per-node updates, upstream color thresholds and default light/dark RGB palettes |
| Import URL | `src/pages/profiles.tsx` | Profile URL field followed by Import and New; import on Enter |
| New profile | `src/components/profile/profile-viewer.tsx`, `file-input.tsx` | Create Profile dialog, Remote / Local, name, description, URL or YAML file, user agent and request timeout |
| Profile selection | `src/pages/profiles.tsx` (`onSelect`), `src/components/profile/profile-item.tsx` | Click a profile card to select and reload it |
| Profile deletion | `src/components/profile/profile-item.tsx` (Delete menu, confirmation dialog), `src/pages/profiles.tsx` (`onDelete`) | Options menu, explicit confirmation; deleting the selected profile reloads the built-in DIRECT configuration |
| Settings layout | `src/pages/settings.tsx` | Two columns: System / Clash on the left, Verge Basic / Advanced on the right; single column on narrow terminals |
| System settings | `src/components/setting/setting-system.tsx`, `mods/tun-viewer.tsx`, `mods/stack-mode-switch.tsx` | GNOME proxy and system-mode TUN switches; TUN stack, device, route settings, MTU and DNS hijack dialog; systemd administration instructions |
| Clash settings | `src/components/setting/setting-clash.tsx`, `mods/clash-port-viewer.tsx`, `mods/dns-viewer.tsx`, `mods/controller-viewer.tsx`, `mods/network-interface-viewer.tsx`, `mods/clash-core-viewer.tsx` | LAN, IPv6, Unified Delay, log level, mixed/HTTP/SOCKS ports, profile-scoped DNS overwrite, on-demand interface viewer, external controller switch, address and secret, managed core operations and manual GeoData update |
| Basic settings | `src/components/setting/setting-verge-basic.tsx`, `mods/misc-viewer.tsx` | Verge's 13 languages with copied upstream translations, terminal-following and complete dark/light interface palettes, proxy environment, start page, grid columns, default test URL/timeout and terminal keyboard help |
| Advanced settings | `src/components/setting/setting-verge-advanced.tsx`, `mods/config-viewer.tsx` | On-demand redacted runtime configuration, directory paths, diagnostic information, exit and version |
| Interface languages | `src/services/i18n.ts`, `src/locales/*/{layout,home,profiles,proxies,shared,settings}.json` | Identical language choices, embedded upstream strings, English default and fallback; user data and raw runtime YAML are not translated |

This implementation recreates the requested structure in terminal cells. It does not run React, Tauri or a browser. Charts, connection tracking, log viewers, profile extension scripts, proxy chains and automatic subscription updates are outside this first implementation. Subscription usage is response metadata, not a traffic monitor. The new-profile form includes the basic fields requested here; advanced update scheduling and unsafe TLS options are omitted.

Settings expose working terminal-compatible controls, not placeholder switches. Service auto-launch is controlled by systemd; only root administers the system service. Directory controls show paths rather than opening desktop windows. Theme selection changes the interface while leaving latency colors independent; Default Follow Terminal preserves the terminal foreground/background. Modern colors are inspired by the official [VS Code dark](https://github.com/microsoft/vscode/blob/main/extensions/theme-defaults/themes/dark_modern.json) and [light](https://github.com/microsoft/vscode/blob/main/extensions/theme-defaults/themes/light_modern.json) palettes. DNS overwrites are profile scoped and preserve original YAML. The external TCP controller is disabled by default; its address and required secret are editable. A Unix socket in the private runtime directory keeps internal control available. Controller changes restart the managed core. Desktop-specific controls, Web UI, tunnel management, backups, startup scripts and automatic application updates are omitted. Supported core fields follow the [mihomo general configuration](https://wiki.metacubex.one/en/config/general/), [TUN configuration](https://wiki.metacubex.one/en/config/inbound/tun/) and [DNS configuration](https://wiki.metacubex.one/en/config/dns/); the application validates changes with the selected managed core before applying them.

Regenerate the trimmed translation catalogs from the pinned reference checkout with `python3 scripts/sync-locales.py`. The script uses only the Python standard library; catalogs are embedded in the executable, with no translation downloads at runtime.
