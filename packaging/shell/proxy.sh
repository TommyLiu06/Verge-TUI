# Shared Bash/Zsh integration. Arguments: executable, data directory, mode.
_verge_tui_executable=$1
_verge_tui_data_dir=$2
_verge_tui_directory=${_verge_tui_executable%/*}
_verge_tui_env_file=${_verge_tui_data_dir%/*}/proxy.env

if [ "$3" = user ]; then
    case ":${PATH-}:" in
        *:"$_verge_tui_directory":*) ;;
        *) export PATH="$_verge_tui_directory${PATH:+:$PATH}" ;;
    esac
fi

_verge_tui_message() {
    printf '%s' "$2"
    if [ -t 1 ] && [ "${TERM-}" != dumb ] && [ "${NO_COLOR+x}" != x ]; then
        printf '\033[%sm%s\033[0m' "$1" "$3"
    else
        printf '%s' "$3"
    fi
    printf '%s\n' "$4"
}

verge-on() {
    if [ ! -r "$_verge_tui_env_file" ]; then
        _verge_tui_message 31 'Proxy environment file is ' unavailable '.' >&2
        return 1
    fi
    . "$_verge_tui_env_file" || return
    _verge_tui_message 32 'Proxy environment: ' enabled '.'
}

verge-off() {
    unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY || return
    _verge_tui_message 31 'Proxy environment: ' disabled '.'
}

verge-status() {
    command "$_verge_tui_executable" --data-dir "$_verge_tui_data_dir" proxy-status
}

# Automatic exports are quiet and apply once per interactive login shell.
case $- in
    *i*)
        if [ "${_verge_tui_login_loaded-}" != "$_verge_tui_data_dir" ]; then
            _verge_tui_is_login=
            if [ -n "${BASH_VERSION-}" ]; then
                shopt -q login_shell && _verge_tui_is_login=1
            elif [ -n "${ZSH_VERSION-}" ]; then
                [[ -o login ]] && _verge_tui_is_login=1
            fi
            if [ "$_verge_tui_is_login" = 1 ]; then
                _verge_tui_login_loaded=$_verge_tui_data_dir
                if _verge_tui_login_exports=$(command "$_verge_tui_executable" --data-dir "$_verge_tui_data_dir" env --auto-login 2>/dev/null); then
                    eval "$_verge_tui_login_exports"
                fi
                unset _verge_tui_login_exports
            fi
            unset _verge_tui_is_login
        fi
        ;;
esac
