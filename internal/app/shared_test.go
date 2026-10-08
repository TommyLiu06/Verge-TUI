package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/management"
	"verge-tui/internal/profile"
	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
)

func shortenStore(t *testing.T, s *state.Store) {
	t.Helper()
	prefix, err := os.MkdirTemp("../../.local", "share-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(prefix) })
	prefix, err = filepath.Abs(prefix)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(prefix, "data")
	if err := os.Rename(s.Root, path); err != nil {
		t.Fatal(err)
	}
	s.Root = path
}

func TestSharedDelayDoesNotLockConfigurationChanges(t *testing.T) {
	started := make(chan struct{}, 1)
	b := delayBackend(t, []string{"Node"}, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/delay") {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	shortenStore(t, b.Store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.RWMutex
	server, err := management.Serve(ctx, b.Store, b.SharedHandlers(&mu))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); server.Close() }()
	remote := NewRemote(b.Store, "test")
	defer remote.Remote.Close()
	delayCtx, stopDelay := context.WithCancel(ctx)
	defer stopDelay()
	finished := make(chan error, 1)
	go func() { finished <- remote.TestDelay(delayCtx, Action{Kind: "delay", ID: "Proxy", Node: "Node"}, nil) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("delay did not start")
	}
	changeCtx, stopChange := context.WithTimeout(ctx, 2*time.Second)
	defer stopChange()
	if err := remote.Execute(changeCtx, Action{Kind: "mode", Value: "direct"}); err != nil {
		t.Fatal("delay blocked configuration update", err)
	}
	c, err := b.Store.Read()
	if err != nil || c.Mode != "direct" {
		t.Fatal("mode change did not take effect before test finished", err)
	}
	stopDelay()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("delay did not cancel")
	}
}

func TestSharedTUIManagesProfilesNodesAndDelayWithoutToken(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	b := delayBackend(t, []string{"Node"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Path == "/proxies/Proxy" {
			w.WriteHeader(200)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"delay": 123})
	})
	shortenStore(t, b.Store)
	if err := b.Store.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := shellenv.Write(b.Store); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.RWMutex
	server, err := management.Serve(ctx, b.Store, b.SharedHandlers(&mu))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); server.Close() }()
	client := NewRemote(b.Store, "test")
	defer client.Close(ctx)
	view, err := client.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Online || view.Settings.Secret != "" || view.Settings.ProxyPassword != "" || view.Owned {
		t.Fatal("shared view exposes private credentials or service ownership")
	}
	for _, action := range []Action{{Kind: "profile-new", Profile: state.Profile{Type: "local", Name: "Shared profile"}}, {Kind: "node-select", ID: "Proxy", Node: "Node"}, {Kind: "tun"}} {
		if err := client.Execute(ctx, action); err != nil {
			t.Fatal(action.Kind, err)
		}
	}
	c, err := b.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 1 || !c.Tun {
		t.Fatal("ordinary client could not manage profile/TUN settings")
	}
	if err := client.Execute(ctx, Action{Kind: "profile-select", ID: c.Profiles[0].ID}); err != nil {
		t.Fatal(err)
	}
	var updates []DelayResult
	if err := client.TestDelay(ctx, Action{Kind: "delay", ID: "Proxy", Node: "Node"}, func(r DelayResult) { updates = append(updates, r) }); err != nil || len(updates) != 1 || updates[0].Delay != 123 {
		t.Fatal("remote delay result missing", updates, err)
	}
	if err := client.Execute(ctx, Action{Kind: "profile-delete", ID: c.Profiles[0].ID}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"start", "stop", "prepare-uninstall"} {
		// Verify the root-side handler too, bypassing the client whitelist.
		if err := client.Remote.Call(ctx, "POST", "/action", Action{Kind: kind}, nil); err == nil || !strings.Contains(err.Error(), "requires root") {
			t.Fatal("service management exposed to ordinary clients", kind, err)
		}
	}
	if err := client.Remote.Call(ctx, "POST", "/action", Action{Kind: "profile-new", File: "/private/admin.yaml"}, nil); err == nil {
		t.Fatal("service accepted a caller-supplied privileged file path")
	}
	file := filepath.Join(filepath.Dir(b.Store.Root), "user.yaml")
	if err := os.WriteFile(file, []byte(profile.DefaultConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if err := client.Execute(ctx, Action{Kind: "profile-new", File: file, Profile: state.Profile{Type: "local", Name: "Uploaded YAML"}}); err != nil {
		t.Fatal("client did not upload its own file", err)
	}
	enabled := false
	client.Desktop.Run = func(_ context.Context, name string, args []string, input string) (string, error) {
		if name == "dconf" {
			switch args[0] {
			case "dump":
				return "[/]\nmode='none'", nil
			case "load":
				enabled = strings.Contains(input, "mode='manual'")
			case "reset":
				enabled = false
			}
			return "", nil
		}
		switch args[2] {
		case "mode":
			if enabled {
				return "'manual'", nil
			}
			return "'none'", nil
		case "host":
			return "'127.0.0.1'", nil
		case "port":
			return fmt.Sprint(c.MixedPort), nil
		}
		return "", nil
	}
	if err := client.Execute(ctx, Action{Kind: "system-proxy"}); err != nil {
		t.Fatal("ordinary user's GNOME switch failed", err)
	}
	backup := filepath.Join(client.Desktop.Store.Root, "gnome-proxy-backup.ini")
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("per-user GNOME backup missing", err)
	}
	if _, err := os.Stat(filepath.Join(b.Store.Root, "gnome-proxy-backup.ini")); !os.IsNotExist(err) {
		t.Fatal("ordinary switch wrote the administrator's desktop backup")
	}
	view, err = client.Load(ctx)
	if err != nil || !view.Desktop.Owned {
		t.Fatal("shared TUI did not observe this user's desktop settings", err)
	}
	if err := client.Execute(ctx, Action{Kind: "system-proxy"}); err != nil || enabled {
		t.Fatal("ordinary user's GNOME settings were not restored", err)
	}
}

func TestPublicViewPreservesPrivateOriginal(t *testing.T) {
	v := View{Owned: true, Settings: state.Settings{Secret: "api-secret", ProxyPassword: "proxy-password", Profiles: []state.Profile{{URL: "https://example.com/secret-path?token=secret", Home: "https://private.example", UserAgent: "private-agent"}}}, Snapshot: controller.Snapshot{}}
	public := PublicView(v)
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"api-secret", "proxy-password", "secret-path", "token=secret", "private-agent", "private.example"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("public view leaked", secret)
		}
	}
	if v.Settings.Secret != "api-secret" || v.Settings.Profiles[0].URL != "https://example.com/secret-path?token=secret" || !v.Owned {
		t.Fatal("public view mutated administrator data")
	}
}
