function _verge_tui_message --argument-names color message
    if isatty stdout; and test "$TERM" != dumb; and not set -q NO_COLOR
        set_color $color
        printf '%s' $message
        set_color normal
        printf '\n'
    else
        printf '%s\n' $message
    end
end
