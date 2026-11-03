package updater

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"verge-tui/internal/controller"
)

func TestPolicyDependenciesAndCascadingDisable(t *testing.T) {
	options := Options{}
	for _, fields := range []map[string]string{{"auto-download": "true"}, {"idle-minutes": "5"}} {
		if _, err := options.WithChanges(fields); err == nil {
			t.Fatal("enabled a child policy without its prerequisite", fields)
		}
	}
	var err error
	options, err = options.WithChanges(map[string]string{"check-minutes": "60"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := options.WithChanges(map[string]string{"idle-minutes": "5"}); err == nil {
		t.Fatal("automatic checks alone enabled automatic installation")
	}
	options, err = options.WithChanges(map[string]string{"auto-download": "true", "idle-minutes": "5"})
	if err != nil || !options.UpdatesEnabled() {
		t.Fatal("valid policy chain was rejected", err)
	}
	downloadOff, err := options.WithChanges(map[string]string{"auto-download": "false"})
	if err != nil || downloadOff.CheckMinutes != 60 || downloadOff.AutoDownload || downloadOff.IdleMinutes != 0 {
		t.Fatal("disabling downloads did not also disable installation", downloadOff, err)
	}
	checksOff, err := options.WithChanges(map[string]string{"check-minutes": "0"})
	if err != nil || checksOff.AutoDownload || checksOff.IdleMinutes != 0 {
		t.Fatal("disabling checks left a child enabled", checksOff, err)
	}
	if _, err := checksOff.WithChanges(map[string]string{"idle-minutes": "-1"}); err == nil {
		t.Fatal("cascade cleanup hid an invalid explicit interval")
	}
}

type releaseServer struct {
	mu                  sync.Mutex
	latest              Release
	archives            map[string][]byte
	metadata, downloads int
}

func (s *releaseServer) client(context.Context) (*http.Client, error) {
	return &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		r := s.latest
		if req.URL.String() == latestAPI {
			s.metadata++
			data, _ := json.Marshal(map[string]any{"tag_name": "v" + r.Version, "html_url": r.URL, "assets": []any{
				map[string]any{"name": r.Asset, "browser_download_url": r.Download, "size": r.Size, "digest": "sha256:" + r.SHA256},
				map[string]any{"name": r.Asset + ".sha256", "browser_download_url": r.Checksum},
			}})
			return response(data), nil
		}
		if req.URL.String() == r.Checksum {
			return response([]byte(r.SHA256 + "  " + r.Asset + "\n")), nil
		}
		if req.URL.String() == r.Download {
			s.downloads++
			return response(s.archives[r.Version]), nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(http.NoBody)}, nil
	})}, nil
}

func TestSupersededArchiveIsDeletedAndManualUpdateRechecksLatest(t *testing.T) {
	oldBytes, old := testBundle(t, nil, "", false)
	newBytes, latest := testBundleVersion(t, testVersion("1.2.4"), nil, "", false)
	server := &releaseServer{latest: old, archives: map[string][]byte{old.Version: oldBytes, latest.Version: newBytes}}
	u := installedFixture(t)
	u.clientFactory = server.client
	status, err := u.PrepareLatest(context.Background())
	if err != nil || !status.Ready || status.Release.Version != old.Version {
		t.Fatal("could not prepare initial update", status, err)
	}
	server.latest = latest
	status, err = u.Check(context.Background())
	if err != nil || status.Ready || status.Release.Version != latest.Version {
		t.Fatal("new metadata retained stale ready status", status, err)
	}
	if _, err := os.Stat(filepath.Join(u.directory(), "prepared.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("superseded archive was not deleted")
	}
	// Return to an old prepared package, then publish the new release without
	// another explicit check. The manual Update flow must refresh on its own.
	server.latest = old
	if _, err := u.PrepareLatest(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.latest = latest
	status, err = u.PrepareLatest(context.Background())
	if err != nil || !status.Ready || status.Release.Version != latest.Version {
		t.Fatal("manual Update would install an outdated cached release", status, err)
	}
	if digest, err := hashFile(filepath.Join(u.directory(), "prepared.tar.gz")); err != nil || digest != latest.SHA256 {
		t.Fatal("latest metadata did not select the latest archive")
	}
}

func TestAutomaticDownloadDoesNotRequireAutomaticInstallation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "check only", true: "download without install"}[enabled], func(t *testing.T) {
			data, release := testBundle(t, nil, "", false)
			server := &releaseServer{latest: release, archives: map[string][]byte{release.Version: data}}
			u := installedFixture(t)
			u.clientFactory = server.client
			if err := u.Configure(Options{CheckMinutes: 60, AutoDownload: enabled}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- u.Run(ctx, func(context.Context, *Request) error {
					t.Error("download policy installed an update")
					return ErrDeferred
				})
			}()
			defer func() {
				cancel()
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				status, err := u.Status()
				if err != nil {
					t.Fatal(err)
				}
				if status.Release != nil && (!enabled || status.Ready) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("automatic check/download did not complete", status)
				}
				time.Sleep(10 * time.Millisecond)
			}
			server.mu.Lock()
			count := server.downloads
			server.mu.Unlock()
			if (count > 0) != enabled {
				t.Fatal("download did not follow its own switch", count)
			}
		})
	}
}

func TestAutomaticCheckReplacesAnAlreadyDownloadedOlderVersion(t *testing.T) {
	oldBytes, old := testBundle(t, nil, "", false)
	newBytes, latest := testBundleVersion(t, testVersion("1.2.4"), nil, "", false)
	server := &releaseServer{latest: old, archives: map[string][]byte{old.Version: oldBytes, latest.Version: newBytes}}
	u := installedFixture(t)
	u.clientFactory = server.client
	if err := u.Configure(Options{CheckMinutes: 1, AutoDownload: true}); err != nil {
		t.Fatal(err)
	}
	status, err := u.PrepareLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status.LastAttempt = time.Now().Add(-2 * time.Minute)
	if err := u.save(status); err != nil {
		t.Fatal(err)
	}
	server.latest = latest
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- u.Run(ctx, func(context.Context, *Request) error { t.Error("Manual installation was ignored"); return ErrDeferred })
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err = u.Status()
		if err != nil {
			t.Fatal(err)
		}
		if status.Ready && status.Release.Version == latest.Version {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic checking did not replace the outdated cached archive", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if digest, err := hashFile(filepath.Join(u.directory(), "prepared.tar.gz")); err != nil || digest != latest.SHA256 {
		t.Fatal("automatic replacement left the old archive")
	}
}

func TestUpdateChecksDoNotResetIdleForTheirOwnTraffic(t *testing.T) {
	now := time.Now()
	var window idleWindow
	window.observe(now, controller.Connections{DownloadTotal: 10}, nil)
	window.afterCheck(now.Add(time.Second), controller.Connections{DownloadTotal: 1000}, nil)
	if since := window.observe(now.Add(2*time.Second), controller.Connections{DownloadTotal: 1000}, nil); !since.Equal(now) {
		t.Fatal("metadata traffic restarted the idle period")
	}
	window.afterCheck(now.Add(3*time.Second), controller.Connections{Connections: []controller.Connection{{ID: "other user"}}, DownloadTotal: 2000}, nil)
	if !window.since.IsZero() {
		t.Fatal("another user's active connection was ignored")
	}
}

func TestDisablingAutomaticDownloadsCancelsAnActiveTransfer(t *testing.T) {
	data, release := testBundle(t, nil, "", false)
	server := &releaseServer{latest: release, archives: map[string][]byte{release.Version: data}}
	u := installedFixture(t)
	started := make(chan struct{})
	u.clientFactory = func(ctx context.Context) (*http.Client, error) {
		client, _ := server.client(ctx)
		base := client.Transport
		client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == release.Download {
				close(started)
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			return base.RoundTrip(req)
		})
		return client, nil
	}
	if err := u.Configure(Options{CheckMinutes: 60, AutoDownload: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := u.prepareAutomatic(ctx); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("automatic download never started")
	}
	if err := u.ModifyOptions(func(options *Options) error {
		next, err := options.WithChanges(map[string]string{"check-minutes": "0"})
		*options = next
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("disabling a policy was treated as a download failure", err)
		}
	case <-ctx.Done():
		t.Fatal("disabling checks did not cancel the child transfer")
	}
	status, err := u.Status()
	if err != nil || status.Ready || status.Error != "" || status.Options.DownloadsEnabled() {
		t.Fatal("cancelled automatic download left incorrect state", status, err)
	}
	if _, err := os.Stat(filepath.Join(u.directory(), "prepared.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("cancelled transfer became an installable archive")
	}
}
