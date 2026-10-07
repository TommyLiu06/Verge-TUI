#!/bin/sh
set -eu
umask 077

usage() {
    echo 'Usage: sh install.sh [--prefix DIR] [--mode user|system] [--service]'
}
fail() { echo "Error: $*" >&2; exit 1; }

bundle_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=
mode=user
install_service=no
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix) [ "$#" -ge 2 ] || fail 'Missing --prefix value'; prefix=$2; shift 2 ;;
        --mode) [ "$#" -ge 2 ] || fail 'Missing --mode value'; mode=$2; shift 2 ;;
        --service) install_service=yes; shift ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; fail "Unknown option: $1" ;;
    esac
done
case "$mode" in user|system) ;; *) fail 'Mode must be user or system' ;; esac
if [ "$mode" = system ]; then
    [ "$(id -u)" -eq 0 ] || fail 'System mode requires root'
    [ "$install_service" = yes ] || fail 'System mode requires --service'
    [ -n "$prefix" ] || prefix=/opt/mihomo-tui
else
    [ -n "$prefix" ] || prefix="${XDG_DATA_HOME:-$HOME/.local/share}/mihomo-tui"
fi
case "$prefix" in /*) ;; *) fail 'Prefix must be an absolute path' ;; esac
[ ! -e "$prefix" ] && [ ! -L "$prefix" ] || fail 'Destination already exists; use core import to add a version'

# Verify files before executing the bundled program or core.
command -v sha256sum >/dev/null 2>&1 || fail 'Install sha256sum (GNU coreutils) first'
cd "$bundle_dir"
sha256sum -c SHA256SUMS
core_id=$(cat CORE_VERSION)
case "$core_id" in ''|*[!A-Za-z0-9._-]*) fail 'Invalid bundled core identifier' ;; esac
case "$(uname -s)/$(uname -m)" in Linux/x86_64) ;; *) fail 'This bundle requires Linux amd64' ;; esac

unit_path=
if [ "$install_service" = yes ]; then
    command -v systemctl >/dev/null 2>&1 || fail 'Install systemd or omit --service in user mode'
    if [ "$mode" = user ]; then
        systemctl --user show-environment >/dev/null || fail 'User systemd manager is unavailable; install without --service'
        unit_path="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mihomo-tui.service"
    else
        systemctl show-environment >/dev/null || fail 'System systemd manager is unavailable'
        unit_path=/etc/systemd/system/mihomo-tui.service
    fi
    [ ! -e "$unit_path" ] && [ ! -L "$unit_path" ] || fail 'Service unit already exists'
fi

mkdir -m 700 -p -- "$prefix"
prefix=$(CDPATH= cd -- "$prefix" && pwd -P)
cp bin/mihomo-tui "$prefix/mihomo-tui"
chmod 700 "$prefix/mihomo-tui"
cp README.md LICENSE NOTICE.md "$prefix/"
cp -R docs "$prefix/docs"
cp DEPENDENCIES.txt "$prefix/"
cp -R licenses "$prefix/licenses"
app_bin="$prefix/mihomo-tui"
"$app_bin" configure --install-mode "$mode"
"$app_bin" core import "$core_id" "$bundle_dir/cores/mihomo"
profile_output=$("$app_bin" profile new Default)
profile_id=${profile_output%% *}
"$app_bin" profile select "$profile_id"
"$app_bin" service --mode "$mode" > "$prefix/mihomo-tui.service"

if [ "$install_service" = yes ]; then
    mkdir -p -- "$(dirname -- "$unit_path")"
    cp "$prefix/mihomo-tui.service" "$unit_path"
    chmod 600 "$unit_path"
    if [ "$mode" = user ]; then
        systemctl --user daemon-reload
        systemctl --user enable --now mihomo-tui.service
        echo 'User service enabled. Startup after logout requires administrator-approved lingering.'
    else
        systemctl daemon-reload
        systemctl enable --now mihomo-tui.service
        echo 'System service enabled. Open Home to enable TUN Mode.'
    fi
fi
echo "Installed in $prefix"
echo "Launch: \"$app_bin\""
