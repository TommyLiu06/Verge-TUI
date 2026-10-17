package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/state"
)

func TestOfficialCatalogTransportWithRealManagedCore(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY to test the real authenticated proxy")
	}
	s := coreStore(t)
	if err := s.Update(func(c *state.Settings) error { c.Engine.LogLevel = "debug"; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := Import(ctx, s, "proxy-test", binary); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Store: s}
	if err := manager.Use(ctx, "proxy-test"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	client, err := downloadClient(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	// GitHub metadata and assets use HTTPS. Exercise the same CONNECT tunnel
	// through the real core, trusting only the local fixture's certificate.
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("private proxy credentials leaked to the destination")
		}
		json.NewEncoder(w).Encode([]Release{compatibleRelease("v2")})
	}))
	defer origin.Close()
	pool := x509.NewCertPool()
	pool.AddCert(origin.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}
	req, _ := http.NewRequest("GET", origin.URL, nil)
	proxy, err := client.Transport.(*http.Transport).Proxy(req)
	if err != nil || proxy == nil || proxy.User == nil {
		t.Fatal("real running core was not selected as the authenticated proxy", err)
	}
	// The controller and listener become available before mihomo finishes
	// loading providers and runs its startup GC. Until tunnel.OnRunning, it
	// closes forwarded connections. Wait for this fixture's forwarding path
	// before asserting that a catalog request succeeds without retries.
	if err := waitForProxyForwarding(ctx, client, origin.URL); err != nil {
		t.Fatal(err)
	}
	page, err := readReleases(ctx, client, origin.URL, 1)
	if err != nil || len(page.Releases) != 1 || page.Releases[0].Tag != "v2" {
		manager.Stop()
		if log, readErr := os.ReadFile(filepath.Join(s.Root, "runtime", "core.log")); readErr == nil {
			c, _ := s.Read()
			text := strings.ReplaceAll(strings.ReplaceAll(string(log), c.ProxyPassword, "[redacted]"), c.Secret, "[redacted]")
			t.Log(text)
		}
		t.Fatal("catalog request failed through real mihomo", err)
	}
}

func waitForProxyForwarding(ctx context.Context, client *http.Client, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("proxy readiness returned HTTP %d", response.StatusCode)
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("test core forwarding did not become ready: %w (last request: %v)", ctx.Err(), lastErr)
		case <-time.After(25 * time.Millisecond):
		}
	}
}
