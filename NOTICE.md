# Third-party software

The terminal layout and page behavior follow Clash Verge Rev, licensed under GPL-3.0:
https://github.com/clash-verge-rev/clash-verge-rev
The referenced source revision is documented in `docs/source-reference.md`.

Offline packages include an unmodified Mihomo Meta executable from MetaCubeX/mihomo, licensed under GPL-3.0:
https://github.com/MetaCubeX/mihomo
The exact bundled release identifier is in `CORE_VERSION`; official releases and source are available at https://github.com/MetaCubeX/mihomo/releases.

The TUI uses tcell (Apache-2.0), gdamore/encoding (Apache-2.0), go-colorful (MIT), clipperhouse/displaywidth and clipperhouse/uax29 (MIT), go.yaml.in/yaml (Apache-2.0 and MIT), and golang.org/x modules (BSD-3-Clause). Dependency versions are pinned in the project's `go.mod` and `go.sum`.

Offline bundles also include unmodified Country.mmdb, GeoIP.dat and GeoSite.dat assets from MetaCubeX/meta-rules-dat:
https://github.com/MetaCubeX/meta-rules-dat
Their snapshot, source URLs and SHA-256 digests are recorded in `geodata/manifest.json`. The upstream repository's GPL-3.0 license and the v2fly/domain-list-community MIT license are included in `licenses/`. Country.mmdb and GeoIP.dat originate from https://github.com/Loyalsoldier/geoip; domain data and upstream source acknowledgments are documented at https://github.com/MetaCubeX/meta-rules-dat#readme.

This product includes GeoLite2 data created by MaxMind, available from https://www.maxmind.com/.
