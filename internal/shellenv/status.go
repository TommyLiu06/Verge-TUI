package shellenv

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"mihomo-tui/internal/controller"
	"mihomo-tui/internal/management"
	"mihomo-tui/internal/state"
)

func Status(ctx context.Context, s *state.Store, environ []string) (string, error) {
	c, err := s.Read()
	public := false
	if os.IsPermission(err) {
		c, err = ReadFile(FilePath(s))
		if err == nil && c.InstallMode != "system" {
			return "", fmt.Errorf("private proxy configuration is not accessible")
		}
		public = true
	}
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var out strings.Builder
	fmt.Fprintf(&out, "Installation: %s\nProxy address: http://127.0.0.1:%d\n", c.InstallMode, c.MixedPort)
	client := controller.New(c)
	defer client.Close()
	var version struct{ Version string }
	err = client.Request(ctx, "GET", "/version", nil, &version)
	if public {
		shared := management.NewClient(s)
		defer shared.Close()
		var view struct {
			Online   bool
			Snapshot controller.Snapshot
		}
		if shared.Call(ctx, "GET", "/view", nil, &view) == nil && view.Online {
			fmt.Fprintf(&out, "Core: running (%s)\nProxy mode: %s\nTUN: %t\n", view.Snapshot.Version, view.Snapshot.Config.Mode, view.Snapshot.Config.Tun.Enable)
		} else if err != nil && strings.Contains(err.Error(), "HTTP 401") {
			out.WriteString("Core: API reachable (administrator authentication required)\n")
		} else {
			out.WriteString("Core: private API status unavailable to this user\n")
		}
	} else if err == nil {
		fmt.Fprintf(&out, "Core: running (%s)\n", version.Version)
		var config controller.Config
		if client.Request(ctx, "GET", "/configs", nil, &config) == nil {
			fmt.Fprintf(&out, "Proxy mode: %s\nTUN: %t\n", config.Mode, config.Tun.Enable)
		}
	} else {
		out.WriteString("Core: API unavailable or authentication failed\n")
	}
	dial := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dial.DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.MixedPort)))
	if err == nil {
		conn.Close()
		out.WriteString("Proxy listener: listening\n")
	} else {
		out.WriteString("Proxy listener: unavailable\n")
	}
	actual := map[string]string{}
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			actual[name] = value
		}
	}
	expected := Expected(c)
	matches, active := 0, 0
	var details strings.Builder
	for _, name := range Variables {
		value, set := actual[name]
		status := "unset"
		if set {
			switch {
			case value == expected[name]:
				status = "matches"
				matches++
			case value == "":
				status = "empty"
			default:
				status = "different (value hidden)"
			}
			if value != "" {
				active++
			}
		}
		fmt.Fprintf(&details, "  %s: %s\n", name, status)
	}
	if matches == len(Variables) {
		out.WriteString("Shell environment: loaded and matches this installation\n")
	} else if active == 0 {
		out.WriteString("Shell environment: not loaded\n")
	} else {
		out.WriteString("Shell environment: partial or different\n")
	}
	out.WriteString(details.String())
	return out.String(), nil
}
