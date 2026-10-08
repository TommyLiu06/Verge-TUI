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

verge_on() {
    if [ ! -r "$_verge_tui_env_file" ]; then
        printf '%s\n' 'Proxy environment file is unavailable.' >&2
        return 1
    fi
    . "$_verge_tui_env_file"
}

verge_off() {
    unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY
}

verge_status() {
    command "$_verge_tui_executable" --data-dir "$_verge_tui_data_dir" proxy-status
}
