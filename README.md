# mihomo-tui

A lightweight English terminal interface following Clash Verge Rev's Home, Proxies and Profiles pages. The application owns its mihomo binaries and profile data.

The source mapping and pinned upstream revision are in [docs/source-reference.md](docs/source-reference.md).

Build with the globally installed official Go toolchain while keeping build data inside this project:

```sh
sh scripts/go.sh test ./...
sh scripts/go.sh build -trimpath -ldflags='-s -w' -o bin/mihomo-tui ./cmd/mihomo-tui
```

Implementation is in progress. Online installation is deferred; offline packaging will include the compiled application and a managed mihomo core. End users will not need Go.
