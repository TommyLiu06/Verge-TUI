#!/bin/sh
set -eu
umask 077

usage() {
    echo 'Usage: sh install.sh [--prefix DIR] [--mode user|system] [--service|--no-service] [--mixed-port PORT] [--api-port PORT] [--shell bash|zsh|none] [--rc-file FILE] [--yes]'
}
fail() { echo "Error: $*" >&2; exit 1; }

bundle_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prefix=
mode=user
requested_mode=
service_choice=auto
assume_yes=no
shell_choice=auto
rc_file=
mixed_port=0
api_port=0
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix) [ "$#" -ge 2 ] || fail 'Missing --prefix value'; prefix=$2; shift 2 ;;
        --mode) [ "$#" -ge 2 ] || fail 'Missing --mode value'; mode=$2; requested_mode=$2; shift 2 ;;
        --service) service_choice=yes; shift ;;
        --no-service) service_choice=no; shift ;;
        --yes|-y) assume_yes=yes; shift ;;
        --shell) [ "$#" -ge 2 ] || fail 'Missing --shell value'; shell_choice=$2; shift 2 ;;
        --rc-file) [ "$#" -ge 2 ] || fail 'Missing --rc-file value'; rc_file=$2; shift 2 ;;
        --mixed-port) [ "$#" -ge 2 ] || fail 'Missing --mixed-port value'; mixed_port=$2; shift 2 ;;
        --api-port) [ "$#" -ge 2 ] || fail 'Missing --api-port value'; api_port=$2; shift 2 ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; fail "Unknown option: $1" ;;
    esac
done
case "$mode" in user|system) ;; *) fail 'Mode must be user or system' ;; esac
# With no explicit mode/prefix, root can rerun the installer for the default
# system installation. Explicit paths inherit their recorded mode below.
if [ -z "$prefix" ] && [ -z "$requested_mode" ] && [ "$(id -u)" -eq 0 ] && [ -d /opt/mihomo-tui ]; then
    prefix=/opt/mihomo-tui
fi
if [ "$mode" = system ]; then
    [ "$(id -u)" -eq 0 ] || fail 'System mode requires root'
    [ -n "$prefix" ] || prefix=/opt/mihomo-tui
else
    [ -n "$prefix" ] || prefix="${XDG_DATA_HOME:-$HOME/.local/share}/mihomo-tui"
fi
case "$prefix" in /*) ;; *) fail 'Prefix must be an absolute path' ;; esac
case "$prefix" in *'
'*|*"$(printf '\r')"*) fail 'Prefix must be a single-line path' ;; esac
command -v sha256sum >/dev/null 2>&1 || fail 'Install sha256sum (GNU coreutils) first'
updating=no
restoring=no
recorded_unit=
recorded_rc=
selected_core=
if [ -e "$prefix" ] || [ -L "$prefix" ]; then
    # Verify the helper before executing it. Inspection itself is read-only.
    (cd "$bundle_dir" && sha256sum --status -c SHA256SUMS) || fail 'Bundle checksum verification failed; update stopped'
    existing=$("$bundle_dir/bin/mihomo-tui" installation inspect --prefix "$prefix") || fail 'Existing installation cannot be updated or reinstalled'
    prefix=$(CDPATH= cd -- "$prefix" && pwd -P)
    recorded_mode=$(printf '%s\n' "$existing" | sed -n '1p')
    recorded_unit=$(printf '%s\n' "$existing" | sed -n '2p')
    recorded_rc=$(printf '%s\n' "$existing" | sed -n '3p')
    selected_core=$(printf '%s\n' "$existing" | sed -n '4p')
    installation_kind=$(printf '%s\n' "$existing" | sed -n '5p')
    [ -z "$requested_mode" ] || [ "$requested_mode" = "$recorded_mode" ] || fail 'Requested mode does not match the installation'
    mode=$recorded_mode
    [ "$mixed_port" = 0 ] && [ "$api_port" = 0 ] || fail 'Updates and reinstalls preserve ports; stop the service and use configure to change them'
    case "$installation_kind" in
        installed)
            updating=yes
            existing_service=no
            [ -z "$recorded_unit" ] || existing_service=yes
            [ "$service_choice" = auto ] || [ "$service_choice" = "$existing_service" ] || fail 'Update preserves the existing service configuration'
            service_choice=$existing_service
            ;;
        retained)
            restoring=yes
            mixed_port=$(printf '%s\n' "$existing" | sed -n '6p')
            api_port=$(printf '%s\n' "$existing" | sed -n '7p')
            ;;
        *) fail 'Unknown installation state' ;;
    esac
    if [ -n "$recorded_rc" ] && [ "$shell_choice" = auto ] && [ -z "$rc_file" ]; then
        shell_choice=registered
        rc_file=$recorded_rc
    fi
fi

if [ "$mode" = system ] && [ "$updating" = no ] && [ "$restoring" = no ]; then
    [ "$mixed_port" != 0 ] || mixed_port=7890
    [ "$api_port" != 0 ] || api_port=9090
fi

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
    registered) ;;
    none) [ -z "$rc_file" ] || fail '--rc-file cannot be combined with --shell none' ;;
    *) fail 'Cannot determine a supported shell; select --shell bash, zsh or none' ;;
esac
if [ -n "$rc_file" ]; then
    case "$rc_file" in /*) ;; *) fail 'Shell rc must be an absolute path' ;; esac
    case "$rc_file" in *'
'*|*"$(printf '\r')"*) fail 'Shell rc must be a single-line path' ;; esac
fi

# Verify files before executing the bundled program or core.
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

if [ "$updating" = yes ]; then printf '\n%s\n' 'mihomo-tui update'
elif [ "$restoring" = yes ]; then printf '\n%s\n' 'mihomo-tui reinstallation'
else printf '\n%s\n' 'mihomo-tui installation'; fi
printf '  Mode:         %s\n' "$mode"
printf '  Destination:  %s\n' "$prefix"
printf '  Bundled core: %s (Linux amd64)\n' "$core_id"
echo '  Geo data:     bundled MMDB, GeoIP.dat and GeoSite.dat (offline)'
if [ "$updating" = yes ] || [ "$restoring" = yes ]; then
    printf '  Current core: %s (preserved)\n' "$selected_core"
    echo '  Data:         profiles, credentials, ports and TUN settings preserved'
fi
if [ "$updating" = yes ]; then
    echo '  Service:      configuration and running/enabled state preserved'
fi
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
    if [ "$updating" = yes ]; then echo '  Background:   existing systemd service'; else echo '  Background:   enabled systemd service'; fi
else
    echo '  Background:   no service; keep the TUI or foreground core running'
fi
if [ "$updating" = yes ]; then
    echo '  Proxy ports:  existing ports preserved'
elif [ "$restoring" = yes ]; then
    printf '  Proxy ports:  %s (mixed), %s (API), retained and checked before startup\n' "$mixed_port" "$api_port"
elif [ "$mode" = system ]; then
    printf '  Proxy ports:  %s (mixed), %s (API), checked before startup\n' "$mixed_port" "$api_port"
elif [ "$mixed_port" != 0 ] || [ "$api_port" != 0 ]; then
    printf '  Proxy ports:  requested mixed=%s, API=%s (0 = automatic)\n' "$mixed_port" "$api_port"
else
    echo '  Proxy ports:  random high ports, checked before startup'
fi

if [ "$mode" = user ]; then echo '  Proxy access: private credentials'; else echo '  Proxy access: shared on loopback; authenticated control API'; fi

if [ "$mode" = system ] && [ "$install_service" != yes ]; then
    fail 'System mode requires an available systemd manager and a service'
fi
[ "$install_service" != yes ] || [ "$manager_available" = yes ] || fail 'Requested service manager is unavailable'
unit_path=
if [ "$install_service" = yes ]; then
    if [ "$updating" = yes ]; then
        unit_path=$recorded_unit
    elif [ "$mode" = user ]; then unit_path="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mihomo-tui.service"; else unit_path=/etc/systemd/system/mihomo-tui.service; fi
    case "$unit_path" in /*) ;; *) fail 'Service configuration directory must be absolute' ;; esac
    case "$unit_path" in *'
'*|*"$(printf '\r')"*) fail 'Service path must be a single-line path' ;; esac
    if [ "$updating" != yes ]; then
        [ ! -e "$unit_path" ] && [ ! -L "$unit_path" ] || fail 'Service unit already exists'
    fi
fi

# Port preflight must finish before creating an installation directory. Skip
# existing ports during updates because the preserved service may own them.
if [ "$updating" = no ] && { [ "$mixed_port" != 0 ] || [ "$api_port" != 0 ]; }; then
    (cd "$bundle_dir" && sha256sum --status -c SHA256SUMS) || fail 'Bundle checksum verification failed; installation stopped'
    if ! "$bundle_dir/bin/mihomo-tui" installation check-ports --mixed-port "$mixed_port" --api-port "$api_port"; then
        [ "$restoring" != yes ] || fail 'Retained ports are unavailable; stop the conflicting application before reinstalling'
        fail 'Requested ports are unavailable; use --mixed-port and --api-port to select free ports'
    fi
fi

if [ "$assume_yes" != yes ]; then
    action=installation
    cancelled='Installation cancelled.'
    if [ "$updating" = yes ]; then action=update; cancelled='Update cancelled.'; fi
    if [ "$restoring" = yes ]; then action=reinstallation; cancelled='Reinstallation cancelled.'; fi
    printf '\nProceed with %s? [y/N] ' "$action"
    if ! IFS= read -r answer; then echo; echo "$cancelled"; exit 0; fi
    case "$answer" in y|Y|yes|YES|Yes) ;; *) echo "$cancelled"; exit 0 ;; esac
fi
echo 'Verifying offline files...'
cd "$bundle_dir"
sha256sum --status -c SHA256SUMS || fail 'Bundle checksum verification failed; installation stopped'
if [ "$updating" = yes ]; then
    echo 'Updating application; keeping selected core and proxy data...'
    if [ "$shell_choice" = none ]; then
        "$bundle_dir/bin/mihomo-tui" installation update --prefix "$prefix"
    else
        "$bundle_dir/bin/mihomo-tui" installation update --prefix "$prefix" --rc-file "$rc_file"
    fi
    echo "Updated in $prefix"
    echo "Launch: \"$prefix/mihomo-tui\""
    if [ "$shell_choice" != none ]; then echo "Open a new shell or source the existing rc: $rc_file"; fi
    exit 0
fi
app_bin="$prefix/mihomo-tui"
if [ "$restoring" = yes ]; then
    echo 'Restoring application; keeping selected core and proxy data...'
    if [ "$shell_choice" = none ]; then
        "$bundle_dir/bin/mihomo-tui" installation update --prefix "$prefix"
    else
        "$bundle_dir/bin/mihomo-tui" installation update --prefix "$prefix" --rc-file "$rc_file"
    fi
else
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
    "$app_bin" configure --install-mode "$mode" --mixed-port "$mixed_port" --api-port "$api_port"
    "$bundle_dir/bin/mihomo-tui" installation seed-geodata --prefix "$prefix"
    "$app_bin" core import "$core_id" "$bundle_dir/cores/mihomo" >/dev/null
    profile_output=$("$app_bin" profile new Default)
    profile_id=${profile_output%% *}
    "$app_bin" profile select "$profile_id"
    "$app_bin" service --mode "$mode" > "$prefix/mihomo-tui.service"
    "$app_bin" env-file
    if [ "$shell_choice" != none ]; then "$app_bin" shell install --rc-file "$rc_file"; fi
fi

if [ "$restoring" = yes ]; then
    "$app_bin" service --mode "$mode" > "$prefix/mihomo-tui.service"
    printf '%s\n' mihomo-tui-install-v1 "$mode" "$unit_path" "$prefix" > "$prefix/.mihomo-tui-install"
fi

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
