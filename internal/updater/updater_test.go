package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/state"
)

func testRelease() Release {
	r := Release{Version: "1.2.3", Size: 1, SHA256: strings.Repeat("a", 64), URL: "https://github.com/" + repository + "/releases/tag/v1.2.3"}
	r.Asset = "verge-tui-1.2.3-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	r.Download = "https://github.com/" + repository + "/releases/download/v1.2.3/" + r.Asset
	r.Checksum = r.Download + ".sha256"
	return r
}

func testBundle(t *testing.T, extra *tar.Header, corrupt string, unlisted bool) ([]byte, Release) {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"bin/verge-tui", "cores/mihomo", "install.sh", "uninstall.sh", "CORE_VERSION", "README.md", "LICENSE", "NOTICE.md", "DEPENDENCIES.txt", "geodata/manifest.json", "geodata/Country.mmdb", "geodata/GeoIP.dat", "geodata/GeoSite.dat", "shell/proxy.sh", "shell/fish/init.fish", "docs/install.md", "licenses/core-LICENSE", "img/home.png"} {
		files[name] = "fixture " + name + "\n"
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var manifest strings.Builder
	for _, name := range names {
		h := sha256.Sum256([]byte(files[name]))
		fmt.Fprintf(&manifest, "%x  %s\n", h, name)
	}
	files["SHA256SUMS"] = manifest.String()
	names = append(names, "SHA256SUMS")
	if corrupt != "" {
		files[corrupt] = "changed after checksum generation"
	}
	if unlisted {
		files["extra"] = "unverified"
		names = append(names, "extra")
	}
	r := testRelease()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		header := &tar.Header{Name: strings.TrimSuffix(r.Asset, ".tar.gz") + "/" + name, Mode: 0755, Size: int64(len(files[name])), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		io.WriteString(tw, files[name])
	}
	if extra != nil {
		if err := tw.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil && (extra == nil || extra.Size <= 256<<20) {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(buffer.Bytes())
	r.Size, r.SHA256 = int64(buffer.Len()), hex.EncodeToString(h[:])
	return buffer.Bytes(), r
}

func TestVerifiedBundleRejectsTamperingAndUnsafeArchives(t *testing.T) {
	root := strings.TrimSuffix(testRelease().Asset, ".tar.gz")
	cases := []struct {
		name     string
		header   *tar.Header
		corrupt  string
		unlisted bool
		alter    bool
	}{
		{name: "valid"},
		{name: "archive tampering", alter: true},
		{name: "binary tampering", corrupt: "bin/verge-tui"},
		{name: "unlisted executable", unlisted: true},
		{name: "traversal", header: &tar.Header{Name: root + "/../../escape", Typeflag: tar.TypeReg}},
		{name: "absolute", header: &tar.Header{Name: "/escape", Typeflag: tar.TypeReg}},
		{name: "symlink", header: &tar.Header{Name: root + "/link", Linkname: "../../escape", Typeflag: tar.TypeSymlink}},
		{name: "hardlink", header: &tar.Header{Name: root + "/link", Linkname: root + "/bin/verge-tui", Typeflag: tar.TypeLink}},
		{name: "duplicate", header: &tar.Header{Name: root + "/bin/verge-tui", Typeflag: tar.TypeReg}},
		{name: "large member", header: &tar.Header{Name: root + "/huge", Size: 257 << 20, Typeflag: tar.TypeReg}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, release := testBundle(t, tc.header, tc.corrupt, tc.unlisted)
			if tc.alter {
				data[len(data)/2] ^= 1
			}
			stage := t.TempDir()
			archive := filepath.Join(stage, "bundle.tar.gz")
			if err := os.WriteFile(archive, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, err := unpack(context.Background(), archive, stage, release)
			if (err == nil) != (tc.name == "valid") {
				t.Fatalf("unexpected verification result: %v", err)
			}
		})
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(data []byte) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: make(http.Header)}
}

func TestOfficialMetadataAndStreamedDownload(t *testing.T) {
	archive, release := testBundle(t, nil, "", false)
	metadata := map[string]any{"tag_name": "v" + release.Version, "html_url": release.URL, "body": "Release notes", "assets": []any{
		map[string]any{"name": release.Asset, "browser_download_url": release.Download, "size": release.Size, "digest": "sha256:" + release.SHA256},
		map[string]any{"name": release.Asset + ".sha256", "browser_download_url": release.Checksum},
	}}
	calls := 0
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.Header.Get("User-Agent") != "verge-tui" {
			t.Error("requests must use HTTPS and an identifying user agent")
		}
		switch r.URL.String() {
		case latestAPI:
			data, _ := json.Marshal(metadata)
			return response(data), nil
		case release.Checksum:
			return response([]byte(release.SHA256 + "  " + release.Asset + "\n")), nil
		case release.Download:
			return response(archive), nil
		}
		t.Fatalf("unexpected request: %s", r.URL)
		return nil, nil
	})}
	actual, err := readRelease(context.Background(), client, latestAPI)
	if err != nil || actual.SHA256 != release.SHA256 {
		t.Fatalf("could not read release: %#v %v", actual, err)
	}
	var progress int64
	file := filepath.Join(t.TempDir(), "archive")
	if err := download(context.Background(), client, actual, file, func(n int64) error { progress = n; return nil }); err != nil {
		t.Fatal(err)
	}
	if progress != release.Size || calls != 3 {
		t.Fatalf("incorrect progress or request count: %d %d", progress, calls)
	}
	for _, change := range []func(*Release){
		func(r *Release) { r.Download = "http://github.com/" + repository + "/bad" },
		func(r *Release) { r.Checksum = "https://other.example/checksum" },
		func(r *Release) { r.Asset = "another-platform.tar.gz" },
		func(r *Release) { r.SHA256 = "invalid" },
	} {
		r := release
		change(&r)
		if r.validate() == nil {
			t.Error("accepted untrusted release metadata")
		}
	}
	metadata["prerelease"] = true
	if _, err := readRelease(context.Background(), client, latestAPI); err == nil {
		t.Error("stable updater accepted a prerelease")
	}
}

func TestVersionAndIdlePolicies(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.1.19", "0.1.18", true}, {"0.2.0", "0.1.99", true}, {"0.1.9", "0.1.18", false}, {"0.1.18", "0.1.18", false}, {"1.0.0-beta", "0.1.18", false}, {"01.2.3", "0.1.18", false},
	} {
		if Newer(tc.latest, tc.current) != tc.want {
			t.Fatalf("incorrect comparison: %#v", tc)
		}
	}
	now := time.Now()
	if checkDue(Status{}, now) || !checkDue(Status{Options: Options{CheckMinutes: 60}}, now) || checkDue(Status{Options: Options{CheckMinutes: 60}, LastAttempt: now.Add(-time.Minute)}, now) {
		t.Fatal("manual or interval policy was ignored")
	}
	if checkDue(Status{Options: Options{CheckMinutes: 1}, RetryAfter: now.Add(time.Hour)}, now) {
		t.Fatal("automatic requests ignored failure backoff")
	}
	var window idleWindow
	empty := controller.Connections{}
	if window.observe(now, empty, nil).IsZero() || window.observe(now.Add(5*time.Second), empty, nil) != now {
		t.Fatal("continuous idle time was not preserved")
	}
	if !window.observe(now.Add(10*time.Second), controller.Connections{Connections: []controller.Connection{{ID: "active"}}}, nil).IsZero() {
		t.Fatal("an active connection did not reset idle time")
	}
	window.observe(now.Add(15*time.Second), empty, nil)
	if !window.observe(now.Add(20*time.Second), controller.Connections{DownloadTotal: 1}, nil).IsZero() {
		t.Fatal("traffic between samples did not reset idle time")
	}
	window.observe(now.Add(25*time.Second), controller.Connections{DownloadTotal: 1}, nil)
	if !window.observe(now.Add(time.Minute), controller.Connections{DownloadTotal: 1}, nil).IsZero() {
		t.Fatal("a suspended poller retained a stale idle period")
	}
}

func TestSettingsPersistAndOperationLockExcludesOtherProcesses(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	u := Updater{Store: s, Version: "0.1.18"}
	options, err := u.Options()
	if err != nil || options != (Options{}) {
		t.Fatalf("defaults should be manual: %#v %v", options, err)
	}
	if err := u.Configure(Options{CheckMinutes: 60, IdleMinutes: 10}); err != nil {
		t.Fatal(err)
	}
	if err := u.Configure(Options{IdleMinutes: -1}); err == nil {
		t.Fatal("invalid settings were persisted")
	}
	options, err = u.Options()
	if err != nil || options.IdleMinutes != 10 || options.CheckMinutes != 60 {
		t.Fatalf("saved policy was lost: %#v %v", options, err)
	}
	lock, err := u.lock("operation")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if other, err := u.lock("operation"); err == nil {
		other.Close()
		t.Fatal("two updaters acquired the same operation lock")
	}
}
