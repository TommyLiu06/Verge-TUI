package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/state"
)

func TestExternalControllerLifecycleWithRealCore(t *testing.T) {
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
	if err := b.Manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	c, _ := b.Store.Read()
	if c.ControllerEnabled {
		t.Fatal("external controller enabled by default")
	}
	address := "127.0.0.1:" + strconv.Itoa(c.APIPort)
	assertClosed := func() {
		t.Helper()
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			conn.Close()
			t.Fatal("disabled TCP controller still listening")
		}
	}
	assertClosed()
	client := controller.New(c)
	defer client.Close()
	if _, err := client.Snapshot(ctx); err != nil {
		t.Fatal("internal IPC failed with external controller off", err)
	}
	for _, secret := range []string{"first-secret", "changed-secret"} {
		if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "controller", Fields: map[string]string{"enabled": "true", "address": address, "secret": secret}}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			response, err := http.Get("http://" + address + "/version")
			if err == nil {
				response.Body.Close()
				if response.StatusCode != 401 {
					t.Fatal("external API missing authentication")
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
		external := controller.New(state.Settings{APIPort: c.APIPort, Secret: secret})
		if err := external.Request(ctx, "GET", "/version", nil, nil); err != nil {
			t.Fatal("configured secret rejected", err)
		}
		external.Close()
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "controller", Fields: map[string]string{"enabled": "false"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	assertClosed()
	if err := b.Execute(ctx, Action{Kind: "mode", Value: "direct"}); err != nil {
		t.Fatal("core control lost after disabling controller", err)
	}
	if _, err := client.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
}
