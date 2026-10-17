function _verge_tui_message --argument-names color prefix state suffix
    printf '%s' $prefix
    if isatty stdout; and test "$TERM" != dumb; and not set -q NO_COLOR
        set_color $color
        printf '%s' $state
        set_color normal
    else
        printf '%s' $state
    end
    printf '%s\n' $suffix
end
