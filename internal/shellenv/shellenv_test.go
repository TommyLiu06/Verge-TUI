package shellenv

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"verge-tui/internal/state"
)

func store(t *testing.T) *state.Store {
	t.Helper()
	s, err := state.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestEnvironmentPermissionsAndRefresh(t *testing.T) {
	s := store(t)
	c, _ := s.Read()
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(FilePath(s))
	if info.Mode().Perm() != 0600 {
		t.Fatal("private proxy environment is exposed")
	}
	loaded, err := ReadFile(FilePath(s))
	if err != nil {
		t.Fatal(err)
	}
	if ProxyURL(loaded) != ProxyURL(c) || loaded.APIPort != c.APIPort {
		t.Fatal("proxy environment does not match settings")
	}
	if err := s.Update(func(c *state.Settings) error {
		c.MixedPort++
		if c.MixedPort == c.APIPort {
			c.MixedPort++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(s); err != nil {
		t.Fatal(err)
	}
	loaded, _ = ReadFile(FilePath(s))
	c, _ = s.Read()
	if loaded.MixedPort != c.MixedPort {
		t.Fatal("proxy environment retained stale ports")
	}
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(FilePath(s))
	data, _ := os.ReadFile(FilePath(s))
	if info.Mode().Perm() != 0644 || strings.Contains(string(data), c.Secret) || strings.Contains(string(data), c.ProxyPassword) {
		t.Fatal("shared environment contains private credentials")
	}
}
func TestRegistrationPreservesAndRemovesOnlyOwnedBlock(t *testing.T) {
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(filepath.Dir(s.Root), "actual-rc")
	alias := filepath.Join(filepath.Dir(s.Root), "rc-link")
	original := "# My settings\nexport EDITOR=vi\n"
	if err := os.WriteFile(rc, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rc, alias); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := Register(s, "/fake/My ' proxy/$bin", alias); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(rc)
	if strings.Count(string(data), "# >>> verge-tui") != 1 || !strings.HasPrefix(string(data), original) {
		t.Fatal("registration duplicated functions or changed unrelated rc content")
	}
	if info, _ := os.Lstat(alias); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("registration replaced a user's rc symlink")
	}
	if info, _ := os.Stat(rc); info.Mode().Perm() != 0640 {
		t.Fatal("registration changed rc permissions")
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(rc)
	if string(data) != original {
		t.Fatal("uninstall did not restore unrelated rc text")
	}
	if err := Remove(s); err != nil {
		t.Fatal("repeated shell removal is not idempotent")
	}
}
func TestStatusSeparatesCoreListenerAndEnvironment(t *testing.T) {
	s := store(t)
	c, _ := s.Read()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+c.Secret {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"test-core"}`))
		case "/configs":
			w.Write([]byte(`{"mode":"rule","tun":{"enable":false}}`))
		default:
			t.Error("status requested monitoring data")
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	u, _ := url.Parse(api.URL)
	port, _ := strconv.Atoi(u.Port())
	proxy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if err := s.Update(func(c *state.Settings) error {
		c.APIPort = port
		c.MixedPort = proxy.Addr().(*net.TCPAddr).Port
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Read()
	var env []string
	for k, v := range Expected(c) {
		env = append(env, k+"="+v)
	}
	out, err := Status(context.Background(), s, env)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Core: running (test-core)", "Proxy listener: listening", "loaded and matches", "TUN: false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing status %q: %s", want, out)
		}
	}
	if strings.Contains(out, c.Secret) || strings.Contains(out, c.ProxyPassword) {
		t.Fatal("status leaked private credentials")
	}
	out, err = Status(context.Background(), s, []string{"http_proxy=http://different:secret@example.com:7890"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "partial or different") || strings.Contains(out, "different:secret") {
		t.Fatal("foreign environment was misreported or leaked credentials")
	}
	out, err = Status(context.Background(), s, nil)
	if err != nil || !strings.Contains(out, "Shell environment: not loaded") {
		t.Fatal("unset environment was not reported separately")
	}
}

func TestSharedStatusDoesNotNeedPrivateAPICredentials(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary file permission checks")
	}
	s := store(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer api.Close()
	u, _ := url.Parse(api.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; c.APIPort = port; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(s.Root, "settings.json")
	if err := os.Chmod(settings, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(settings, 0600)
	out, err := Status(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "administrator authentication required") || !strings.Contains(out, "Shell environment: not loaded") {
		t.Fatal("shared status could not use the public environment metadata")
	}
}
