#!/bin/sh
set -eu
umask 077

usage() { echo 'Usage: sh uninstall.sh [--prefix DIR] [--mode user|system] [--purge] [--legacy]'; }
fail() { echo "Error: $*" >&2; exit 1; }
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=$script_dir
requested_mode=
purge=no
legacy=no
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix) [ "$#" -ge 2 ] || fail 'Missing --prefix value'; prefix=$2; shift 2 ;;
        --mode) [ "$#" -ge 2 ] || fail 'Missing --mode value'; requested_mode=$2; shift 2 ;;
        --purge) purge=yes; shift ;;
        --legacy) legacy=yes; shift ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; fail "Unknown option: $1" ;;
    esac
done
case "$prefix" in /*) ;; *) fail 'Prefix must be an absolute path' ;; esac
[ -d "$prefix" ] && [ ! -L "$prefix" ] || fail 'Installation must be a real directory'
prefix=$(CDPATH= cd -- "$prefix" && pwd -P)
[ "$prefix" != / ] || fail 'Refusing to uninstall the root directory'
marker="$prefix/.mihomo-tui-install"
app_bin="$prefix/mihomo-tui"
if [ "$legacy" = yes ] && [ ! -e "$marker" ] && [ ! -L "$marker" ]; then
    # Older bundles predate the marker and uninstall helper. Only an explicit
    # legacy request with the recognizable installation layout is accepted.
    for file in mihomo-tui mihomo-tui.service data/settings.json; do
        [ -f "$prefix/$file" ] && [ ! -L "$prefix/$file" ] || fail 'Destination does not match a legacy mihomo-tui installation'
    done
    for dir in data data/cores data/profiles data/runtime; do
        [ -d "$prefix/$dir" ] && [ ! -L "$prefix/$dir" ] || fail 'Destination does not match a legacy mihomo-tui installation'
    done
    app_bin="$script_dir/bin/mihomo-tui"
    [ -x "$app_bin" ] || fail 'Legacy uninstall requires the script and helper from the updated offline bundle'
    status=$("$app_bin" --data-dir "$prefix/data" status)
    mode=$(printf '%s\n' "$status" | sed -n 's/.*"InstallMode":"\([^"]*\)".*/\1/p')
    if [ "$mode" = user ]; then unit_path="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mihomo-tui.service"; else unit_path=/etc/systemd/system/mihomo-tui.service; fi
else
    [ -f "$marker" ] && [ ! -L "$marker" ] || fail 'No mihomo-tui installation marker; use --legacy only for an older offline installation'
    [ "$(sed -n '1p' "$marker")" = mihomo-tui-install-v1 ] || fail 'Unknown installation marker'
    mode=$(sed -n '2p' "$marker")
    unit_path=$(sed -n '3p' "$marker")
    [ "$(sed -n '4p' "$marker")" = "$prefix" ] || fail 'Installation was moved; original service paths must be resolved first'
fi
case "$mode" in user|system) ;; *) fail 'Invalid recorded installation mode' ;; esac
[ -z "$requested_mode" ] || [ "$requested_mode" = "$mode" ] || fail 'Requested mode does not match the installation'
if [ "$mode" = system ]; then
    [ "$(id -u)" -eq 0 ] || fail 'System mode uninstallation requires root'
    [ -z "$unit_path" ] || [ "$unit_path" = /etc/systemd/system/mihomo-tui.service ] || fail 'Unexpected system service path'
else
    if [ -n "$unit_path" ]; then
        case "$unit_path" in /*/systemd/user/mihomo-tui.service) ;; *) fail 'Unexpected user service path' ;; esac
    fi
fi

if [ ! -x "$app_bin" ]; then
    app_bin="$script_dir/bin/mihomo-tui"
    [ -x "$app_bin" ] || fail 'Use uninstall.sh from the extracted offline bundle to remove retained data'
fi

systemctl_mode() {
    if [ "$mode" = user ]; then systemctl --user "$@"; else systemctl "$@"; fi
}
if [ -n "$unit_path" ] && { [ -e "$unit_path" ] || [ -L "$unit_path" ]; }; then
    [ ! -L "$unit_path" ] || fail 'Service unit is a symlink; ownership cannot be verified'
    [ -f "$prefix/mihomo-tui.service" ] || fail 'Original service unit is missing; ownership cannot be verified'
    cmp -s "$prefix/mihomo-tui.service" "$unit_path" || fail 'Service unit differs from this installation; it will not be changed'
    command -v systemctl >/dev/null 2>&1 || fail 'Install systemd tools to stop and remove this service'
    systemctl_mode show-environment >/dev/null || fail 'Service manager is unavailable; no installation files have been removed'
    systemctl_mode disable --now mihomo-tui.service
fi

# Check the supervisor lock instead of trusting a stale PID, and restore any
# GNOME proxy backup before deleting application files or credentials.
"$app_bin" --data-dir "$prefix/data" prepare-uninstall
"$app_bin" --data-dir "$prefix/data" shell remove

if [ -n "$unit_path" ] && [ -f "$unit_path" ]; then
    rm -- "$unit_path"
    systemctl_mode daemon-reload
fi
if [ "$purge" = yes ]; then
    rm -rf -- "$prefix"
    echo "Removed installation and data: $prefix"
else
    rm -f -- "$prefix/mihomo-tui" "$prefix/mihomo-tui.service" "$prefix/uninstall.sh" \
        "$prefix/README.md" "$prefix/LICENSE" "$prefix/NOTICE.md" "$prefix/DEPENDENCIES.txt" "$prefix/proxy.env"
    rm -rf -- "$prefix/docs" "$prefix/licenses"
    # Forget the removed service so a later purge cannot affect a new install.
    printf '%s\n' mihomo-tui-install-v1 "$mode" '' "$prefix" > "$marker"
    echo "Application removed. Profiles, credentials and managed cores retained in $prefix/data"
    echo 'Use the offline bundle uninstall.sh with --prefix and --purge to remove retained data.'
fi
echo 'Shell rc integration removed. Already loaded functions and variables remain in existing shells.'
echo 'Clear them with: mihomo_off; unset -f mihomo_on mihomo_off mihomo_status'
