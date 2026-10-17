package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func coreStore(t *testing.T) *state.Store {
	t.Helper()
	s, err := state.Open(filepath.Join(testutil.Dir(t), "data"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func compatibleRelease(tag string) Release {
	return Release{Tag: tag, Assets: []Asset{{Name: "mihomo-linux-amd64-compatible-" + tag + ".gz", URL: "https://github.com/MetaCubeX/mihomo/releases/download/" + tag + "/mihomo.gz", Digest: "sha256:" + strings.Repeat("a", 64)}}}
}

func TestOfficialReleaseCatalogFiltersAssetsAndPaginates(t *testing.T) {
	good := compatibleRelease("v2")
	draft := compatibleRelease("draft")
	draft.Draft = true
	wrongCPU := compatibleRelease("v3")
	wrongCPU.Assets[0].Name = "mihomo-linux-amd64-v3-v3.gz"
	untrusted := compatibleRelease("v4")
	untrusted.Assets[0].URL = "https://other.example/mihomo.gz"
	unsigned := compatibleRelease("v5")
	unsigned.Assets[0].Digest = ""
	malformed := compatibleRelease("v6")
	malformed.Assets[0].Digest = "sha256:not-a-valid-digest"
	alpha := Release{Tag: "Prerelease-Alpha", Prerelease: true, Assets: []Asset{{Name: "mihomo-linux-amd64-compatible-alpha-1234567.gz", URL: "https://github.com/MetaCubeX/mihomo/releases/download/Prerelease-Alpha/mihomo.gz", Digest: good.Assets[0].Digest}}}
	releases := []Release{good, draft, wrongCPU, untrusted, unsigned, malformed, alpha}
	for len(releases) < releasesPerPage {
		releases = append(releases, draft)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "20" || r.Header.Get("User-Agent") != "verge-tui" {
			t.Error("catalog request omitted pagination or user agent")
		}
		if r.URL.Query().Get("page") == "2" {
			json.NewEncoder(w).Encode([]Release{compatibleRelease("v1")})
			return
		}
		json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()
	page, err := readReleases(context.Background(), server.Client(), server.URL, 1)
	if err != nil || page.NextPage != 2 || len(page.Releases) != 2 || page.Releases[0].Tag != "v2" || page.Releases[1].ID != "alpha-1234567" {
		t.Fatalf("incorrect compatible official releases: %#v, %v", page, err)
	}
	page, err = readReleases(context.Background(), server.Client(), server.URL, 2)
	if err != nil || page.NextPage != 0 || len(page.Releases) != 1 || page.Releases[0].Tag != "v1" {
		t.Fatalf("incorrect final page: %#v, %v", page, err)
	}
}

func TestCatalogAndDownloadUseAuthenticatedManagedProxy(t *testing.T) {
	for _, mode := range []string{"user", "system"} {
		t.Run(mode, func(t *testing.T) { testCatalogAndDownloadUseManagedProxy(t, mode) })
	}
}

func testCatalogAndDownloadUseManagedProxy(t *testing.T, mode string) {
	t.Setenv("NO_PROXY", "*")
	s := coreStore(t)
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = mode; return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Read()
	api := testutil.CoreServer(t, s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+c.Secret {
			t.Error("controller probe was not authenticated")
		}
		io.WriteString(w, `{"version":"test"}`)
	}))
	defer api.Close()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	io.WriteString(gz, "#!/bin/sh\nprintf '%s\\n' 'Mihomo Meta test linux amd64'\n")
	gz.Close()
	release := compatibleRelease("v2")
	digest := sha256.Sum256(compressed.Bytes())
	release.Assets[0].Digest = "sha256:" + hex.EncodeToString(digest[:])
	var metadata, archives atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to the origin")
		}
		if strings.Contains(r.URL.Path, "/releases/download/") {
			archives.Add(1)
			w.Write(compressed.Bytes())
		} else if r.URL.Query().Has("page") {
			metadata.Add(1)
			json.NewEncoder(w).Encode([]Release{release})
		} else {
			metadata.Add(1)
			json.NewEncoder(w).Encode(release)
		}
	}))
	defer origin.Close()
	var mu sync.Mutex
	hosts := map[string]int{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := ""
		if mode == "user" {
			expected = "Basic " + base64.StdEncoding.EncodeToString([]byte(c.ProxyUser+":"+c.ProxyPassword))
		}
		if r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != expected {
			t.Error("managed proxy was not used with its private credentials")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		mu.Lock()
		hosts[r.Host]++
		mu.Unlock()
		fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		done := make(chan struct{})
		go func() { io.Copy(upstream, rw); upstream.(*net.TCPConn).CloseWrite(); close(done) }()
		io.Copy(client, upstream)
		client.Close()
		<-done
	}))
	defer proxy.Close()
	port := proxy.Listener.Addr().(*net.TCPAddr).Port
	if err := s.Update(func(c *state.Settings) error { c.MixedPort = port; return nil }); err != nil {
		t.Fatal(err)
	}
	client, err := downloadClient(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	pool := x509.NewCertPool()
	pool.AddCert(origin.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: origin.Certificate().DNSNames[0]}
	page, err := readReleases(context.Background(), client, releasesAPI, 1)
	if err != nil || len(page.Releases) != 1 {
		t.Fatalf("catalog did not use the managed HTTPS proxy: %v", err)
	}
	installed, err := downloadRelease(context.Background(), s, client, releasesAPI+"/tags/v2")
	if err != nil || installed.ID != "v2" {
		t.Fatalf("verified download did not use the managed HTTPS proxy: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if metadata.Load() != 2 || archives.Load() != 1 || hosts["api.github.com:443"] == 0 || hosts["github.com:443"] == 0 {
		t.Fatal("catalog and archive did not both pass through the proxy", hosts)
	}
}

func TestUnrelatedListenerDoesNotReceiveProxyCredentials(t *testing.T) {
	s := coreStore(t)
	var requests atomic.Int32
	unrelated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("credentials were sent to an unrelated listener")
		}
	}))
	defer unrelated.Close()
	if err := s.Update(func(c *state.Settings) error { c.MixedPort = unrelated.Listener.Addr().(*net.TCPAddr).Port; return nil }); err != nil {
		t.Fatal(err)
	}
	client, err := downloadClient(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "direct") }))
	defer target.Close()
	data, err := fetch(context.Background(), client, target.URL, 1024)
	if err != nil || string(data) != "direct" || requests.Load() != 0 {
		t.Fatal("unrelated listener was treated as the managed proxy", err)
	}
}
