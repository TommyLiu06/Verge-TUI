package app

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"verge-tui/internal/management"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestSystemProxyPreferencesApplyImmediatelyAndRollbackFailedChanges(t *testing.T) {
	b := settingsBackend(t)
	c, _ := b.Store.Read()
	ctx := context.Background()
	mock := &testutil.Desktop{Original: "[/]\nmode='none'\n"}
	b.Desktop.Run = mock.Run
	a := Action{Kind: "preferences-save", ID: "system-proxy", Fields: map[string]string{"proxy-host": "localhost", "use-default-bypass": "false", "proxy-bypass": "*.example.com, 10.0.0.0/8"}}
	if err := b.Execute(ctx, a); err != nil || mock.Loads != 0 {
		t.Fatal("saving disabled proxy settings changed the desktop", err)
	}
	if err := b.Desktop.Enable(ctx, c); err != nil {
		t.Fatal(err)
	}
	a.Fields["proxy-host"] = "[::1]"
	a.Fields["use-default-bypass"] = "true"
	if err := b.Execute(ctx, a); err != nil {
		t.Fatal("enabled proxy was not reconfigured", err)
	}
	p, err := state.ReadPreferences(b.Store.Root)
	if err != nil || p.SystemProxy.Host != "::1" || !p.SystemProxy.UseDefaultBypass || !strings.Contains(mock.Payload, "'192.168.0.0/16'") || !b.Desktop.Read(ctx, c).Owned {
		t.Fatal("preferences or desktop configuration did not update", err)
	}
	managed := mock.Payload
	mock.FailNextLoad = true
	a.Fields["proxy-host"] = "localhost"
	if b.Execute(ctx, a) == nil {
		t.Fatal("partial desktop write was accepted")
	}
	after, err := state.ReadPreferences(b.Store.Root)
	if err != nil || after != p || mock.Payload != managed {
		t.Fatal("failed save changed preferences or enabled proxy", err)
	}
	for _, invalid := range []map[string]string{{"proxy-host": "http://localhost"}, {"proxy-bypass": "bad'host"}, {"use-default-bypass": "yes"}} {
		if b.Execute(ctx, Action{Kind: "preferences-save", ID: "system-proxy", Fields: invalid}) == nil {
			t.Fatal("accepted invalid proxy preference", invalid)
		}
		got, _ := state.ReadPreferences(b.Store.Root)
		if got != p || mock.Payload != managed {
			t.Fatal("invalid settings changed existing configuration")
		}
	}
	// Saving a host that matches externally modified settings must not turn
	// those settings into an owned proxy or discard the original backup.
	external := strings.ReplaceAll(managed, "host='::1'", "host='192.0.2.1'")
	mock.Payload = external
	a.Fields["proxy-host"] = "192.0.2.1"
	if b.Execute(ctx, a) == nil || mock.Payload != external {
		t.Fatal("save adopted or overwrote externally modified proxy settings")
	}
	unchanged, _ := state.ReadPreferences(b.Store.Root)
	if unchanged != p {
		t.Fatal("save changed preferences after detecting an external proxy change")
	}
	mock.Payload = managed
	coreSettings, err := b.Store.Read()
	if err != nil || !reflect.DeepEqual(coreSettings, c) {
		t.Fatal("desktop preferences changed core listeners or TUN", err)
	}
	if err := b.Desktop.Disable(ctx, c); err != nil || mock.Payload != mock.Original {
		t.Fatal("updated proxy could not restore original settings", err)
	}
}

func TestSystemProxyPreferencesRemainPrivateToSharedServiceUsers(t *testing.T) {
	b := settingsBackend(t)
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
	client := NewRemote(b.Store, "test")
	defer client.Close(ctx)
	if err := client.Execute(ctx, Action{Kind: "preferences-save", ID: "system-proxy", Fields: map[string]string{"proxy-host": "localhost", "use-default-bypass": "false", "proxy-bypass": "*.example.org"}}); err != nil {
		t.Fatal(err)
	}
	v, err := client.Load(ctx)
	if err != nil || v.Preferences.SystemProxy.Host != "localhost" || v.Preferences.SystemProxy.Bypass != "*.example.org" {
		t.Fatal("shared client did not load its private proxy configuration", err)
	}
	servicePreferences, err := state.ReadPreferences(b.Store.Root)
	if err != nil || servicePreferences != state.DefaultPreferences() {
		t.Fatal("ordinary user's proxy preferences changed administrator preferences", err)
	}
	if err := client.Remote.Call(ctx, "POST", "/action", Action{Kind: "preferences-save", ID: "system-proxy"}, nil); err == nil {
		t.Fatal("service exposed administrator proxy settings")
	}
}
