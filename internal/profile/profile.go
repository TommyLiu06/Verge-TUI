package profile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
	"verge-tui/internal/state"
)

const MaxSize = 16 << 20
const DefaultConfig = "proxies: []\nproxy-groups:\n  - name: Proxy\n    type: select\n    proxies: [DIRECT]\nrules:\n  - MATCH,Proxy\n"
const defaultUserAgent = "clash-verge/v2.5.8"

func Parse(data []byte) (map[string]any, error) {
	if len(data) == 0 || len(data) > MaxSize {
		return nil, errors.New("profile must be between 1 byte and 16 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid YAML profile: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("profile must contain one YAML document")
	}
	if doc == nil {
		return nil, errors.New("profile must be a YAML mapping")
	}
	if _, a := doc["proxies"]; !a {
		if _, b := doc["proxy-providers"]; !b {
			return nil, errors.New("expected a Clash YAML profile with proxies or proxy-providers")
		}
	}
	return doc, nil
}

func Add(s *state.Store, p state.Profile, data []byte) (state.Profile, error) {
	if p.Type != "local" && p.Type != "remote" {
		return p, errors.New("profile type must be Remote or Local")
	}
	if _, err := Parse(data); err != nil {
		return p, err
	}
	id, err := state.Token()
	if err != nil {
		return p, err
	}
	p.ID = id[:16]
	if p.Name == "" {
		p.Name = p.Type + " file"
	}
	p.Updated = time.Now()
	path, _ := s.ProfilePath(p.ID)
	if err = state.Write(path, data, 0600); err != nil {
		return p, err
	}
	err = s.Update(func(c *state.Settings) error { c.Profiles = append(c.Profiles, p); return nil })
	if err != nil {
		os.Remove(path)
	}
	return p, err
}

func Local(s *state.Store, name, description, path string) (state.Profile, error) {
	data := []byte(DefaultConfig)
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return state.Profile{}, err
		}
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, MaxSize+1))
		if err != nil {
			return state.Profile{}, err
		}
		if name == "" {
			name = filepath.Base(path)
		}
	}
	return Add(s, state.Profile{Name: name, Type: "local", Description: description}, data)
}

func Remote(ctx context.Context, s *state.Store, p state.Profile) (state.Profile, error) {
	p.Type = "remote"
	data, result, err := Download(ctx, p)
	if err != nil {
		return p, err
	}
	return Add(s, result, data)
}

func Download(ctx context.Context, p state.Profile) ([]byte, state.Profile, error) {
	u, err := url.Parse(p.URL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, p, errors.New("enter an HTTP or HTTPS subscription URL without embedded credentials")
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30
	}
	if timeout < 1 || timeout > 300 {
		return nil, p, errors.New("HTTP Request Timeout must be between 1 and 300 seconds")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, p, err
	}
	ua := p.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	// Subscription fetches are direct and never inherit the host's proxy settings.
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Duration(timeout) * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, p, errors.New("subscription request failed (check connectivity, URL and certificate)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, p, fmt.Errorf("subscription returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxSize+1))
	if err != nil {
		return nil, p, err
	}
	if _, err = Parse(data); err != nil {
		return nil, p, err
	}
	if p.Name == "" {
		_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
		p.Name = params["filename"]
		if p.Name == "" {
			p.Name = u.Hostname()
		}
	}
	p.Usage = ParseUsage(resp.Header.Get("Subscription-Userinfo"))
	p.Home = resp.Header.Get("Profile-Web-Page-Url")
	p.Updated = time.Now()
	return data, p, nil
}

func ParseUsage(header string) *state.Usage {
	if header == "" {
		return nil
	}
	u := &state.Usage{}
	for _, field := range strings.Split(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "upload":
			u.Upload = n
		case "download":
			u.Download = n
		case "total":
			u.Total = n
		case "expire":
			if n <= 1<<63-1 {
				u.Expire = int64(n)
			}
		}
	}
	return u
}

func Runtime(s *state.Store, c state.Settings, id string) ([]byte, error) {
	doc := map[string]any{}
	if id != "" {
		if _, found := state.Find(c, id); !found {
			return nil, errors.New("profile does not exist")
		}
		path, err := s.ProfilePath(id)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		doc, err = Parse(data)
		if err != nil {
			return nil, err
		}
	} else {
		doc, _ = Parse([]byte(DefaultConfig))
	}
	return RuntimeDocument(c, doc)
}

func RuntimeData(c state.Settings, data []byte) ([]byte, error) {
	doc, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return RuntimeDocument(c, doc)
}

func RuntimeDocument(c state.Settings, doc map[string]any) ([]byte, error) {
	for _, key := range []string{"external-controller-unix", "external-controller-pipe", "external-controller-tls", "external-ui", "external-ui-url", "external-doh-server", "external-controller-cors", "listeners", "tunnels"} {
		delete(doc, key)
	}
	doc["mixed-port"] = c.MixedPort
	doc["port"] = c.HTTPPort
	doc["socks-port"] = c.SOCKSPort
	for _, key := range []string{"redir-port", "tproxy-port"} {
		doc[key] = 0
	}
	doc["external-controller"] = ""
	if c.ControllerEnabled {
		address := c.ControllerAddress
		if address == "" {
			address = "127.0.0.1:" + strconv.Itoa(c.APIPort)
		}
		doc["external-controller"] = address
	}
	if c.ControllerSocket != "" {
		doc["external-controller-unix"] = c.ControllerSocket
	}
	doc["secret"] = c.Secret
	doc["allow-lan"] = c.Engine.AllowLAN
	doc["bind-address"] = "127.0.0.1"
	if c.Engine.AllowLAN {
		doc["bind-address"] = "*"
	}
	doc["skip-auth-prefixes"] = []string{}
	doc["authentication"] = []string{c.ProxyUser + ":" + c.ProxyPassword}
	if c.InstallMode == "system" {
		doc["authentication"] = []string{}
	}
	doc["mode"] = c.Mode
	doc["profile"] = map[string]any{"store-selected": true}
	doc["log-level"] = "warning"
	if c.Engine.LogLevel != "" {
		doc["log-level"] = c.Engine.LogLevel
	}
	if c.Engine.IPv6 != nil {
		doc["ipv6"] = *c.Engine.IPv6
	}
	if c.Engine.UnifiedDelay != nil {
		doc["unified-delay"] = *c.Engine.UnifiedDelay
	}
	doc["find-process-mode"] = "off"
	tun := state.DefaultTun()
	if c.Engine.Tun != nil {
		tun = *c.Engine.Tun
	}
	doc["tun"] = map[string]any{"enable": c.Tun && c.InstallMode == "system", "stack": tun.Stack, "device": tun.Device, "auto-route": tun.AutoRoute, "auto-detect-interface": tun.AutoDetect, "strict-route": tun.StrictRoute, "mtu": tun.MTU, "dns-hijack": tun.DNSHijack, "route-exclude-address": tun.RouteExclude}
	if override, ok := c.Engine.DNS[c.Current]; ok && override.Enabled {
		dns, ok := doc["dns"].(map[string]any)
		if !ok {
			dns = map[string]any{}
		}
		dns["enable"], dns["enhanced-mode"], dns["ipv6"] = true, override.Mode, override.IPv6
		dns["nameserver"], dns["default-nameserver"], dns["fallback"] = override.Nameservers, override.DefaultNameservers, override.Fallback
		dns["fake-ip-range"] = override.FakeIPRange
		if c.InstallMode == "system" && strings.TrimSpace(override.Listen) != "" {
			dns["listen"] = strings.TrimSpace(override.Listen)
		}
		doc["dns"] = dns
	}
	if dns, ok := doc["dns"].(map[string]any); ok && c.InstallMode != "system" {
		delete(dns, "listen")
	}
	if c.Tun && c.InstallMode == "system" {
		if _, ok := doc["dns"]; !ok {
			doc["dns"] = map[string]any{"enable": true, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "nameserver": []string{"https://1.1.1.1/dns-query", "https://dns.alidns.com/dns-query"}}
		}
	}
	return yaml.Marshal(doc)
}
