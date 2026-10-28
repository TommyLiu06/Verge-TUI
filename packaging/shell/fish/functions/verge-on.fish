function verge-on
    set -l proxy_commands (command $_verge_tui_executable --data-dir $_verge_tui_data_dir env --shell fish)
    or return $status
    printf '%s\n' $proxy_commands | source
    or return $status
    _verge_tui_message green 'Proxy environment: ' enabled '.'
end
