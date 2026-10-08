package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mihomo-tui/internal/controller"
	"mihomo-tui/internal/core"
	"mihomo-tui/internal/profile"
	"mihomo-tui/internal/state"
)

func TestSubscriptionWorkflowWithRealCore(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := New(s, "test")
	if _, err := core.Import(ctx, s, "test", binary); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Use(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "start"}); err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	if err := b.PrepareUninstall(ctx); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatal("uninstaller accepted a running foreground core")
	}
	data := profile.DefaultConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			time.Sleep(3 * time.Millisecond)
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Subscription-Userinfo", "upload=5; download=10; total=100")
		w.Write([]byte(data))
	}))
	defer server.Close()
	if err := b.Execute(ctx, Action{Kind: "profile-import", Profile: state.Profile{URL: server.URL, Name: "Example"}}); err != nil {
		t.Fatal(err)
	}
	v, err := b.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Settings.Profiles) != 1 || v.Settings.Current != "" {
		t.Fatal("import selected a profile before an explicit action")
	}
	id := v.Settings.Profiles[0].ID
	if err := b.Execute(ctx, Action{Kind: "profile-select", ID: id}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "node-select", ID: "Proxy", Node: "DIRECT"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "delay", ID: "Proxy", Value: server.URL + "/health"}); err != nil {
		t.Fatal(err)
	}
	// A malformed update is rejected before replacing the selected profile.
	data = "not YAML profile"
	if err := b.Execute(ctx, Action{Kind: "profile-update", ID: id}); err == nil {
		t.Fatal("malformed subscription update accepted")
	}
	v, err = b.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Settings.Current != id || v.Snapshot.Proxies["Proxy"].Name != "Proxy" {
		t.Fatal("failed update replaced live profile")
	}
	data = "proxies: []\nproxy-groups:\n - name: New Group\n   type: select\n   proxies: [DIRECT]\nrules:\n - MATCH,New Group\n"
	if err := b.Execute(ctx, Action{Kind: "profile-update", ID: id}); err != nil {
		t.Fatal(err)
	}
	v, err = b.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Snapshot.Proxies["New Group"].Name != "New Group" || len(v.GroupOrder) != 1 || v.GroupOrder[0] != "New Group" {
		t.Fatal("updated subscription was not reloaded")
	}
	if err := b.Execute(ctx, Action{Kind: "tun"}); err == nil {
		t.Fatal("user mode allowed TUN")
	}
	if err := b.Execute(ctx, Action{Kind: "profile-delete", ID: id}); err != nil {
		t.Fatal(err)
	}
	v, err = b.Load(ctx)
	if err != nil || v.Settings.Current != "" || len(v.Settings.Profiles) != 0 || v.Snapshot.Proxies["Proxy"].Now != "DIRECT" {
		t.Fatal("deleting the selected profile did not reload DIRECT")
	}
}

func TestPrepareUninstallRestoresDesktopBackup(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := New(s, "test")
	backup := filepath.Join(s.Root, "gnome-proxy-backup.ini")
	original := "[/]\nmode='none'\n"
	if err := state.Write(backup, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if err := b.PrepareUninstall(context.Background()); err == nil {
		t.Fatal("uninstaller ignored an inaccessible desktop backup")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("uninstaller removed a backup before restoration")
	}
	reset, restored := false, false
	b.Desktop.Run = func(_ context.Context, name string, args []string, input string) (string, error) {
		if name == "gsettings" {
			return "'none'", nil
		}
		if name != "dconf" {
			t.Fatal("unexpected desktop command")
		}
		if args[0] == "reset" {
			reset = true
		}
		if args[0] == "load" {
			restored = reset && input == original
		}
		return "", nil
	}
	if err := b.PrepareUninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Fatal("original desktop proxy settings were not restored")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("restored desktop backup was not removed")
	}
}

func TestHomeGroupUsesLiveModeAndProfileOrder(t *testing.T) {
	v := View{Online: true, Settings: state.Settings{Mode: "global"}, GroupOrder: []string{"Z Proxy", "A Proxy"}, Snapshot: controller.Snapshot{Config: controller.Config{Mode: "rule"}, Proxies: map[string]controller.Proxy{
		"Z Proxy": {Name: "Z Proxy", Type: "Selector", All: []string{"DIRECT"}}, "A Proxy": {Name: "A Proxy", Type: "Selector", All: []string{"DIRECT"}}, "GLOBAL": {Name: "GLOBAL", All: []string{"DIRECT"}},
	}}}
	if HomeGroup(v).Name != "Z Proxy" {
		t.Fatal("home ignored live mode or profile group order")
	}
}
