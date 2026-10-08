package core_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

func TestRealCoreLifecycle(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY to an application-managed core")
	}
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := core.Import(ctx, s, "integration", binary); err != nil {
		t.Fatal(err)
	}
	m := &core.Manager{Store: s}
	if err := m.Use(ctx, "integration"); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	c, _ := s.Read()
	client := controller.New(c)
	defer client.Close()
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version == "" || snapshot.Config.MixedPort != c.MixedPort {
		t.Fatal("incorrect live core configuration")
	}
	if err := (&core.Manager{Store: s}).Start(ctx); err == nil {
		t.Fatal("two supervisors acquired the same core")
	}
	response, err := http.Get(client.Base + "/configs")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("API authentication bypass: HTTP %d", response.StatusCode)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// mihomo rejects a delay truncated to zero milliseconds.
		time.Sleep(3 * time.Millisecond)
		w.WriteHeader(204)
	}))
	defer server.Close()
	proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(c.MixedPort))
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	probe := &http.Client{Transport: transport}
	response, err = probe.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 407 {
		t.Fatalf("proxy authentication bypass: HTTP %d", response.StatusCode)
	}
	proxy.User = url.UserPassword(c.ProxyUser, c.ProxyPassword)
	transportAuth := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transportAuth.CloseIdleConnections()
	response, err = (&http.Client{Transport: transportAuth}).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatalf("authenticated proxy failed: HTTP %d", response.StatusCode)
	}
	if _, err := client.Delay(ctx, "DIRECT", server.URL); err != nil {
		t.Fatal(err)
	}
	p, err := profile.Local(s, "Example", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, func(c *state.Settings) error { c.Current = p.ID; c.Mode = "global"; return nil }); err != nil {
		t.Fatal(err)
	}
	snapshot, err = client.Snapshot(ctx)
	if err != nil || snapshot.Config.Mode != "global" {
		t.Fatalf("profile/mode reload failed: %v", err)
	}
	if err := client.Select(ctx, "GLOBAL", "Proxy"); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Import(ctx, s, "second", binary); err != nil {
		t.Fatal(err)
	}
	if err := m.Use(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Read()
	if c.Core != "second" {
		t.Fatal("core selection not persisted")
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	running, err := core.Running(s)
	if err != nil || running {
		t.Fatal("core lock not released")
	}
}
