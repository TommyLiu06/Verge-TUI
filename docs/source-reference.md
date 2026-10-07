# Clash Verge Rev source reference

Reference checkout: `.reference/clash-verge-rev` (shallow clone, excluded from this repository).
Upstream: https://github.com/clash-verge-rev/clash-verge-rev
Commit: `d01f43cc783a9dbd05f064b79ea3ce39dcbfce16`.

| Requested feature | Upstream source | TUI behavior |
| --- | --- | --- |
| Navigation | `src/pages/_navigation.tsx`, `src/components/layout/layout-sidebar.tsx` | Left sidebar; Home, Proxies, Profiles in upstream order |
| Home layout | `src/pages/home.tsx` | Profile / Current Node, Network Settings / Proxy Mode, Clash Info / System Info; two columns, one on narrow screens |
| Subscription information | `src/components/home/home-profile-card.tsx` | Selected profile, source host, update time, subscription usage and expiry |
| Current node | `src/components/home/current-proxy-card.tsx` | Profile-scoped group selection, selected member, delay check; GLOBAL in global mode, DIRECT in direct mode |
| Network settings | `src/components/home/proxy-tun-card.tsx` | System Proxy / TUN Mode tabs, observed status, switch |
| Proxy mode | `src/components/home/clash-mode-card.tsx` | Rule / Global / Direct choices, persisted core configuration |
| Clash information | `src/components/home/clash-info-card.tsx` | Core version, proxy address, mixed port, uptime, rules count |
| System information | `src/components/home/system-info-card.tsx` | OS info, auto launch, running mode, update check, app version |
| Proxy selection | `src/pages/proxies.tsx`, `src/components/proxy/proxy-groups.tsx`, `proxy-render.tsx`, `proxy-item.tsx`, `use-render-list.ts` | Mode selector, collapsible groups, node grid, selected indicator |
| Delay testing | `src/components/proxy/proxy-group-tools.tsx`, `src/services/delay.ts` | Group delay check and per-node delay check, configurable test URL |
| Import URL | `src/pages/profiles.tsx` | Profile URL field followed by Import and New; import on Enter |
| New profile | `src/components/profile/profile-viewer.tsx`, `file-input.tsx` | Create Profile dialog, Remote / Local, name, description, URL or YAML file, user agent and request timeout |
| Profile selection | `src/pages/profiles.tsx` (`onSelect`), `src/components/profile/profile-item.tsx` | Click a profile card to select and reload it |
| English labels | `src/locales/en/{home,profiles,proxies,shared,settings}.json` | Corresponding page and action labels |

This implementation recreates the requested structure in terminal cells. It does not run React, Tauri or a browser. Charts, connection tracking, log viewers, profile extension scripts, proxy chains and automatic subscription updates are outside this first implementation. Subscription usage is response metadata, not a traffic monitor. The new-profile form includes the basic fields requested here; advanced update scheduling and unsafe TLS options are omitted.
