package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
	"verge-tui/internal/desktop"
	"verge-tui/internal/profile"
	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
)

type CoreOptions struct {
	IPv6         bool
	UnifiedDelay bool
	LogLevel     string
}

func coreOptions(c state.Settings, doc map[string]any) CoreOptions {
	o := CoreOptions{IPv6: true, LogLevel: "warning"}
	if value, ok := doc["ipv6"].(bool); ok {
		o.IPv6 = value
	}
	if value, ok := doc["unified-delay"].(bool); ok {
		o.UnifiedDelay = value
	}
	if c.Engine.IPv6 != nil {
		o.IPv6 = *c.Engine.IPv6
	}
	if c.Engine.UnifiedDelay != nil {
		o.UnifiedDelay = *c.Engine.UnifiedDelay
	}
	if c.Engine.LogLevel != "" {
		o.LogLevel = c.Engine.LogLevel
	}
	return o
}

func (b *Backend) preferencesRoot() string {
	if b.Remote != nil {
		return filepath.Join(state.PreferencesUsersPath(b.Store), strconv.Itoa(os.Geteuid()))
	}
	return b.Store.Root
}

func (b *Backend) ReadPreferences() (state.Preferences, error) {
	return state.ReadPreferences(b.preferencesRoot())
}

func (b *Backend) savePreferences(fields map[string]string) error {
	if b.Remote != nil {
		if err := desktop.PrepareUser(&state.Store{Root: b.preferencesRoot()}); err != nil {
			return err
		}
	}
	return state.UpdatePreferences(b.preferencesRoot(), func(p *state.Preferences) error {
		var err error
		for key, value := range fields {
			switch key {
			case "language":
				p.Language = value
			case "theme":
				p.Theme = value
			case "start-page":
				p.StartPage = value
			case "proxy-columns":
				p.ProxyColumns, err = strconv.Atoi(value)
			case "delay-url":
				p.DelayURL = value
			case "delay-timeout":
				p.DelayTimeout, err = strconv.Atoi(value)
			case "proxy-on-login":
				p.ProxyOnLogin, err = boolField(fields, key)
			default:
				return errors.New("unknown application setting")
			}
			if err != nil {
				return fmt.Errorf("invalid %s", key)
			}
		}
		if err := p.Validate(); err != nil {
			return err
		}
		return nil
	})
}

func (b *Backend) saveSystemProxy(ctx context.Context, fields map[string]string) error {
	root := b.preferencesRoot()
	previous, err := state.ReadPreferences(root)
	if err != nil {
		return err
	}
	next := previous
	for key, value := range fields {
		switch key {
		case "proxy-host":
			next.SystemProxy.Host = strings.TrimSpace(value)
			if strings.HasPrefix(next.SystemProxy.Host, "[") && strings.HasSuffix(next.SystemProxy.Host, "]") {
				next.SystemProxy.Host = next.SystemProxy.Host[1 : len(next.SystemProxy.Host)-1]
			}
		case "use-default-bypass":
			next.SystemProxy.UseDefaultBypass, err = boolField(fields, key)
		case "proxy-bypass":
			next.SystemProxy.Bypass = strings.TrimSpace(value)
		default:
			return errors.New("unknown System Proxy setting")
		}
		if err != nil {
			return err
		}
	}
	if err := next.Validate(); err != nil {
		return err
	}
	var c state.Settings
	if b.Remote != nil {
		v, loadErr := b.Load(ctx)
		if loadErr != nil {
			return loadErr
		}
		c = v.Settings
		if err := desktop.PrepareUser(&state.Store{Root: root}); err != nil {
			return err
		}
	} else {
		c, err = b.Store.Read()
		if err != nil {
			return err
		}
	}
	observed := b.Desktop.Read(ctx, c)
	owned := observed.Owned
	if !owned && fileExists(filepath.Join(b.Desktop.Store.Root, "gnome-proxy-backup.ini")) {
		return errors.New("restore System Proxy before changing its settings; the existing backup could not be verified")
	}
	if owned {
		if err := b.Desktop.Reconfigure(ctx, c, previous.SystemProxy, next.SystemProxy); err != nil {
			return err
		}
	}
	if err := state.UpdatePreferences(root, func(p *state.Preferences) error { p.SystemProxy = next.SystemProxy; return nil }); err != nil {
		if owned {
			return errors.Join(err, b.Desktop.Reconfigure(ctx, c, next.SystemProxy, previous.SystemProxy))
		}
		return err
	}
	return nil
}

func list(value string) []string {
	var items []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func boolField(fields map[string]string, key string) (bool, error) {
	switch fields[key] {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s", key)
	}
}

func (b *Backend) saveSettings(ctx context.Context, a Action) error {
	before, err := b.Store.Read()
	if err != nil {
		return err
	}
	if a.ID == "ports" && b.Desktop.Read(ctx, before).Owned {
		return errors.New("disable System Proxy before changing ports")
	}
	err = b.Manager.Apply(ctx, func(c *state.Settings) error {
		// Work on separate maps so a rejected change cannot alter a snapshot.
		if c.Engine.DNS != nil {
			copyDNS := make(map[string]state.DNSSettings, len(c.Engine.DNS))
			for id, dns := range c.Engine.DNS {
				copyDNS[id] = dns
			}
			c.Engine.DNS = copyDNS
		}
		switch a.ID {
		case "allow-lan":
			c.Engine.AllowLAN, err = boolField(a.Fields, "enabled")
		case "ipv6":
			value, parseErr := boolField(a.Fields, "enabled")
			err = parseErr
			c.Engine.IPv6 = &value
		case "unified-delay":
			value, parseErr := boolField(a.Fields, "enabled")
			err = parseErr
			c.Engine.UnifiedDelay = &value
		case "controller":
			enabled, parseErr := boolField(a.Fields, "enabled")
			if parseErr != nil {
				return parseErr
			}
			if !enabled {
				c.ControllerEnabled = false
				break
			}
			address := strings.TrimSpace(a.Fields["address"])
			if strings.HasPrefix(address, ":") {
				address = "127.0.0.1" + address
			}
			host, portText, parseErr := net.SplitHostPort(address)
			if parseErr != nil || net.ParseIP(host) == nil {
				return errors.New("enter a controller IP address and port")
			}
			port, parseErr := strconv.Atoi(portText)
			if parseErr != nil || port < 1024 || port > 65535 {
				return errors.New("controller port must be between 1024 and 65535")
			}
			if port == c.MixedPort || port == c.HTTPPort || port == c.SOCKSPort {
				return errors.New("proxy and controller ports must be different")
			}
			secret := strings.TrimSpace(a.Fields["secret"])
			if secret == "" {
				return errors.New("secret is required")
			}
			if !c.ControllerEnabled || address != c.ControllerAddress {
				listener, listenErr := net.Listen("tcp", address)
				if listenErr != nil {
					return fmt.Errorf("controller address unavailable: %w", listenErr)
				}
				listener.Close()
			}
			c.ControllerEnabled, c.ControllerAddress, c.Secret, c.APIPort = true, address, secret, port
		case "log-level":
			switch a.Fields["level"] {
			case "debug", "info", "warning", "error", "silent":
				c.Engine.LogLevel = a.Fields["level"]
			default:
				return errors.New("invalid log level")
			}
		case "ports":
			next := []int{c.MixedPort, c.HTTPPort, c.SOCKSPort}
			old := append([]int(nil), next...)
			seen := map[int]bool{c.APIPort: true}
			for i, key := range []string{"mixed-port", "http-port", "socks-port"} {
				port, err := strconv.Atoi(a.Fields[key])
				if err != nil || port < 0 || port > 65535 || port > 0 && port < 1024 || i == 0 && port == 0 {
					return fmt.Errorf("%s must be between 1024 and 65535 (0 disables optional ports)", key)
				}
				if port != 0 {
					if seen[port] {
						return errors.New("proxy and controller ports must be different")
					}
					seen[port] = true
					if port != old[i] {
						if err := state.CheckPort(port); err != nil {
							return err
						}
					}
				}
				next[i] = port
			}
			c.MixedPort, c.HTTPPort, c.SOCKSPort = next[0], next[1], next[2]
		case "tun":
			if c.InstallMode != "system" {
				return errors.New("TUN settings require a system installation")
			}
			t := state.DefaultTun()
			t.Stack, t.Device = a.Fields["stack"], a.Fields["device"]
			if t.Stack != "system" && t.Stack != "gvisor" && t.Stack != "mixed" {
				return errors.New("unsupported TUN stack")
			}
			if len(t.Device) > 15 || strings.ContainsAny(t.Device, "/ \t\n") {
				return errors.New("invalid TUN device name")
			}
			if t.AutoRoute, err = boolField(a.Fields, "auto-route"); err != nil {
				return err
			}
			if t.AutoDetect, err = boolField(a.Fields, "auto-detect"); err != nil {
				return err
			}
			if t.StrictRoute, err = boolField(a.Fields, "strict-route"); err != nil {
				return err
			}
			if t.MTU, err = strconv.Atoi(a.Fields["mtu"]); err != nil || t.MTU < 1280 || t.MTU > 65535 {
				return errors.New("MTU must be between 1280 and 65535")
			}
			t.DNSHijack, t.RouteExclude = list(a.Fields["dns-hijack"]), list(a.Fields["route-exclude"])
			for _, item := range t.RouteExclude {
				if _, _, err := net.ParseCIDR(item); err != nil {
					return errors.New("route exclusions must be IP CIDRs")
				}
			}
			c.Engine.Tun = &t
		case "dns", "dns-toggle":
			if c.Current == "" || c.Current != a.Node {
				return errors.New("the active profile changed; reopen DNS settings")
			}
			listen, hasListen := a.Fields["listen"]
			if hasListen && c.InstallMode != "system" {
				return errors.New("DNS listening requires a system installation")
			}
			dns, ok := c.Engine.DNS[c.Current]
			if !ok {
				dns = state.DefaultDNS()
			}
			if a.ID == "dns-toggle" {
				dns.Enabled = !dns.Enabled
			} else {
				dns.Enabled = true
				dns.Mode, dns.FakeIPRange = a.Fields["mode"], a.Fields["fake-ip-range"]
				if dns.Mode != "fake-ip" && dns.Mode != "redir-host" {
					return errors.New("invalid DNS enhanced mode")
				}
				if dns.IPv6, err = boolField(a.Fields, "ipv6"); err != nil {
					return err
				}
				if _, _, err := net.ParseCIDR(dns.FakeIPRange); err != nil {
					return errors.New("invalid fake IP range")
				}
				dns.Nameservers, dns.DefaultNameservers, dns.Fallback = list(a.Fields["nameservers"]), list(a.Fields["default-nameservers"]), list(a.Fields["fallback"])
				if len(dns.Nameservers) == 0 || len(dns.DefaultNameservers) == 0 {
					return errors.New("nameservers and default nameservers are required")
				}
				if hasListen {
					dns.Listen = strings.TrimSpace(listen)
					if dns.Listen != "" {
						_, portText, parseErr := net.SplitHostPort(dns.Listen)
						if parseErr != nil {
							return errors.New("enter a DNS listen address and port")
						}
						port, parseErr := strconv.Atoi(portText)
						if parseErr != nil || port < 1 || port > 65535 {
							return errors.New("DNS listen port must be between 1 and 65535")
						}
					}
				}
			}
			if c.Engine.DNS == nil {
				c.Engine.DNS = map[string]state.DNSSettings{}
			}
			c.Engine.DNS[c.Current] = dns
		default:
			return errors.New("unknown core setting")
		}
		return err
	})
	if err != nil {
		return err
	}
	if a.ID == "ports" {
		return shellenv.Refresh(b.Store)
	}
	return nil
}

// Inspect is requested on demand, keeping large documents out of normal views.
func (b *Backend) Inspect(ctx context.Context, kind string) ([]string, error) {
	if kind == "controller" {
		if b.Remote != nil {
			var lines []string
			err := b.Remote.Call(ctx, "GET", "/inspect/controller", nil, &lines)
			return lines, err
		}
		c, err := b.Store.Read()
		if err != nil {
			return nil, err
		}
		address := c.ControllerAddress
		if address == "" {
			address = "127.0.0.1:" + strconv.Itoa(c.APIPort)
		}
		return []string{strconv.FormatBool(c.ControllerEnabled), address, c.Secret}, nil
	}
	if kind == "interfaces" {
		interfaces, err := net.Interfaces()
		if err != nil {
			return nil, err
		}
		var lines []string
		for _, iface := range interfaces {
			lines = append(lines, iface.Name+"  "+iface.Flags.String(), "MAC: "+iface.HardwareAddr.String())
			addresses, err := iface.Addrs()
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				lines = append(lines, "  "+address.String())
			}
		}
		return lines, nil
	}
	if b.Remote != nil {
		var lines []string
		err := b.Remote.Call(ctx, "GET", "/inspect/"+url.PathEscape(kind), nil, &lines)
		return lines, err
	}
	if kind != "runtime" {
		return nil, errors.New("unknown settings viewer")
	}
	c, err := b.Store.Read()
	if err != nil {
		return nil, err
	}
	data, err := profile.Runtime(b.Store, c, c.Current)
	if err != nil {
		return nil, err
	}
	doc, err := profile.Parse(data)
	if err != nil {
		return nil, err
	}
	data, err = yaml.Marshal(redactConfig(doc))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) > 10000 {
		lines = append(lines[:10000], "# Viewer truncated after 10000 lines.")
	}
	return lines, nil
}

func redactConfig(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "key") || lower == "uuid" || lower == "id" || lower == "auth" || strings.HasPrefix(lower, "auth-") || lower == "authentication" || lower == "users" || lower == "url" || lower == "username" || lower == "headers" {
				v[key] = "[redacted]"
			} else {
				v[key] = redactConfig(item)
			}
		}
	case []any:
		for i, item := range v {
			v[i] = redactConfig(item)
		}
	case string:
		// Resolver and other nested URLs can carry user credentials or tokens
		// in their query/path even when the mapping key is not named "url".
		if u, err := url.Parse(v); err == nil && u.Scheme != "" && u.Host != "" {
			return "[redacted URL]"
		}
	}
	return value
}

func (b *Backend) readDelayPreferences(a Action) (Action, error) {
	if a.DelayTimeout != 0 {
		return a, nil
	}
	p, err := state.ReadPreferences(b.preferencesRoot())
	if err != nil {
		return a, err
	}
	a.DelayTimeout = p.DelayTimeout
	if a.Value == "" {
		a.Value = p.DelayURL
	}
	return a, nil
}
