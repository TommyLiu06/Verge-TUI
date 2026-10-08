package desktop

import (
	"context"
	"errors"
	"fmt"
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
	Store *state.Store
	Run   Runner
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
func (g GNOME) Read(ctx context.Context, c state.Settings) Status {
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
	status.Address = strings.Trim(host, "'") + ":" + strings.TrimSpace(port)
	_, err = os.Stat(filepath.Join(g.Store.Root, "gnome-proxy-backup.ini"))
	status.Owned = err == nil && mode == "'manual'" && status.Address == "127.0.0.1:"+strconv.Itoa(c.MixedPort)
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
	auth := "false"
	user := ""
	password := ""
	if c.InstallMode == "user" {
		auth = "true"
		user = c.ProxyUser
		password = c.ProxyPassword
	}
	payload := fmt.Sprintf("[/]\nmode='manual'\nuse-same-proxy=false\nautoconfig-url=''\nignore-hosts=['localhost', '127.0.0.0/8', '::1']\n\n[http]\nhost='127.0.0.1'\nport=%d\nuse-authentication=%s\nauthentication-user=%s\nauthentication-password=%s\n\n[https]\nhost='127.0.0.1'\nport=%d\n\n[socks]\nhost=''\nport=0\n\n[ftp]\nhost=''\nport=0\n", c.MixedPort, auth, variant(user), variant(password), c.MixedPort)
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
