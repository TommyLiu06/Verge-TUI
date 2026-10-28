#!/bin/sh
set -eu
umask 022
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"
fail() { echo "Error: $*" >&2; exit 1; }
[ "$#" -ge 2 ] && [ "$#" -le 3 ] || fail 'Usage: sh scripts/package.sh /absolute/path/to/mihomo CORE_ID [GEODATA_DIR]'
core_path=$1
core_id=$2
geodata_dir=${3:-"$project_dir/.local/geodata-source"}
case "$core_path" in /*) ;; *) fail 'Core path must be absolute' ;; esac
case "$core_id" in ''|*[!A-Za-z0-9._-]*) fail 'Invalid core identifier' ;; esac
[ -f "$core_path" ] && [ -x "$core_path" ] || fail 'Core must be an executable file'
core_version=$("$core_path" -v)
case "$core_version" in *'Mihomo Meta '*linux\ amd64*) ;; *) fail 'Core must be Mihomo for Linux amd64' ;; esac

GOOS=linux GOARCH=amd64 GOAMD64=v1 sh scripts/go.sh build -trimpath -ldflags='-s -w' -o bin/verge-tui ./cmd/verge-tui
version=$(bin/verge-tui version)
bundle_name="verge-tui-$version-linux-amd64"
mkdir -p dist
staging_dir=$(mktemp -d "$project_dir/dist/.package.XXXXXX")
trap 'rm -rf -- "$staging_dir"' EXIT INT TERM
bundle_dir="$staging_dir/$bundle_name"
mkdir -p "$bundle_dir/bin" "$bundle_dir/cores" "$bundle_dir/geodata"
for file in Country.mmdb GeoIP.dat GeoSite.dat; do
    [ -f "$geodata_dir/$file" ] && [ ! -L "$geodata_dir/$file" ] || fail "Missing geodata file: $geodata_dir/$file (see packaging/geodata/manifest.json)"
    cp "$geodata_dir/$file" "$bundle_dir/geodata/$file"
done
cp packaging/geodata/manifest.json "$bundle_dir/geodata/manifest.json"
"$project_dir/bin/verge-tui" installation check-geodata --source-dir "$bundle_dir/geodata" --core-file "$core_path"
cp bin/verge-tui "$bundle_dir/bin/"
cp -R packaging/shell "$bundle_dir/"
cp "$core_path" "$bundle_dir/cores/mihomo"
chmod 755 "$bundle_dir/bin/verge-tui" "$bundle_dir/cores/mihomo"
cp packaging/install.sh "$bundle_dir/install.sh"
cp packaging/uninstall.sh "$bundle_dir/uninstall.sh"
chmod 755 "$bundle_dir/install.sh" "$bundle_dir/uninstall.sh"
cp README.md LICENSE NOTICE.md "$bundle_dir/"
cp -R img "$bundle_dir/img"
cp -R docs "$bundle_dir/docs"
mkdir "$bundle_dir/licenses"
cp packaging/geodata/*LICENSE "$bundle_dir/licenses/"
sh scripts/go.sh list -m -f '{{if .Version}}{{.Path}} {{.Version}} {{.Dir}}{{end}}' all > "$bundle_dir/DEPENDENCIES.txt"
while read -r module_name module_version module_dir; do
    [ -n "$module_name" ] || continue
    license_name=$(printf '%s-%s' "$module_name" "$module_version" | tr '/' '_')
    for license_file in LICENSE NOTICE; do
        if [ -f "$module_dir/$license_file" ]; then
            cp "$module_dir/$license_file" "$bundle_dir/licenses/$license_name-$license_file"
        fi
    done
done < "$bundle_dir/DEPENDENCIES.txt"
# Record dependency versions without embedding the developer's cache path.
cut -d ' ' -f 1,2 "$bundle_dir/DEPENDENCIES.txt" > "$bundle_dir/DEPENDENCIES.tmp"
mv "$bundle_dir/DEPENDENCIES.tmp" "$bundle_dir/DEPENDENCIES.txt"
printf '%s\n' "$core_id" > "$bundle_dir/CORE_VERSION"
cd "$bundle_dir"
sha256sum bin/verge-tui cores/mihomo geodata/* install.sh uninstall.sh CORE_VERSION DEPENDENCIES.txt README.md LICENSE NOTICE.md img/* docs/*.md licenses/* shell/proxy.sh shell/fish/init.fish shell/fish/functions/*.fish > SHA256SUMS
cd "$staging_dir"
tar -czf "$bundle_name.tar.gz" "$bundle_name"
sha256sum "$bundle_name.tar.gz" > "$bundle_name.tar.gz.sha256"
mv "$bundle_name.tar.gz" "$bundle_name.tar.gz.sha256" "$project_dir/dist/"
echo "Package: $project_dir/dist/$bundle_name.tar.gz"
