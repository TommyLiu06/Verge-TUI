package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"go.yaml.in/yaml/v3"
	"verge-tui/internal/state"
)

func TestImportSubscriptionAndSecurityOverlay(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		w.Header().Set("Subscription-Userinfo", "upload=1024; download=2048; total=4096; expire=1800000000")
		w.Header().Set("Content-Disposition", `attachment; filename="Example.yaml"`)
		w.Write([]byte(DefaultConfig + "authentication: []\nskip-auth-prefixes: [127.0.0.1/8]\nexternal-controller-unix: /tmp/exposed\nlisteners: [{type: mixed, port: 7890}]\ntun: {enable: true}\n"))
	}))
	defer server.Close()
	p, err := Remote(context.Background(), s, state.Profile{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Example.yaml" || p.Usage == nil || p.Usage.Download != 2048 {
		t.Fatal("subscription metadata missing")
	}
	c, _ := s.Read()
	c.Current = p.ID
	c.Tun = true
	data, err := Runtime(s, c, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["external-controller-unix"] != c.ControllerSocket || doc["external-controller"] != "" {
		t.Fatal("subscription exposed a controller")
	}
	if _, ok := doc["listeners"]; ok {
		t.Fatal("unprotected inbound retained")
	}
	if len(doc["skip-auth-prefixes"].([]any)) != 0 || len(doc["authentication"].([]any)) != 1 {
		t.Fatal("user proxy authentication bypass")
	}
	if doc["tun"].(map[string]any)["enable"] != false {
		t.Fatal("TUN enabled in user mode")
	}
	path, _ := s.ProfilePath(p.ID)
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("profile is not private")
	}
	c.InstallMode = "system"
	data, err = Runtime(s, c, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	yaml.Unmarshal(data, &doc)
	if doc["tun"].(map[string]any)["enable"] != true || len(doc["authentication"].([]any)) != 0 {
		t.Fatal("system mode cannot share proxy/TUN")
	}
}

func TestSubscriptionRequestUsesDefaultOrCustomUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name, configured, want string
	}{
		{"default", "", "clash-verge/v2.5.8"},
		{"custom", "CustomClient/1.0", "CustomClient/1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("User-Agent"); got != tc.want {
					t.Errorf("subscription User-Agent = %q, want %q", got, tc.want)
				}
				w.Write([]byte(DefaultConfig))
			}))
			defer server.Close()
			if _, _, err := Download(context.Background(), state.Profile{URL: server.URL, UserAgent: tc.configured}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidImportsDoNotChangeState(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"<html>not YAML</html>", "hello", "proxies: []\n---\nproxies: []", "proxies: ["} {
		if _, err := Add(s, state.Profile{Type: "local"}, []byte(data)); err == nil {
			t.Errorf("accepted malformed profile %q", data)
		}
	}
	c, _ := s.Read()
	if len(c.Profiles) != 0 || c.Current != "" {
		t.Fatal("failed import modified state")
	}
	entries, _ := os.ReadDir(s.Root + "/profiles")
	if len(entries) != 0 {
		t.Fatal("failed import left files")
	}
}
