package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func Unit(executable, root, mode string) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(root) || strings.ContainsAny(executable+root, "\r\n\x00") {
		return "", errors.New("service paths must be absolute single-line paths")
	}
	if mode != "system" && mode != "user" {
		return "", errors.New("service mode must be system or user")
	}
	quote := func(s string) string {
		s = strings.ReplaceAll(s, "%", "%%")
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\"", "\\\"")
		return "\"" + s + "\""
	}
	unit := "[Unit]\nDescription=verge-tui managed proxy core\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\n"
	// Disable systemd environment expansion so literal '$' in paths is preserved.
	unit += fmt.Sprintf("ExecStart=:%s --data-dir %s run\n", quote(executable), quote(root))
	unit += "Restart=on-failure\nRestartSec=5s\nTimeoutStopSec=10s\nKillMode=control-group\nUMask=0077\nNoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=strict\n"
	unit += "ReadWritePaths=" + quote(root) + "\n"
	if mode == "system" {
		unit += "User=root\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE\nAmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE\n"
	}
	// No CapabilityBoundingSet in user mode: dropping capabilities from the
	// bounding set requires CAP_SETPCAP, which the systemd user manager does
	// not hold, so the service would die at spawn with 218/CAPABILITIES.
	unit += "\n[Install]\nWantedBy="
	if mode == "system" {
		unit += "multi-user.target\n"
	} else {
		unit += "default.target\n"
	}
	return unit, nil
}
