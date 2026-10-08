package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"verge-tui/internal/state"
)

type Runner func(context.Context, string, []string, string) (string, error)
type GNOME struct {
	Store           *state.Store
	PreferencesRoot string
	Run             Runner
}
type Status struct {
	Available bool
	Enabled   bool
	Owned     bool
	Address   string
	Detail    string
}

func Exec(ctx context.Context, name string, args []string, input string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed; check the GNOME session and permissions", name)
	}
	return strings.TrimSpace(string(b)), nil
}
func (g GNOME) runner() Runner {
	if g.Run != nil {
		return g.Run
	}
	return Exec
}
func (g GNOME) available() bool {
	if g.Run != nil {
		return true
	}
	if !strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "gnome") || os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return false
	}
	_, a := exec.LookPath("gsettings")
	_, b := exec.LookPath("dconf")
	return a == nil && b == nil
}
func (g GNOME) preferences() (state.Preferences, error) {
	root := g.PreferencesRoot
	if root == "" {
		root = g.Store.Root
	}
	return state.ReadPreferences(root)
}
func (g GNOME) Read(ctx context.Context, c state.Settings) Status {
	if !g.available() {
		return Status{Detail: "GNOME session, gsettings and dconf required"}
	}
	p, err := g.preferences()
	if err != nil {
		return Status{Available: true, Detail: err.Error()}
	}
	return g.read(ctx, c, p.SystemProxy.Host)
}
func (g GNOME) read(ctx context.Context, c state.Settings, expectedHost string) Status {
	status := Status{Available: g.available(), Detail: "GNOME session, gsettings and dconf required"}
	if !status.Available {
		return status
	}
	run := g.runner()
	mode, err := run(ctx, "gsettings", []string{"get", "org.gnome.system.proxy", "mode"}, "")
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	status.Enabled = mode == "'manual'" || mode == "'auto'"
	status.Detail = "System Proxy Disabled"
	if !status.Enabled {
		return status
	}
	status.Detail = "System Proxy Enabled"
	host, err := run(ctx, "gsettings", []string{"get", "org.gnome.system.proxy.http", "host"}, "")
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	port, err := run(ctx, "gsettings", []string{"get", "org.gnome.system.proxy.http", "port"}, "")
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	status.Address = net.JoinHostPort(strings.Trim(host, "'"), strings.TrimSpace(port))
	_, err = os.Stat(filepath.Join(g.Store.Root, "gnome-proxy-backup.ini"))
	status.Owned = err == nil && mode == "'manual'" && status.Address == net.JoinHostPort(expectedHost, strconv.Itoa(c.MixedPort))
	return status
}
func variant(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "\\'")
	return "'" + value + "'"
}
func (g GNOME) Enable(ctx context.Context, c state.Settings) error {
	if !g.available() {
		return errors.New("GNOME system proxy requires a GNOME session, gsettings and dconf")
	}
	p, err := g.preferences()
	if err != nil {
		return err
	}
	backup := filepath.Join(g.Store.Root, "gnome-proxy-backup.ini")
	if _, err := os.Stat(backup); err == nil {
		return errors.New("a proxy backup already exists; disable and restore it before enabling again")
	} else if !os.IsNotExist(err) {
		return err
	}
	run := g.runner()
	previous, err := run(ctx, "dconf", []string{"dump", "/system/proxy/"}, "")
	if err != nil {
		return err
	}
	if err := state.Write(backup, []byte(previous+"\n"), 0600); err != nil {
		return err
	}
	payload := proxyPayload(c, p.SystemProxy)
	// dconf load takes values from stdin, keeping passwords out of process arguments.
	_, err = run(ctx, "dconf", []string{"load", "/system/proxy/"}, payload)
	if err != nil {
		return fmt.Errorf("%w; original settings saved in %s", err, backup)
	}
	observed := g.Read(ctx, c)
	if !observed.Enabled || !observed.Owned {
		return errors.New("GNOME proxy settings were written but could not be verified; backup retained")
	}
	return nil
}
func (g GNOME) Disable(ctx context.Context, c state.Settings) error {
	observed := g.Read(ctx, c)
	if !observed.Available {
		return errors.New(observed.Detail)
	}
	backup := filepath.Join(g.Store.Root, "gnome-proxy-backup.ini")
	b, err := os.ReadFile(backup)
	if err != nil {
		return errors.New("no original GNOME proxy backup; external settings will not be changed")
	}
	if observed.Enabled && !observed.Owned {
		return errors.New("GNOME proxy was changed externally; restore the saved backup manually")
	}
	run := g.runner()
	// An empty dump means the keys used schema defaults. Reset our subtree before restoring it.
	if _, err := run(ctx, "dconf", []string{"reset", "-f", "/system/proxy/"}, ""); err != nil {
		return err
	}
	if _, err := run(ctx, "dconf", []string{"load", "/system/proxy/"}, string(b)); err != nil {
		return err
	}
	return os.Remove(backup)
}

func proxyPayload(c state.Settings, p state.SystemProxySettings) string {
	auth := "false"
	user := ""
	password := ""
	if c.InstallMode == "user" {
		auth = "true"
		user = c.ProxyUser
		password = c.ProxyPassword
	}
	host := variant(p.Host)
	var bypass []string
	for _, item := range p.IgnoreHosts() {
		bypass = append(bypass, variant(item))
	}
	return fmt.Sprintf("[/]\nmode='manual'\nuse-same-proxy=false\nautoconfig-url=''\nignore-hosts=[%s]\n\n[http]\nhost=%s\nport=%d\nuse-authentication=%s\nauthentication-user=%s\nauthentication-password=%s\n\n[https]\nhost=%s\nport=%d\n\n[socks]\nhost=''\nport=0\n\n[ftp]\nhost=''\nport=0\n", strings.Join(bypass, ", "), host, c.MixedPort, auth, variant(user), variant(password), host, c.MixedPort)
}

// Keep the original pre-enable backup while changing the managed settings.
func (g GNOME) Reconfigure(ctx context.Context, c state.Settings, previous, next state.SystemProxySettings) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if observed := g.read(ctx, c, previous.Host); !observed.Owned {
		return errors.New("System Proxy was changed externally; disable and restore it before configuring it")
	}
	run := g.runner()
	snapshot, err := run(ctx, "dconf", []string{"dump", "/system/proxy/"}, "")
	if err != nil {
		return err
	}
	_, err = run(ctx, "dconf", []string{"load", "/system/proxy/"}, proxyPayload(c, next))
	if err == nil && !g.read(ctx, c, next.Host).Owned {
		err = errors.New("System Proxy settings could not be verified")
	}
	if err == nil {
		return nil
	}
	// A failed write may be partial. Restore the exact managed state, not the
	// pre-enable backup, so a failed configuration save leaves the proxy on.
	if _, restoreErr := run(ctx, "dconf", []string{"reset", "-f", "/system/proxy/"}, ""); restoreErr != nil {
		return errors.Join(err, fmt.Errorf("restore previous System Proxy settings: %w", restoreErr))
	}
	_, restoreErr := run(ctx, "dconf", []string{"load", "/system/proxy/"}, snapshot+"\n")
	return errors.Join(err, restoreErr)
}
