package state

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// System proxy preferences affect the calling desktop user, not core listeners.
type SystemProxySettings struct {
	Host             string `json:"host"`
	UseDefaultBypass bool   `json:"use_default_bypass"`
	Bypass           string `json:"bypass"`
}

func DefaultSystemProxy() SystemProxySettings {
	return SystemProxySettings{Host: "127.0.0.1", UseDefaultBypass: true}
}

// Match SysproxyViewer's Linux default list in the pinned Verge source.
func DefaultProxyBypass() []string {
	return []string{"localhost", "127.0.0.1", "192.168.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "::1"}
}

func (p SystemProxySettings) IgnoreHosts() []string {
	if p.UseDefaultBypass {
		return DefaultProxyBypass()
	}
	return splitProxyBypass(p.Bypass)
}

func splitProxyBypass(value string) []string {
	var result []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }) {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func proxyHostValid(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Zone() == ""
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				return false
			}
		}
	}
	return true
}

func proxyBypassValid(value string) bool {
	if _, err := netip.ParsePrefix(value); err == nil {
		return true
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Zone() == ""
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
		value = host
	}
	return proxyHostValid(strings.TrimPrefix(strings.TrimPrefix(value, "*."), "."))
}

func (p SystemProxySettings) Validate() error {
	if !proxyHostValid(p.Host) {
		return errors.New("enter a proxy IP address or hostname, without a port or URL")
	}
	items := splitProxyBypass(p.Bypass)
	if len(p.Bypass) > 8192 || len(items) > 256 {
		return errors.New("proxy bypass list is too long")
	}
	for _, item := range items {
		if !proxyBypassValid(item) {
			return fmt.Errorf("invalid proxy bypass %q; use a hostname, *.domain, IP address or CIDR (hosts may include a port)", item)
		}
	}
	return nil
}
