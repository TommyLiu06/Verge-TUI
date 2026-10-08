package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestConfiguredProxyPreservesOriginalBackupAndRestoresAfterFailure(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.Read()
	ctx := context.Background()
	mock := &testutil.Desktop{Original: "[/]\nmode='none'\n"}
	g := GNOME{Store: s, Run: mock.Run}
	p := state.DefaultPreferences()
	p.SystemProxy.Host = "localhost"
	p.SystemProxy.UseDefaultBypass = false
	p.SystemProxy.Bypass = "*.example.com, 10.0.0.0/8"
	if err := state.WritePreferences(s.Root, p); err != nil {
		t.Fatal(err)
	}
	if err := g.Enable(ctx, c); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"host='localhost'", "ignore-hosts=['*.example.com', '10.0.0.0/8']", "authentication-password=" + variant(c.ProxyPassword)} {
		if !strings.Contains(mock.Payload, text) {
			t.Fatal("configured system proxy is ineffective", text)
		}
	}
	next := p.SystemProxy
	next.Host = "::1"
	if err := g.Reconfigure(ctx, c, p.SystemProxy, next); err != nil {
		t.Fatal(err)
	}
	p.SystemProxy = next
	if err := state.WritePreferences(s.Root, p); err != nil {
		t.Fatal(err)
	}
	if !g.Read(ctx, c).Owned || !strings.HasPrefix(g.Read(ctx, c).Address, "[::1]:") {
		t.Fatal("IPv6 proxy cannot be observed or restored")
	}
	managed := mock.Payload
	mock.FailNextLoad = true
	next.Host = "127.0.0.1"
	if g.Reconfigure(ctx, c, p.SystemProxy, next) == nil || mock.Payload != managed || !g.Read(ctx, c).Owned {
		t.Fatal("failed update did not restore the previous enabled proxy")
	}
	backup, err := os.ReadFile(filepath.Join(s.Root, "gnome-proxy-backup.ini"))
	if err != nil || string(backup) != mock.Original {
		t.Fatal("reconfiguration overwrote the pre-enable backup", err)
	}
	if err := g.Disable(ctx, c); err != nil || mock.Payload != mock.Original {
		t.Fatal("disable did not restore the original desktop settings", err)
	}
}
