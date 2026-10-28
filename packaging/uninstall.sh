#!/bin/sh
set -eu
umask 077

usage() { echo 'Usage: sh uninstall.sh [--prefix DIR] [--mode user|system] [--purge] [--yes]'; }
fail() { echo "Error: $*" >&2; exit 1; }
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=
requested_mode=
purge=no
assume_yes=no
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix) [ "$#" -ge 2 ] || fail 'Missing --prefix value'; prefix=$2; shift 2 ;;
        --mode) [ "$#" -ge 2 ] || fail 'Missing --mode value'; requested_mode=$2; shift 2 ;;
        --purge) purge=yes; shift ;;
        --yes|-y) assume_yes=yes; shift ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; fail "Unknown option: $1" ;;
    esac
done
case "$requested_mode" in ''|user|system) ;; *) fail 'Mode must be user or system' ;; esac
if [ "$requested_mode" = system ]; then
    [ "$(id -u)" -eq 0 ] || fail 'System mode uninstallation requires root'
fi
if [ -z "$prefix" ]; then
    if [ -f "$script_dir/.verge-tui-install" ]; then
        prefix=$script_dir
    elif [ "$requested_mode" = system ] || { [ -z "$requested_mode" ] && [ "$(id -u)" -eq 0 ] && [ -d /opt/verge-tui ]; }; then
        prefix=/opt/verge-tui
    else
        prefix="${XDG_DATA_HOME:-$HOME/.local/share}/verge-tui"
    fi
fi
case "$prefix" in /*) ;; *) fail 'Prefix must be an absolute path' ;; esac
case "$prefix" in *'
'*|*"$(printf '\r')"*) fail 'Prefix must be a single-line path' ;; esac
[ -d "$prefix" ] && [ ! -L "$prefix" ] || fail 'Installation must be a real directory'
prefix=$(CDPATH= cd -- "$prefix" && pwd -P)
[ "$prefix" != / ] || fail 'Refusing to uninstall the root directory'
marker="$prefix/.verge-tui-install"
app_bin="$prefix/verge-tui"
[ -f "$marker" ] && [ ! -L "$marker" ] || fail 'No verge-tui installation marker'
[ "$(wc -l < "$marker")" -eq 4 ] || fail 'Invalid installation marker'
[ "$(sed -n '1p' "$marker")" = verge-tui-install-v1 ] || fail 'Unknown installation marker'
mode=$(sed -n '2p' "$marker")
unit_path=$(sed -n '3p' "$marker")
[ "$(sed -n '4p' "$marker")" = "$prefix" ] || fail 'Installation was moved; original service paths must be resolved first'
case "$mode" in user|system) ;; *) fail 'Invalid recorded installation mode' ;; esac
[ -z "$requested_mode" ] || [ "$requested_mode" = "$mode" ] || fail 'Requested mode does not match the installation'
if [ "$mode" = system ]; then
    [ "$(id -u)" -eq 0 ] || fail 'System mode uninstallation requires root'
    [ -z "$unit_path" ] || [ "$unit_path" = /etc/systemd/system/verge-tui.service ] || fail 'Unexpected system service path'
else
    if [ -n "$unit_path" ]; then
        case "$unit_path" in /*/systemd/user/verge-tui.service) ;; *) fail 'Unexpected user service path' ;; esac
    fi
fi

if [ -e "$app_bin" ] || [ -L "$app_bin" ]; then
    [ -f "$app_bin" ] && [ ! -L "$app_bin" ] && [ -x "$app_bin" ] || fail 'Installed application must be a real executable file'
else
    app_bin="$script_dir/bin/verge-tui"
    [ -f "$app_bin" ] && [ ! -L "$app_bin" ] && [ -x "$app_bin" ] || fail 'Use uninstall.sh from the extracted offline bundle to remove retained data'
    command -v sha256sum >/dev/null 2>&1 || fail 'Install sha256sum (GNU coreutils) first'
    (cd "$script_dir" && sha256sum --status -c SHA256SUMS) || fail 'Bundle checksum verification failed; uninstallation stopped'
fi

systemctl_mode() {
    if [ "$mode" = user ]; then systemctl --user "$@"; else systemctl "$@"; fi
}
if [ -n "$unit_path" ] && { [ -e "$unit_path" ] || [ -L "$unit_path" ]; }; then
    [ ! -L "$unit_path" ] || fail 'Service unit is a symlink; ownership cannot be verified'
    [ -f "$prefix/verge-tui.service" ] && [ ! -L "$prefix/verge-tui.service" ] || fail 'Original service unit is missing or a symlink; ownership cannot be verified'
    cmp -s "$prefix/verge-tui.service" "$unit_path" || fail 'Service unit differs from this installation; it will not be changed'
    command -v systemctl >/dev/null 2>&1 || fail 'Install systemd tools to stop and remove this service'
    systemctl_mode show-environment >/dev/null || fail 'Service manager is unavailable; no installation files have been removed'
fi

printf '\n%s\n' 'verge-tui uninstallation'
printf '  Mode:         %s\n  Destination:  %s\n' "$mode" "$prefix"
if [ -n "$unit_path" ]; then printf '  Service:      %s\n' "$unit_path"; else echo '  Service:      none'; fi
if [ "$purge" = yes ]; then
    echo '  Data:         remove profiles, credentials, cores and geography databases'
else
    echo '  Data:         keep profiles, credentials, cores and geography databases'
fi
echo "  Shell:        remove this installation's registered functions"
if [ "$assume_yes" != yes ]; then
    printf '\nProceed with uninstallation? [y/N] '
    if ! IFS= read -r answer; then echo; echo 'Uninstallation cancelled.'; exit 0; fi
    case "$answer" in y|Y|yes|YES|Yes) ;; *) echo 'Uninstallation cancelled.'; exit 0 ;; esac
fi
if [ -n "$unit_path" ] && [ -f "$unit_path" ]; then
    systemctl_mode disable --now verge-tui.service
fi

# Check the supervisor lock instead of trusting a stale PID, and restore any
# GNOME proxy backup before deleting application files or credentials.
"$app_bin" --data-dir "$prefix/data" prepare-uninstall
"$app_bin" --data-dir "$prefix/data" shell uninstall --all
"$app_bin" installation unlink --prefix "$prefix"
"$app_bin" installation retain --prefix "$prefix"

if [ -n "$unit_path" ] && [ -f "$unit_path" ]; then
    rm -- "$unit_path"
    systemctl_mode daemon-reload
fi
if [ "$purge" = yes ]; then
    rm -rf -- "$prefix"
    echo "Removed installation and data: $prefix"
else
    rm -f -- "$prefix/verge-tui" "$prefix/verge-tui.service" "$prefix/uninstall.sh" \
        "$prefix/README.md" "$prefix/LICENSE" "$prefix/NOTICE.md" "$prefix/DEPENDENCIES.txt" "$prefix/proxy.env"
    rm -rf -- "$prefix/docs" "$prefix/licenses" "$prefix/shell" "$prefix/img"
    # Forget the removed service so a later purge cannot affect a new install.
    printf '%s\n' verge-tui-install-v1 "$mode" '' "$prefix" > "$marker"
    echo "Application removed. Profiles, credentials and managed cores retained in $prefix/data"
    echo 'Use the offline bundle uninstall.sh with --prefix and --purge to remove retained data.'
fi
echo 'Shell rc integration removed. Already loaded functions and variables remain in existing shells.'
echo 'Bash/Zsh: verge-off; unset -f verge-on verge-off verge-status _verge_tui_message'
echo 'fish: verge-off; functions --erase verge-on verge-off verge-status _verge_tui_message'
