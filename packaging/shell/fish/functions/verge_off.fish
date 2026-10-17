function verge_off
    for proxy_variable in http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY
        set -eg $proxy_variable
        if set -qU $proxy_variable
            set -gu $proxy_variable ''
        end
    end
    _verge_tui_message red 'Proxy environment disabled.'
end
