package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOfficialCatalogTransportWithRealManagedCore(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY to test the real authenticated proxy")
	}
	s := coreStore(t)
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
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("private proxy credentials leaked to the destination")
		}
		json.NewEncoder(w).Encode([]Release{compatibleRelease("v2")})
	}))
	defer origin.Close()
	req, _ := http.NewRequest("GET", origin.URL, nil)
	proxy, err := client.Transport.(*http.Transport).Proxy(req)
	if err != nil || proxy == nil || proxy.User == nil {
		t.Fatal("real running core was not selected as the authenticated proxy", err)
	}
	page, err := readReleases(ctx, client, origin.URL, 1)
	if err != nil || len(page.Releases) != 1 || page.Releases[0].Tag != "v2" {
		t.Fatal("catalog request failed through real mihomo", err)
	}
}
