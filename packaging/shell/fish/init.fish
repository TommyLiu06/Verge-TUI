# Arguments: executable, data directory, mode. Uses session variables only.
set -g _verge_tui_executable $argv[1]
set -g _verge_tui_data_dir $argv[2]
set -g _verge_tui_directory (string replace -r '/[^/]+$' '' -- $_verge_tui_executable)
set -g _verge_tui_functions "$_verge_tui_directory/shell/fish/functions"

if test "$argv[3]" = user
    if not contains -- $_verge_tui_directory $PATH
        set -gx PATH $_verge_tui_directory $PATH
    end
end

if not contains -- $_verge_tui_functions $fish_function_path
    set -g fish_function_path $_verge_tui_functions $fish_function_path
end

if status is-interactive; and status is-login; and not contains -- $_verge_tui_data_dir $_verge_tui_login_loaded
    set -g _verge_tui_login_loaded $_verge_tui_data_dir
    set -l proxy_commands (command $_verge_tui_executable --data-dir $_verge_tui_data_dir env --shell fish --auto-login)
    if test $status -eq 0; and test (count $proxy_commands) -gt 0
        verge-on
    end
end
