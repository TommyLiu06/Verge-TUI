#!/bin/sh
set -eu
umask 077

usage() {
    echo 'Usage: sh install.sh [--prefix DIR] [--mode user|system] [--service|--no-service] [--mixed-port PORT] [--api-port PORT] [--shell bash|zsh|fish|none] [--rc-file FILE] [--bin-dir DIR] [--yes]'
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
requested_rc=
mixed_port=0
api_port=0
bin_dir=/usr/local/bin
requested_bin_dir=
registration_uid=
elevated=no
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix) [ "$#" -ge 2 ] || fail 'Missing --prefix value'; prefix=$2; shift 2 ;;
        --mode) [ "$#" -ge 2 ] || fail 'Missing --mode value'; mode=$2; requested_mode=$2; shift 2 ;;
        --service) service_choice=yes; shift ;;
        --no-service) service_choice=no; shift ;;
        --yes|-y) assume_yes=yes; shift ;;
        --shell) [ "$#" -ge 2 ] || fail 'Missing --shell value'; shell_choice=$2; shift 2 ;;
        --rc-file) [ "$#" -ge 2 ] || fail 'Missing --rc-file value'; rc_file=$2; requested_rc=$2; shift 2 ;;
        --mixed-port) [ "$#" -ge 2 ] || fail 'Missing --mixed-port value'; mixed_port=$2; shift 2 ;;
        --bin-dir) [ "$#" -ge 2 ] || fail 'Missing --bin-dir value'; bin_dir=$2; requested_bin_dir=$2; shift 2 ;;
        --shell-user) [ "$#" -ge 2 ] || fail 'Missing initiating user'; registration_uid=$2; shift 2 ;;
        --elevated) elevated=yes; shift ;;
        --api-port) [ "$#" -ge 2 ] || fail 'Missing --api-port value'; api_port=$2; shift 2 ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; fail "Unknown option: $1" ;;
    esac
done
# Offline verification precedes every invocation of the bundled helper.
command -v sha256sum >/dev/null 2>&1 || fail 'Install sha256sum (GNU coreutils) first'
(cd "$bundle_dir" && sha256sum --status -c SHA256SUMS) || fail 'Bundle checksum verification failed'
case "$(uname -s)/$(uname -m)" in Linux/x86_64) ;; *) fail 'This bundle requires Linux amd64' ;; esac
helper="$bundle_dir/bin/verge-tui"
caller_uid=$(id -u)
user_prefix=${prefix:-"${XDG_DATA_HOME:-$HOME/.local/share}/verge-tui"}
system_prefix=${prefix:-/opt/verge-tui}

candidate_status() {
    candidate=$1
    expected_mode=$2
    if [ ! -e "$candidate" ] && [ ! -L "$candidate" ]; then printf '%s' Install; return; fi
    if summary=$("$helper" installation discover --prefix "$candidate" 2>/dev/null); then
        status=$(printf '%s\n' "$summary" | sed -n '1p')
        known_mode=$(printf '%s\n' "$summary" | sed -n '2p')
        known_version=$(printf '%s\n' "$summary" | sed -n '3p')
        if [ "$status" != absent ]; then
            if [ "$known_mode" != "$expected_mode" ]; then printf 'Unavailable: belongs to %s mode' "$known_mode"; return; fi
            if [ "$status" = retained ]; then printf '%s' 'Reinstall (data retained)'; else printf 'Update (%s)' "$known_version"; fi
            return
        fi
    fi
    if [ -f "$candidate/.verge-tui-install" ]; then
        if [ -f "$candidate/verge-tui" ]; then printf '%s' 'Update (verification required)'; else printf '%s' 'Reinstall (data retained; verification required)'; fi
        return
    fi
    # Presence is a menu hint only. Private inspection after mode selection
    # rejects unrelated/incomplete directories and insufficient permissions.
    printf '%s' 'Existing destination — verification required'
}

if [ -z "$requested_mode" ]; then
    if [ "$assume_yes" = yes ] && [ -n "$prefix" ]; then
        mode=user
        if summary=$("$helper" installation discover --prefix "$prefix" 2>/dev/null); then
            discovered_mode=$(printf '%s\n' "$summary" | sed -n '2p')
            case "$discovered_mode" in user|system) mode=$discovered_mode ;; esac
        fi
    elif [ "$assume_yes" = yes ]; then
        fail 'Unattended installation requires --mode user or --mode system'
    else
        printf '\n%s\n' 'verge-tui setup'
        printf '  Current user: %s (uid %s)\n  Platform:     Linux amd64\n' "$(id -un)" "$caller_uid"
        user_manager=no; system_manager=no
        if command -v systemctl >/dev/null 2>&1; then
            if systemctl --user show-environment >/dev/null 2>&1; then user_manager=yes; fi
            if systemctl show-environment >/dev/null 2>&1; then system_manager=yes; fi
        fi
        printf '  User systemd: %s\n  System systemd: %s\n' "$user_manager" "$system_manager"
        printf '\nChoose installation mode:\n  1. User   — %s\n     %s\n  2. System — %s\n     %s\n  0. Cancel\n' "$(candidate_status "$user_prefix" user)" "$user_prefix" "$(candidate_status "$system_prefix" system)" "$system_prefix"
        while :; do
            printf 'Selection [0/1/2]: '
            if ! IFS= read -r selection; then echo; echo 'Installation cancelled.'; exit 0; fi
            case "$selection" in
                1|user) mode=user; break ;;
                2|system) mode=system; break ;;
                0|'') echo 'Installation cancelled.'; exit 0 ;;
                *) echo 'Select 1 (user), 2 (system), or 0 (cancel).' ;;
            esac
        done
    fi
    requested_mode=$mode
fi
case "$mode" in user|system) ;; *) fail 'Mode must be user or system' ;; esac
if [ "$mode" = system ]; then prefix=$system_prefix; else prefix=$user_prefix; fi
# Reject an explicitly mismatched installation before asking for sudo. Public
# discovery is a menu/preflight hint; private inspection still runs below.
if summary=$("$helper" installation discover --prefix "$prefix" 2>/dev/null); then
    known_mode=$(printf '%s\n' "$summary" | sed -n '2p')
    [ -z "$known_mode" ] || [ "$mode" = "$known_mode" ] || fail 'Requested mode does not match the installation'
fi
if [ "$mode" = system ]; then
    if summary=$("$helper" installation discover --prefix "$prefix" 2>/dev/null); then
        existing_link=$(printf '%s\n' "$summary" | sed -n '4p')
        if [ -n "$existing_link" ]; then
            existing_bin_dir=$(dirname -- "$existing_link")
            [ -z "$requested_bin_dir" ] || [ "$requested_bin_dir" = "$existing_bin_dir" ] || fail 'Updates and reinstalls preserve the system command directory'
            bin_dir=$existing_bin_dir
        fi
    fi
fi

# Resolve the initiating shell before sudo changes HOME and process ancestry.
if [ "$shell_choice" = auto ]; then
    parent_shell=
    if command -v ps >/dev/null 2>&1; then
        shell_pid=${PPID:-0}
        shell_depth=0
        while [ "$shell_depth" -lt 8 ]; do
            case "$shell_pid" in ''|*[!0-9]*|0) break ;; esac
            parent_shell=$(ps -p "$shell_pid" -o comm= 2>/dev/null) || break
            parent_shell=${parent_shell#-}
            case "$parent_shell" in
                bash|zsh|fish) break ;;
                sudo|sh)
                    shell_pid=$(ps -p "$shell_pid" -o ppid= 2>/dev/null | tr -d '[:space:]')
                    shell_depth=$((shell_depth + 1))
                    ;;
                *) break ;;
            esac
        done
    fi
    login_shell=${SHELL:-}
    case "$parent_shell" in bash|zsh|fish) shell_choice=$parent_shell ;; *) shell_choice=${login_shell##*/} ;; esac
fi
[ -n "$registration_uid" ] || registration_uid=$caller_uid
registration_home=$HOME
# An external sudo invocation also belongs to its initiating account for system
# registration. A root user-mode installation always belongs to root itself.
if [ "$mode" = system ] && [ "$caller_uid" = 0 ] && [ "$registration_uid" = 0 ] && [ -n "${SUDO_UID:-}" ]; then
    registration_uid=$SUDO_UID
    account=$("$helper" installation account --user "$registration_uid") || fail 'Cannot resolve initiating account'
    registration_home=$(printf '%s\n' "$account" | sed -n '2p')
    unset XDG_CONFIG_HOME ZDOTDIR
fi
case "$registration_uid" in ''|*[!0-9]*) fail 'Invalid initiating UID' ;; esac
if [ "$mode" = user ]; then registration_uid=$caller_uid; fi
case "$shell_choice" in
    bash) [ -n "$rc_file" ] || rc_file="$registration_home/.bashrc" ;;
    zsh) [ -n "$rc_file" ] || rc_file="${ZDOTDIR:-$registration_home}/.zshrc" ;;
    fish) [ -n "$rc_file" ] || rc_file="${XDG_CONFIG_HOME:-$registration_home/.config}/fish/config.fish" ;;
    none) [ -z "$rc_file" ] || fail '--rc-file cannot be combined with --shell none' ;;
    *) fail 'Unsupported user shell; select --shell bash, zsh, fish or none' ;;
esac

# Preserve an existing custom rc and shell before sudo turns the resolved path
# into an explicit --rc-file argument in the elevated process.
if [ "$shell_choice" != none ] && [ -d "$prefix/data" ]; then
    registered=$("$helper" --data-dir "$prefix/data" shell info --user "$registration_uid") || fail "Cannot inspect this user's shell registration"
    user_rc=$(printf '%s\n' "$registered" | sed -n '1p')
    user_shell=$(printf '%s\n' "$registered" | sed -n '2p')
    if [ -n "$user_rc" ] && [ -z "$requested_rc" ]; then rc_file=$user_rc; shell_choice=$user_shell; fi
fi
if [ "$mode" = system ] && [ "$caller_uid" != 0 ]; then
    [ "$elevated" = no ] || fail 'sudo did not grant root privileges'
    [ "$registration_uid" = "$caller_uid" ] || fail 'The initiating UID must match the current user'
    command -v sudo >/dev/null 2>&1 || fail 'System installation requires sudo; ask an administrator or select user mode'
    echo 'System mode needs administrator privileges. Continuing through sudo...'
    set -- "$bundle_dir/install.sh" --elevated --mode system --prefix "$prefix" --shell "$shell_choice" --shell-user "$registration_uid" --bin-dir "$bin_dir" --mixed-port "$mixed_port" --api-port "$api_port"
    [ -z "$rc_file" ] || set -- "$@" --rc-file "$rc_file"
    case "$service_choice" in yes) set -- "$@" --service ;; no) set -- "$@" --no-service ;; esac
    [ "$assume_yes" != yes ] || set -- "$@" --yes
    exec sudo -- sh "$@"
fi
case "$bin_dir" in /*) ;; *) fail 'Command directory must be absolute' ;; esac
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
    existing=$("$bundle_dir/bin/verge-tui" installation inspect --prefix "$prefix") || fail 'Existing installation cannot be updated or reinstalled'
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

fi

if [ "$mode" = system ] && [ "$updating" = no ] && [ "$restoring" = no ]; then
    [ "$mixed_port" != 0 ] || mixed_port=7890
    [ "$api_port" != 0 ] || api_port=9090
fi

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

if [ "$updating" = yes ]; then printf '\n%s\n' 'verge-tui update'
elif [ "$restoring" = yes ]; then printf '\n%s\n' 'verge-tui reinstallation'
else printf '\n%s\n' 'verge-tui installation'; fi
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
    echo '  Functions:    verge-on, verge-off, verge-status'
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
    printf '  Proxy ports:  %s (mixed), %s (external controller, disabled unless enabled in Settings), retained\n' "$mixed_port" "$api_port"
elif [ "$mode" = system ]; then
    printf '  Proxy ports:  %s (mixed), %s (external controller, disabled by default)\n' "$mixed_port" "$api_port"
elif [ "$mixed_port" != 0 ] || [ "$api_port" != 0 ]; then
    printf '  Proxy ports:  requested mixed=%s, API=%s (0 = automatic)\n' "$mixed_port" "$api_port"
else
    echo '  Proxy ports:  random high ports, checked before startup'
fi

if [ "$mode" = user ]; then echo '  Proxy access: private credentials'; else echo '  Proxy access: shared on loopback; private local control socket'; fi

if [ "$mode" = system ] && [ "$install_service" != yes ]; then
    fail 'System mode requires an available systemd manager and a service'
fi
[ "$install_service" != yes ] || [ "$manager_available" = yes ] || fail 'Requested service manager is unavailable'
unit_path=
if [ "$install_service" = yes ]; then
    if [ "$updating" = yes ]; then
        unit_path=$recorded_unit
    elif [ "$mode" = user ]; then unit_path="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/verge-tui.service"; else unit_path=/etc/systemd/system/verge-tui.service; fi
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
    if ! "$bundle_dir/bin/verge-tui" installation check-ports --mixed-port "$mixed_port" --api-port 0; then
        [ "$restoring" != yes ] || fail 'Retained ports are unavailable; stop the conflicting application before reinstalling'
        fail 'Requested ports are unavailable; use --mixed-port and --api-port to select free ports'
    fi
fi

if [ "$mode" = system ]; then
    "$helper" installation check-link --prefix "$prefix" --bin-dir "$bin_dir" || fail 'System command path is already occupied'
    printf '  Command:      %s/verge-tui\n' "$bin_dir"
fi
printf '  Shell owner:  uid %s\n' "$registration_uid"
if [ "$shell_choice" = fish ]; then echo '  Shell files:  bundled functions; conf.d/ loader; config.fish unchanged'; fi
finish_registration() {
    app_bin="$prefix/verge-tui"
    "$app_bin" installation record --prefix "$prefix" --bin-dir "$bin_dir"
    if [ "$shell_choice" != none ]; then
        if [ "$updating" = yes ] && [ -z "$requested_rc" ]; then
            "$app_bin" shell install --user "$registration_uid"
        else
            "$app_bin" shell install --shell "$shell_choice" --rc-file "$rc_file" --user "$registration_uid"
        fi
    fi
}

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
    "$bundle_dir/bin/verge-tui" installation update --prefix "$prefix"
    finish_registration
    echo "Updated in $prefix"
    echo "Launch: \"$prefix/verge-tui\""
    if [ "$shell_choice" != none ]; then echo 'Open a new shell to load the integration.'; fi
    exit 0
fi
app_bin="$prefix/verge-tui"
if [ "$restoring" = yes ]; then
    echo 'Restoring application; keeping selected core and proxy data...'
    "$bundle_dir/bin/verge-tui" installation update --prefix "$prefix"
else
    echo 'Installing application and managed core...'

    mkdir -m 700 -p -- "$prefix"
    prefix=$(CDPATH= cd -- "$prefix" && pwd -P)
    cp bin/verge-tui "$prefix/verge-tui"
    cp -R shell "$prefix/"
    chmod 755 "$prefix/shell" "$prefix/shell/fish" "$prefix/shell/fish/functions"
    chmod 644 "$prefix/shell/proxy.sh" "$prefix/shell/fish/init.fish" "$prefix/shell/fish/functions/"*.fish
    chmod 700 "$prefix/verge-tui"
    if [ "$mode" = system ]; then chmod 755 "$prefix" "$prefix/verge-tui"; fi
    cp README.md LICENSE NOTICE.md "$prefix/"
    cp -R img "$prefix/img"
    cp -R docs "$prefix/docs"
    cp DEPENDENCIES.txt "$prefix/"
    cp -R licenses "$prefix/licenses"
    cp uninstall.sh "$prefix/uninstall.sh"
    printf '%s\n' verge-tui-install-v1 "$mode" "$unit_path" "$prefix" > "$prefix/.verge-tui-install"
    app_bin="$prefix/verge-tui"
    "$app_bin" configure --install-mode "$mode" --mixed-port "$mixed_port" --api-port "$api_port"
    "$bundle_dir/bin/verge-tui" installation seed-geodata --prefix "$prefix"
    "$app_bin" core import "$core_id" "$bundle_dir/cores/mihomo" >/dev/null
    profile_output=$("$app_bin" profile new Default)
    profile_id=${profile_output%% *}
    "$app_bin" profile select "$profile_id"
    "$app_bin" service --mode "$mode" > "$prefix/verge-tui.service"
    "$app_bin" env-file
fi

if [ "$restoring" = yes ]; then
    "$app_bin" service --mode "$mode" > "$prefix/verge-tui.service"
    printf '%s\n' verge-tui-install-v1 "$mode" "$unit_path" "$prefix" > "$prefix/.verge-tui-install"
fi

if [ "$install_service" = yes ]; then
    mkdir -p -- "$(dirname -- "$unit_path")"
    cp "$prefix/verge-tui.service" "$unit_path"
    chmod 600 "$unit_path"
    if [ "$mode" = user ]; then
        systemctl --user daemon-reload
        systemctl --user enable --now verge-tui.service
        echo 'User service enabled. Startup after logout requires administrator-approved lingering.'
    else
        systemctl daemon-reload
        systemctl enable --now verge-tui.service
        echo 'System service enabled. Open Home to enable TUN Mode.'
    fi
fi
finish_registration
echo "Installed in $prefix"
echo "Launch: \"$app_bin\""
