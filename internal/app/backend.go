package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"mihomo-tui/internal/controller"
	"mihomo-tui/internal/core"
	"mihomo-tui/internal/desktop"
	"mihomo-tui/internal/profile"
	"mihomo-tui/internal/shellenv"
	"mihomo-tui/internal/state"
)

type View struct {
	Settings   state.Settings
	Snapshot   controller.Snapshot
	Online     bool
	CoreStatus core.Status
	Desktop    desktop.Status
	Cores      []core.Installed
	OS         string
	Version    string
	Owned      bool
	GroupOrder []string
}
type Action struct {
	Kind, ID, Node, Value string
	Profile               state.Profile
	File                  string
}
type Backend struct {
	Store   *state.Store
	Manager *core.Manager
	Desktop desktop.GNOME
	Version string
}

func New(s *state.Store, version string) *Backend {
	return &Backend{Store: s, Manager: &core.Manager{Store: s}, Desktop: desktop.GNOME{Store: s}, Version: version}
}
func (b *Backend) PrepareUninstall(ctx context.Context) error {
	running, err := core.Running(b.Store)
	if err != nil {
		return err
	}
	if running {
		return errors.New("core is still running; quit the TUI or stop the foreground supervisor before uninstalling")
	}
	backup := filepath.Join(b.Store.Root, "gnome-proxy-backup.ini")
	if _, err := os.Lstat(backup); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	c, err := b.Store.Read()
	if err != nil {
		return err
	}
	return b.Desktop.Disable(ctx, c)
}
func (b *Backend) Close(ctx context.Context) error {
	if !b.Manager.Owned() {
		return nil
	}
	c, err := b.Store.Read()
	if err != nil {
		return errors.Join(err, b.Manager.Stop())
	}
	var restoreErr error
	if b.Desktop.Read(ctx, c).Owned {
		restoreErr = b.Desktop.Disable(ctx, c)
	}
	return errors.Join(restoreErr, b.Manager.Stop())
}
func OSInfo() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return "Linux"
	}
	str := func(b []byte) string {
		for i, c := range b {
			if c == 0 {
				return string(b[:i])
			}
		}
		return string(b)
	}
	return str(u.Sysname[:]) + " " + str(u.Release[:]) + " / " + str(u.Machine[:])
}
func (b *Backend) Load(ctx context.Context) (View, error) {
	v := View{OS: OSInfo(), Version: b.Version, Owned: b.Manager.Owned()}
	c, err := b.Store.Read()
	if err != nil {
		return v, err
	}
	v.Settings = c
	if path, pathErr := b.Store.ProfilePath(c.Current); pathErr == nil {
		if data, readErr := os.ReadFile(path); readErr == nil {
			if doc, parseErr := profile.Parse(data); parseErr == nil {
				if groups, ok := doc["proxy-groups"].([]any); ok {
					for _, item := range groups {
						if group, ok := item.(map[string]any); ok {
							if name, ok := group["name"].(string); ok {
								v.GroupOrder = append(v.GroupOrder, name)
							}
						}
					}
				}
			}
		}
	}
	client := controller.New(c)
	defer client.Close()
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	snap, err := client.Snapshot(probe)
	if err == nil {
		v.Snapshot = snap
		v.Online = true
		v.CoreStatus = core.ReadStatus(b.Store)
	}
	v.Desktop = b.Desktop.Read(ctx, c)
	v.Cores, err = core.List(b.Store)
	return v, err
}
func (b *Backend) Execute(ctx context.Context, a Action) error {
	switch a.Kind {
	case "refresh":
		return nil
	case "start":
		return b.Manager.Start(ctx)
	case "stop":
		if !b.Manager.Owned() {
			return errors.New("this core is owned by a service; use systemctl to stop it")
		}
		c, err := b.Store.Read()
		if err != nil {
			return err
		}
		observed := b.Desktop.Read(ctx, c)
		if observed.Owned {
			if err := b.Desktop.Disable(ctx, c); err != nil {
				return err
			}
		}
		return b.Manager.Stop()
	case "core-install":
		item, err := core.Download(ctx, b.Store, a.Value)
		if err != nil {
			return err
		}
		c, err := b.Store.Read()
		if err != nil {
			return err
		}
		if c.Core == "" {
			return b.Manager.Use(ctx, item.ID)
		}
		return nil
	case "core-use":
		return b.Manager.Use(ctx, a.ID)
	case "mode":
		return b.Manager.Apply(ctx, func(c *state.Settings) error {
			if a.Value != "rule" && a.Value != "global" && a.Value != "direct" {
				return errors.New("invalid proxy mode")
			}
			c.Mode = a.Value
			return nil
		})
	case "tun":
		return b.Manager.Apply(ctx, func(c *state.Settings) error {
			if c.InstallMode != "system" {
				return errors.New("TUN Mode Service Required; configure a system service first")
			}
			c.Tun = !c.Tun
			return nil
		})
	case "system-proxy":
		c, err := b.Store.Read()
		if err != nil {
			return err
		}
		observed := b.Desktop.Read(ctx, c)
		if observed.Owned || !observed.Enabled && fileExists(filepath.Join(b.Store.Root, "gnome-proxy-backup.ini")) {
			return b.Desktop.Disable(ctx, c)
		}
		client := controller.New(c)
		defer client.Close()
		var version struct{ Version string }
		if err := client.Request(ctx, "GET", "/version", nil, &version); err != nil {
			return errors.New("start the core before enabling System Proxy")
		}
		return b.Desktop.Enable(ctx, c)
	case "profile-import":
		_, err := profile.Remote(ctx, b.Store, a.Profile)
		return err
	case "profile-new":
		if a.Profile.Type == "remote" {
			_, err := profile.Remote(ctx, b.Store, a.Profile)
			return err
		}
		_, err := profile.Local(b.Store, a.Profile.Name, a.Profile.Description, a.File)
		return err
	case "profile-select":
		return b.Manager.Apply(ctx, func(c *state.Settings) error {
			if _, ok := state.Find(*c, a.ID); !ok {
				return errors.New("profile does not exist")
			}
			c.Current = a.ID
			return nil
		})
	case "profile-update":
		return b.updateProfile(ctx, a.ID)
	case "home-group":
		return b.Store.Update(func(c *state.Settings) error { c.HomeGroup[c.Current] = a.ID; return nil })
	case "node-select":
		c, err := b.Store.Read()
		if err != nil {
			return err
		}
		client := controller.New(c)
		defer client.Close()
		snap, err := client.Snapshot(ctx)
		if err != nil {
			return err
		}
		g, ok := snap.Proxies[a.ID]
		if !ok || g.Type == "LoadBalance" || g.Type == "Relay" {
			return errors.New("this group does not support manual selection")
		}
		found := false
		for _, name := range g.All {
			if name == a.Node {
				found = true
				break
			}
		}
		if !found {
			return errors.New("node is not a member of the group")
		}
		return client.Select(ctx, a.ID, a.Node)
	case "delay":
		return b.TestDelay(ctx, a, nil)
	}
	return errors.New("unknown action")
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func (b *Backend) updateProfile(ctx context.Context, id string) error {
	c, err := b.Store.Read()
	if err != nil {
		return err
	}
	p, ok := state.Find(c, id)
	if !ok || p.Type != "remote" {
		return errors.New("only remote profiles can be updated")
	}
	data, updated, err := profile.Download(ctx, p)
	if err != nil {
		return err
	}
	if c.Current == id && c.Core != "" {
		payload, err := profile.RuntimeData(c, data)
		if err != nil {
			return err
		}
		if err := b.Manager.Validate(ctx, c, payload); err != nil {
			return err
		}
		client := controller.New(c)
		defer client.Close()
		running, err := core.Running(b.Store)
		if err != nil {
			return err
		}
		if running {
			if err := client.Reload(ctx, payload); err != nil {
				return err
			}
		}
		if err := state.Write(filepath.Join(b.Store.Root, "runtime", "config.yaml"), payload, 0600); err != nil {
			return err
		}
	}
	path, err := b.Store.ProfilePath(id)
	if err != nil {
		return err
	}
	if err := state.Write(path, data, 0600); err != nil {
		return err
	}
	return b.Store.Update(func(c *state.Settings) error {
		for i, p := range c.Profiles {
			if p.ID == id {
				c.Profiles[i] = updated
				return nil
			}
		}
		return errors.New("profile was removed during update")
	})
}
func Groups(v View) []controller.Proxy {
	mode := v.Settings.Mode
	if v.Online {
		mode = v.Snapshot.Config.Mode
	}
	if mode == "direct" {
		return nil
	}
	if mode == "global" {
		g, ok := v.Snapshot.Proxies["GLOBAL"]
		if ok {
			return []controller.Proxy{g}
		}
		return nil
	}
	var groups []controller.Proxy
	for name, g := range v.Snapshot.Proxies {
		if len(g.All) > 0 && name != "GLOBAL" && !g.Hidden {
			groups = append(groups, g)
		}
	}
	order := map[string]int{}
	for i, name := range v.GroupOrder {
		order[name] = i + 1
	}
	sort.Slice(groups, func(i, j int) bool {
		left, right := order[groups[i].Name], order[groups[j].Name]
		if left != 0 && right == 0 {
			return true
		}
		if left == 0 && right != 0 {
			return false
		}
		if left != right {
			return left < right
		}
		return groups[i].Name < groups[j].Name
	})
	return groups
}
func HomeGroup(v View) controller.Proxy {
	groups := Groups(v)
	if len(groups) == 0 {
		return controller.Proxy{}
	}
	if !v.Online && v.Settings.Mode == "global" || v.Online && v.Snapshot.Config.Mode == "global" {
		return groups[0]
	}
	for _, g := range groups {
		if g.Name == v.Settings.HomeGroup[v.Settings.Current] {
			return g
		}
	}
	for _, g := range groups {
		if g.Type != "Selector" && g.Type != "URLTest" {
			continue
		}
		for _, keyword := range []string{"auto", "select", "proxy", "节点选择", "自动选择"} {
			if strings.Contains(strings.ToLower(g.Name), keyword) {
				return g
			}
		}
	}
	for _, g := range groups {
		if g.Type == "Selector" || g.Type == "URLTest" {
			return g
		}
	}
	return controller.Proxy{}
}
func ProxyEnvironment(c state.Settings) string {
	return strings.TrimSuffix(shellenv.Exports(c), "\n")
}
