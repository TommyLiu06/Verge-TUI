package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"verge-tui/internal/testutil"

	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/management"
	"verge-tui/internal/profile"
	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
)

func settingsBackend(t *testing.T) *Backend {
	t.Helper()
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	s, err := state.Open(testutil.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	return New(s, "test")
}

func TestSettingsRejectInvalidChangesWithoutWriting(t *testing.T) {
	b := settingsBackend(t)
	c, _ := b.Store.Read()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupied := listener.Addr().(*net.TCPAddr).Port
	for _, action := range []Action{
		{Kind: "settings-save", ID: "ipv6", Fields: map[string]string{"enabled": "maybe"}},
		{Kind: "settings-save", ID: "log-level", Fields: map[string]string{"level": "trace"}},
		{Kind: "settings-save", ID: "tun", Fields: map[string]string{"stack": "system"}},
		{Kind: "settings-save", ID: "ports", Fields: map[string]string{"mixed-port": strconv.Itoa(c.APIPort), "http-port": "0", "socks-port": "0"}},
		{Kind: "settings-save", ID: "ports", Fields: map[string]string{"mixed-port": strconv.Itoa(occupied), "http-port": "0", "socks-port": "0"}},
		{Kind: "preferences-save", Fields: map[string]string{"delay-timeout": "0"}},
	} {
		if err := b.Execute(context.Background(), action); err == nil {
			t.Fatalf("accepted invalid change: %+v", action)
		}
		after, err := b.Store.Read()
		if err != nil || !reflect.DeepEqual(c, after) {
			t.Fatal("rejected settings modified persisted state", err)
		}
	}
}

func TestDNSOverridesAreProfileScopedAndProtectController(t *testing.T) {
	b := settingsBackend(t)
	ctx := context.Background()
	first, err := profile.Add(b.Store, state.Profile{Type: "local"}, []byte(profile.DefaultConfig+"dns: {enable: true, nameserver: [8.8.8.8], nameserver-policy: {example.com: 9.9.9.9}, listen: '0.0.0.0:53'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := profile.Local(b.Store, "Other", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "profile-select", ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns", Node: first.ID, Fields: map[string]string{"mode": "redir-host", "ipv6": "false", "nameservers": "1.1.1.1, https://dns.alidns.com/dns-query", "default-nameservers": "223.5.5.5", "fake-ip-range": "198.18.0.1/16"}}); err != nil {
		t.Fatal(err)
	}
	c, _ := b.Store.Read()
	data, err := profile.Runtime(b.Store, c, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := profile.Parse(data)
	dns := doc["dns"].(map[string]any)
	if dns["enhanced-mode"] != "redir-host" || dns["nameserver-policy"] == nil || dns["listen"] != nil || doc["external-controller"] != "" || doc["external-controller-unix"] != c.ControllerSocket {
		t.Fatal("DNS override replaced unrelated policy or weakened listeners")
	}
	if err := b.Execute(ctx, Action{Kind: "profile-select", ID: second.ID}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns-toggle", Node: first.ID}); err == nil {
		t.Fatal("stale DNS form modified the newly selected profile")
	}
	c, _ = b.Store.Read()
	data, _ = profile.Runtime(b.Store, c, second.ID)
	doc, _ = profile.Parse(data)
	if doc["dns"] != nil {
		t.Fatal("DNS override leaked into another profile")
	}
	if err := b.Execute(ctx, Action{Kind: "profile-select", ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns-toggle", Node: first.ID}); err != nil {
		t.Fatal(err)
	}
	c, _ = b.Store.Read()
	data, _ = profile.Runtime(b.Store, c, first.ID)
	doc, _ = profile.Parse(data)
	dns = doc["dns"].(map[string]any)
	if dns["enhanced-mode"] != nil || dns["nameserver"].([]any)[0] != "8.8.8.8" {
		t.Fatal("disabling override did not restore the profile DNS")
	}
}

func TestSharedSettingsAndPrivatePreferences(t *testing.T) {
	b := settingsBackend(t)
	shortenStore(t, b.Store)
	if err := b.Store.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.RWMutex
	server, err := management.Serve(ctx, b.Store, b.SharedHandlers(&mu))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	remote := NewRemote(b.Store, "test")
	defer remote.Close(ctx)
	if err := remote.Execute(ctx, Action{Kind: "preferences-save", Fields: map[string]string{"start-page": "settings", "theme": "dark", "delay-timeout": "12000"}}); err != nil {
		t.Fatal(err)
	}
	rootPrefs, err := state.ReadPreferences(b.Store.Root)
	if err != nil || rootPrefs != state.DefaultPreferences() {
		t.Fatal("ordinary user's preferences changed the service preferences", err)
	}
	v, err := remote.Load(ctx)
	if err != nil || v.Preferences.StartPage != "settings" || v.Preferences.DelayTimeout != 12000 {
		t.Fatal("per-user preferences not loaded", err)
	}
	if err := remote.Execute(ctx, Action{Kind: "settings-save", ID: "ipv6", Fields: map[string]string{"enabled": "false"}}); err != nil {
		t.Fatal(err)
	}
	c, _ := b.Store.Read()
	if c.Engine.IPv6 == nil || *c.Engine.IPv6 {
		t.Fatal("shared settings did not reach the service")
	}
	data := strings.Replace(profile.DefaultConfig, "proxies: []", "proxies: [{name: Example, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: node-password}, {name: WireGuard, type: wireguard, server: example.com, port: 51820, private-key: wg-private, pre-shared-key: wg-preshared}]", 1) + "proxy-providers:\n  sample:\n    type: http\n    url: https://example.com/private-token\n    headers: {Authorization: private-header}\ndns: {nameserver: ['https://dns.example/private-resolver-token']}\n"
	p, err := profile.Add(b.Store, state.Profile{Type: "local"}, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Execute(ctx, Action{Kind: "profile-select", ID: p.ID}); err != nil {
		t.Fatal(err)
	}
	lines, err := remote.Inspect(ctx, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	for _, secret := range []string{c.Secret, c.ProxyPassword, "node-password", "private-token", "private-header", "wg-private", "wg-preshared", "private-resolver-token"} {
		if strings.Contains(text, secret) {
			t.Fatal("runtime viewer leaked a credential", secret)
		}
	}
	if !strings.Contains(text, "[redacted]") || !strings.Contains(text, "mixed-port:") {
		t.Fatal("runtime viewer unavailable or unredacted")
	}
	if _, err := remote.Inspect(ctx, "../settings.json"); err == nil {
		t.Fatal("viewer accepted an arbitrary private path")
	}
}

func TestSettingsReloadAndEnvironmentWithRealCore(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY")
	}
	b := settingsBackend(t)
	ctx := context.Background()
	if _, err := core.Import(ctx, b.Store, "test", binary); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Use(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if err := shellenv.Write(b.Store); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	for _, action := range []Action{{Kind: "settings-save", ID: "ipv6", Fields: map[string]string{"enabled": "false"}}, {Kind: "settings-save", ID: "unified-delay", Fields: map[string]string{"enabled": "true"}}, {Kind: "settings-save", ID: "log-level", Fields: map[string]string{"level": "error"}}} {
		if err := b.Execute(ctx, action); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := b.Store.Read()
	client := controller.New(c)
	defer client.Close()
	var config map[string]any
	if err := client.Request(ctx, "GET", "/configs", nil, &config); err != nil {
		t.Fatal(err)
	}
	if config["ipv6"] != false || config["log-level"] != "error" {
		t.Fatal("settings did not reach the running core", config)
	}
	newPort, err := state.FreePort(c.APIPort)
	if err != nil {
		t.Fatal(err)
	}
	httpPort, err := state.FreePort(newPort)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "ports", Fields: map[string]string{"mixed-port": strconv.Itoa(newPort), "http-port": strconv.Itoa(httpPort), "socks-port": "0"}}); err != nil {
		t.Fatal(err)
	}
	c, _ = b.Store.Read()
	env, err := shellenv.ReadFile(shellenv.FilePath(b.Store))
	if err != nil || env.MixedPort != newPort {
		t.Fatal("proxy.env still contains the old port", err)
	}
	snapshot, err := client.Snapshot(ctx)
	if err != nil || snapshot.Config.MixedPort != newPort {
		t.Fatal("live mixed port not updated", err)
	}
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(newPort))
	if err != nil {
		t.Fatal("new proxy listener not running", err)
	}
	conn.Close()
	conn, err = net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(httpPort))
	if err != nil {
		t.Fatal("optional HTTP listener not running", err)
	}
	conn.Close()
}

func TestDelayUsesSavedPreferencesAndExplicitOverride(t *testing.T) {
	var expectedURL string
	b := delayBackend(t, []string{"Node"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != expectedURL || r.URL.Query().Get("timeout") != "12000" {
			t.Error("saved URL or timeout ignored")
		}
		json.NewEncoder(w).Encode(map[string]int{"delay": 6000})
	})
	ctx := context.Background()
	if err := b.Execute(ctx, Action{Kind: "preferences-save", Fields: map[string]string{"delay-url": "https://example.com/global", "delay-timeout": "12000"}}); err != nil {
		t.Fatal(err)
	}
	expectedURL = "https://example.com/global"
	if err := b.TestDelay(ctx, Action{ID: "Proxy", Node: "Node"}, nil); err != nil {
		t.Fatal(err)
	}
	expectedURL = "https://example.com/override"
	if err := b.TestDelay(ctx, Action{ID: "Proxy", Node: "Node", Value: expectedURL}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTUNSettingsValidateWithRealCoreWithoutEnablingTUN(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY")
	}
	b := settingsBackend(t)
	ctx := context.Background()
	if _, err := core.Import(ctx, b.Store, "test", binary); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Use(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if err := b.Store.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{"stack": "mixed", "device": "Mihomo", "auto-route": "true", "auto-detect": "true", "strict-route": "false", "mtu": "1500", "dns-hijack": "any:53", "route-exclude": "192.168.0.0/16, fd00::/8"}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "tun", Fields: fields}); err != nil {
		t.Fatal(err)
	}
	c, _ := b.Store.Read()
	if c.Tun || c.Engine.Tun == nil || c.Engine.Tun.Stack != "mixed" || len(c.Engine.Tun.RouteExclude) != 2 {
		t.Fatal("TUN form changed enabled state or lost settings")
	}
	fields["route-exclude"] = "not-a-cidr"
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "tun", Fields: fields}); err == nil {
		t.Fatal("invalid route exclusion accepted")
	}
	after, _ := b.Store.Read()
	if !reflect.DeepEqual(c, after) {
		t.Fatal("invalid TUN settings replaced the working configuration")
	}
}

func TestGeoUpdateUsesAuthenticatedCoreEndpoint(t *testing.T) {
	called := false
	b := delayBackend(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/configs/geo" {
			t.Error("wrong geodata update endpoint")
		}
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	if err := b.Execute(context.Background(), Action{Kind: "geo-update"}); err != nil || !called {
		t.Fatal("geodata update not requested", err)
	}
}
