#!/bin/sh
set -eu
umask 077

usage() {
    echo 'Usage: sh install.sh [--prefix DIR] [--mode user|system] [--service|--no-service] [--shell bash|zsh|none] [--rc-file FILE] [--yes]'
}
fail() { echo "Error: $*" >&2; exit 1; }

bundle_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=
mode=user
service_choice=auto
assume_yes=no
shell_choice=auto
rc_file=
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix) [ "$#" -ge 2 ] || fail 'Missing --prefix value'; prefix=$2; shift 2 ;;
        --mode) [ "$#" -ge 2 ] || fail 'Missing --mode value'; mode=$2; shift 2 ;;
        --service) service_choice=yes; shift ;;
        --no-service) service_choice=no; shift ;;
        --yes|-y) assume_yes=yes; shift ;;
        --shell) [ "$#" -ge 2 ] || fail 'Missing --shell value'; shell_choice=$2; shift 2 ;;
        --rc-file) [ "$#" -ge 2 ] || fail 'Missing --rc-file value'; rc_file=$2; shift 2 ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; fail "Unknown option: $1" ;;
    esac
done
case "$mode" in user|system) ;; *) fail 'Mode must be user or system' ;; esac
if [ "$mode" = system ]; then
    [ "$(id -u)" -eq 0 ] || fail 'System mode requires root'
    [ -n "$prefix" ] || prefix=/opt/mihomo-tui
else
    [ -n "$prefix" ] || prefix="${XDG_DATA_HOME:-$HOME/.local/share}/mihomo-tui"
fi
case "$prefix" in /*) ;; *) fail 'Prefix must be an absolute path' ;; esac
case "$prefix" in *'
'*|*"$(printf '\r')"*) fail 'Prefix must be a single-line path' ;; esac
[ ! -e "$prefix" ] && [ ! -L "$prefix" ] || fail 'Destination already exists; use core import to add a version'

if [ "$shell_choice" = auto ]; then
    parent_shell=
    if command -v ps >/dev/null 2>&1; then parent_shell=$(ps -p "${PPID:-0}" -o comm= 2>/dev/null) || parent_shell=; fi
    parent_shell=${parent_shell#-}
    login_shell=${SHELL:-}
    case "$parent_shell" in bash|zsh) shell_choice=$parent_shell ;; *) shell_choice=${login_shell##*/} ;; esac
fi
case "$shell_choice" in
    bash) [ -n "$rc_file" ] || rc_file="$HOME/.bashrc" ;;
    zsh) [ -n "$rc_file" ] || rc_file="${ZDOTDIR:-$HOME}/.zshrc" ;;
    none) [ -z "$rc_file" ] || fail '--rc-file cannot be combined with --shell none' ;;
    *) fail 'Cannot determine a supported shell; select --shell bash, zsh or none' ;;
esac
if [ -n "$rc_file" ]; then
    case "$rc_file" in /*) ;; *) fail 'Shell rc must be an absolute path' ;; esac
    case "$rc_file" in *'
'*|*"$(printf '\r')"*) fail 'Shell rc must be a single-line path' ;; esac
fi

# Verify files before executing the bundled program or core.
command -v sha256sum >/dev/null 2>&1 || fail 'Install sha256sum (GNU coreutils) first'
core_id=$(cat "$bundle_dir/CORE_VERSION")
case "$core_id" in ''|*[!A-Za-z0-9._-]*) fail 'Invalid bundled core identifier' ;; esac
case "$(uname -s)/$(uname -m)" in Linux/x86_64) ;; *) fail 'This bundle requires Linux amd64' ;; esac

manager_available=no
linger=unknown
if command -v systemctl >/dev/null 2>&1; then
    if [ "$mode" = user ]; then
        if systemctl --user show-environment >/dev/null 2>&1; then manager_available=yes; fi
        if command -v loginctl >/dev/null 2>&1; then
            if linger_value=$(loginctl show-user "$(id -u)" --property=Linger --value 2>/dev/null); then
                case "$linger_value" in yes|no) linger=$linger_value ;; esac
            fi
        fi
    elif systemctl show-environment >/dev/null 2>&1; then
        manager_available=yes
    fi
fi
install_service=$service_choice
if [ "$install_service" = auto ]; then install_service=$manager_available; fi

printf '\n%s\n' 'mihomo-tui installation'
printf '  Mode:         %s\n' "$mode"
printf '  Destination:  %s\n' "$prefix"
printf '  Bundled core: %s (Linux amd64)\n' "$core_id"
printf '  Proxy env:    %s/proxy.env\n' "$prefix"
if [ "$shell_choice" != none ]; then
    printf '  Shell:        %s\n  Shell rc:     %s\n' "$shell_choice" "$rc_file"
    echo '  Functions:    mihomo_on, mihomo_off, mihomo_status'
else
    echo '  Shell:        rc integration disabled'
fi
if [ "$mode" = user ]; then
    printf '  User systemd: %s\n' "$manager_available"
    case "$linger" in
        yes) echo '  After logout: supported (lingering enabled)' ;;
        no) echo '  After logout: service may stop (lingering disabled)' ;;
        *) echo '  After logout: unknown (lingering status unavailable)' ;;
    esac
fi
if [ "$install_service" = yes ]; then
    echo '  Background:   enabled systemd service'
else
    echo '  Background:   no service; keep the TUI or foreground core running'
fi
echo '  Proxy ports:  random high ports, checked before startup'
if [ "$mode" = user ]; then echo '  Proxy access: private credentials'; else echo '  Proxy access: shared on loopback; authenticated control API'; fi

if [ "$mode" = system ] && [ "$install_service" != yes ]; then
    fail 'System mode requires an available systemd manager and a service'
fi
[ "$install_service" != yes ] || [ "$manager_available" = yes ] || fail 'Requested service manager is unavailable'
unit_path=
if [ "$install_service" = yes ]; then
    if [ "$mode" = user ]; then unit_path="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mihomo-tui.service"; else unit_path=/etc/systemd/system/mihomo-tui.service; fi
    case "$unit_path" in /*) ;; *) fail 'Service configuration directory must be absolute' ;; esac
    case "$unit_path" in *'
'*|*"$(printf '\r')"*) fail 'Service path must be a single-line path' ;; esac
    [ ! -e "$unit_path" ] && [ ! -L "$unit_path" ] || fail 'Service unit already exists'
fi

if [ "$assume_yes" != yes ]; then
    printf '\nProceed with installation? [y/N] '
    if ! IFS= read -r answer; then echo; echo 'Installation cancelled.'; exit 0; fi
    case "$answer" in y|Y|yes|YES|Yes) ;; *) echo 'Installation cancelled.'; exit 0 ;; esac
fi
echo 'Verifying offline files...'
cd "$bundle_dir"
sha256sum --status -c SHA256SUMS || fail 'Bundle checksum verification failed; installation stopped'
echo 'Installing application and managed core...'

mkdir -m 700 -p -- "$prefix"
prefix=$(CDPATH= cd -- "$prefix" && pwd -P)
cp bin/mihomo-tui "$prefix/mihomo-tui"
chmod 700 "$prefix/mihomo-tui"
if [ "$mode" = system ]; then chmod 755 "$prefix" "$prefix/mihomo-tui"; fi
cp README.md LICENSE NOTICE.md "$prefix/"
cp -R docs "$prefix/docs"
cp DEPENDENCIES.txt "$prefix/"
cp -R licenses "$prefix/licenses"
cp uninstall.sh "$prefix/uninstall.sh"
printf '%s\n' mihomo-tui-install-v1 "$mode" "$unit_path" "$prefix" > "$prefix/.mihomo-tui-install"
app_bin="$prefix/mihomo-tui"
"$app_bin" configure --install-mode "$mode"
"$app_bin" core import "$core_id" "$bundle_dir/cores/mihomo" >/dev/null
profile_output=$("$app_bin" profile new Default)
profile_id=${profile_output%% *}
"$app_bin" profile select "$profile_id"
"$app_bin" service --mode "$mode" > "$prefix/mihomo-tui.service"
"$app_bin" env-file
if [ "$shell_choice" != none ]; then "$app_bin" shell install --rc-file "$rc_file"; fi

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
