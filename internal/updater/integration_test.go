package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/installation"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func installedFixture(t *testing.T, suffix ...string) Updater {
	t.Helper()
	prefix := testutil.Dir(t)
	if len(suffix) > 0 {
		prefix = filepath.Join(prefix, suffix[0])
	}
	s, err := state.Open(filepath.Join(prefix, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct{ name, data string }{
		{"verge-tui", "#!/bin/sh\nprintf '0.1.18\\n'\n"},
		{"verge-tui.service", "# fixture: no registered service\n"},
		{".verge-tui-install", "verge-tui-install-v1\nuser\n\n" + prefix + "\n"},
	} {
		if err := os.WriteFile(filepath.Join(prefix, file.name), []byte(file.data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installation.Record(prefix, "0.1.18", ""); err != nil {
		t.Fatal(err)
	}
	return Updater{Store: s, Version: "0.1.18"}
}

func TestAutomaticApplyRechecksActivityAndPolicy(t *testing.T) {
	u := installedFixture(t)
	options := Options{CheckMinutes: 60, IdleMinutes: 5}
	if err := u.Configure(options); err != nil {
		t.Fatal(err)
	}
	options, _ = u.Options()
	connections := controller.Connections{UploadTotal: 10, DownloadTotal: 20}
	testutil.CoreServer(t, u.Store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			t.Error("unexpected request", r.URL.Path)
		}
		json.NewEncoder(w).Encode(connections)
	}))
	s := Status{Options: options, IdleSince: time.Now().Add(-6 * time.Minute), IdleUpload: 10, IdleDownload: 20}
	if err := u.AllowAutomatic(context.Background(), s); err != nil {
		t.Fatal("idle installation was refused", err)
	}
	connections.Connections = []controller.Connection{{ID: "started-after-scheduling"}}
	if err := u.AllowAutomatic(context.Background(), s); err == nil {
		t.Fatal("new active connection did not postpone applying")
	}
	connections.Connections = nil
	connections.DownloadTotal++
	if err := u.AllowAutomatic(context.Background(), s); err == nil {
		t.Fatal("short-lived traffic before applying was ignored")
	}
	connections.DownloadTotal--
	if err := u.Configure(Options{CheckMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	if err := u.AllowAutomatic(context.Background(), s); err == nil {
		t.Fatal("disabling automatic updates did not cancel a pending apply")
	}
	if err := u.Configure(options); err != nil {
		t.Fatal(err)
	}
	if err := u.AllowAutomatic(context.Background(), s); err == nil {
		t.Fatal("quickly disabling and re-enabling reused the old idle window")
	}
}

func TestSchedulerSurvivesServiceStartingBeforeInstallationRegistration(t *testing.T) {
	u := installedFixture(t)
	prefix := filepath.Dir(u.Store.Root)
	if err := os.Remove(filepath.Join(prefix, installation.SummaryFile)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- u.Run(ctx, func(context.Context, *Request) error { return nil }) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("scheduler exited before final installation registration: %v", err)
		default:
		}
		_, err := os.Stat(filepath.Join(u.directory(), "scheduler.lock"))
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler never started leadership election")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := installation.Record(prefix, "0.1.18", ""); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not shut down cleanly")
	}
}

func TestServiceUpdaterUsesIndependentTransientUnit(t *testing.T) {
	u := installedFixture(t, "Proxy's ${APP} 100% path")
	bin := testutil.Dir(t)
	record := filepath.Join(bin, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$UPDATE_TEST_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(bin, "systemd-run"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("UPDATE_TEST_ARGS", record)
	if err := u.Launch(context.Background(), &Request{Version: "1.2.3", Automatic: true}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(filepath.Dir(u.Store.Root), "verge-tui")
	for _, expected := range []string{"--user\n", "--service-type=exec\n", "--collect\n", executable + "\n", "--data-dir\n" + strings.ReplaceAll(u.Store.Root, "$", "$$") + "\n", "apply\n--version\n1.2.3\n--automatic\n"} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("updater would share the proxy service sandbox or cgroup: %s", args)
		}
	}
}

func TestApplyRealOfflinePackagePreservesPrivateData(t *testing.T) {
	archive := os.Getenv("VERGE_TUI_TEST_UPDATE_ARCHIVE")
	if archive == "" {
		t.Skip("set VERGE_TUI_TEST_UPDATE_ARCHIVE to a built offline tar.gz")
	}
	u := installedFixture(t)
	if err := u.Configure(Options{}); err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(archive), "verge-tui-"), "-linux-amd64.tar.gz")
	r := testRelease()
	r.Version, r.Asset = version, filepath.Base(archive)
	r.URL = "https://github.com/" + repository + "/releases/tag/v" + version
	r.Download = "https://github.com/" + repository + "/releases/download/v" + version + "/" + r.Asset
	r.Checksum = r.Download + ".sha256"
	var err error
	r.SHA256, err = hashFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	r.Size = info.Size()
	if err := u.Store.Update(func(c *state.Settings) error {
		c.Core, c.Current = "my-core", "my-profile"
		c.Profiles = []state.Profile{{ID: "my-profile", Name: "Private profile", Type: "local"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"cores/my-core/mihomo": "selected core bytes", "profiles/my-profile.yaml": "proxies: []\nrules: []\n", "preferences.json": "{\"theme\":\"mist-dark\"}\n"} {
		file := filepath.Join(u.Store.Root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(filepath.Join(u.Store.Root, "settings.json"))
	input, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(filepath.Join(u.directory(), "prepared.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		t.Fatal(err)
	}
	if err := u.save(Status{Release: &r, Phase: "ready", Ready: true}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := u.Apply(context.Background(), version, false, &log); err != nil {
		t.Fatalf("update failed: %v\n%s", err, log.String())
	}
	after, _ := os.ReadFile(filepath.Join(u.Store.Root, "settings.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("update overwrote private settings or credentials")
	}
	data, err := os.ReadFile(filepath.Join(u.Store.Root, "cores/my-core/mihomo"))
	if err != nil || string(data) != "selected core bytes" {
		t.Fatal("update replaced the independently selected mihomo core")
	}
	data, err = exec.Command(filepath.Join(filepath.Dir(u.Store.Root), "verge-tui"), "version").Output()
	if err != nil || strings.TrimSpace(string(data)) != version {
		t.Fatalf("installed executable did not update: %s %v", data, err)
	}
	summary, err := installation.Discover(filepath.Dir(u.Store.Root))
	if err != nil || summary.Version != version {
		t.Fatal("public installation version was not updated")
	}
	for name, expected := range map[string]string{"preferences.json": "{\"theme\":\"mist-dark\"}\n", "profiles/my-profile.yaml": "proxies: []\nrules: []\n"} {
		data, err := os.ReadFile(filepath.Join(u.Store.Root, name))
		if err != nil || string(data) != expected {
			t.Fatalf("update changed %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(u.directory(), "prepared.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("successfully applied archive was not cleaned up")
	}
}

func TestPendingAutomaticUpdateDoesNotResetItsIdleWindowWithChecks(t *testing.T) {
	r := testRelease()
	s := Status{CurrentVersion: "0.1.18", Release: &r, Options: Options{CheckMinutes: 1, IdleMinutes: 30}, Ready: true, Phase: "ready"}
	if checkDue(s, time.Now()) {
		t.Fatal("metadata traffic would prevent a longer automatic idle window from ever completing")
	}
	s.Options.IdleMinutes = 0
	if !checkDue(s, time.Now()) {
		t.Fatal("manual installation unexpectedly disabled periodic update checks")
	}
}

func TestChecksumDisagreementStopsBeforeDownloadingExecutable(t *testing.T) {
	r := testRelease()
	client := &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != r.Checksum {
			t.Fatal("downloaded an executable after a checksum mismatch")
		}
		return response([]byte(fmt.Sprintf("%064d  %s\n", 0, r.Asset))), nil
	})}
	if err := download(context.Background(), client, r, filepath.Join(t.TempDir(), "archive"), func(int64) error { return nil }); err == nil {
		t.Fatal("mismatched published checksums were accepted")
	}
}

func TestCancellingApplyAllowsInstallerToRestoreOriginalState(t *testing.T) {
	u := installedFixture(t)
	if err := u.Configure(Options{}); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(u.directory(), "installer-started")
	restored := filepath.Join(u.directory(), "rollback-completed")
	t.Setenv("UPDATE_TEST_STARTED", started)
	t.Setenv("UPDATE_TEST_RESTORED", restored)
	script := `#!/bin/sh
if [ "$1" = version ]; then printf '1.2.3\n'; exit 0; fi
trap 'printf restored > "$UPDATE_TEST_RESTORED"; exit 7' INT TERM
printf started > "$UPDATE_TEST_STARTED"
while :; do sleep 0.05; done
`
	data, release := testBundle(t, nil, "", false, script)
	if err := os.WriteFile(filepath.Join(u.directory(), "prepared.tar.gz"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := u.save(Status{Release: &release, Ready: true, Phase: "ready"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- u.Apply(ctx, release.Version, false, io.Discard) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("installer did not reach the cancellable phase")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled installer reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("installer did not react to cancellation")
	}
	if data, err := os.ReadFile(restored); err != nil || string(data) != "restored" {
		t.Fatal("installer was force-killed before its rollback handler could run")
	}
	summary, err := installation.Discover(filepath.Dir(u.Store.Root))
	if err != nil || summary.Version != "0.1.18" {
		t.Fatal("cancelled installation changed the installed version")
	}
}
