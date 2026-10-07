# Third-party software

The terminal layout and page behavior follow Clash Verge Rev, licensed under GPL-3.0:
https://github.com/clash-verge-rev/clash-verge-rev
The referenced source revision is documented in `docs/source-reference.md`.

Offline packages include an unmodified Mihomo Meta executable from MetaCubeX/mihomo, licensed under GPL-3.0:
https://github.com/MetaCubeX/mihomo
The exact bundled release identifier is in `CORE_VERSION`; official releases and source are available at https://github.com/MetaCubeX/mihomo/releases.

The TUI uses tcell (Apache-2.0), gdamore/encoding (Apache-2.0), go-colorful (MIT), clipperhouse/displaywidth and clipperhouse/uax29 (MIT), go.yaml.in/yaml (Apache-2.0 and MIT), and golang.org/x modules (BSD-3-Clause). Dependency versions are pinned in the project's `go.mod` and `go.sum`.
