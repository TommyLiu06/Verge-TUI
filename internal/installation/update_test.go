package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

type updateFixture struct {
	prefix, bundle, unit, rc string
	store                    *state.Store
	calls                    []string
	active                   string
	failedStarts             int
	failedReadiness          int
}

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func content(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func newUpdate(t *testing.T, mode, active string, unmarked bool) *updateFixture {
	t.Helper()
	root := t.TempDir()
	f := &updateFixture{prefix: filepath.Join(root, "Proxy's $literal `name`"), bundle: filepath.Join(root, "bundle"), rc: filepath.Join(root, "shellrc"), active: active}
	f.unit = filepath.Join(root, "systemd", "user", "verge-tui.service")
	if mode == "system" {
		f.unit = filepath.Join(root, "systemd", "system", "verge-tui.service")
	}
	var err error
	f.store, err = state.Open(filepath.Join(f.prefix, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(func(c *state.Settings) error {
		c.InstallMode, c.Core, c.Current, c.Tun, c.Mode = mode, "custom-core", "subscription", mode == "system", "global"
		c.Profiles = []state.Profile{{ID: "subscription", Name: "Selected subscription", Type: "local"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(f.prefix, "verge-tui"), "old executable")
	put(t, filepath.Join(f.prefix, "verge-tui.service"), "original service configuration\n")
	put(t, f.unit, "original service configuration\n")
	put(t, filepath.Join(f.store.Root, "cores/custom-core/mihomo"), "selected core bytes")
	put(t, filepath.Join(f.store.Root, "profiles/subscription.yaml"), "proxies: []\n")
	put(t, filepath.Join(f.store.Root, "gnome-proxy-backup.ini"), "desktop backup must remain untouched")
	for _, name := range []string{"README.md", "LICENSE", "NOTICE.md", "DEPENDENCIES.txt", "uninstall.sh", "docs/offline.md", "licenses/core-LICENSE", "img/home.png"} {
		put(t, filepath.Join(f.prefix, name), "old "+name)
		put(t, filepath.Join(f.bundle, name), "new "+name)
	}
	put(t, filepath.Join(f.bundle, "bin/verge-tui"), "new executable")
	testutil.ShellScripts(t, f.prefix)
	testutil.ShellScripts(t, f.bundle)
	put(t, filepath.Join(f.prefix, "shell/proxy.sh"), "# previous installed shell implementation\n"+content(t, filepath.Join(f.prefix, "shell/proxy.sh")))
	put(t, f.rc, "# User configuration\n")
	if err := shellenv.Write(f.store); err != nil {
		t.Fatal(err)
	}
	if _, err := shellenv.Register(f.store, filepath.Join(f.prefix, "verge-tui"), f.rc); err != nil {
		t.Fatal(err)
	}
	if !unmarked {
		put(t, filepath.Join(f.prefix, ".verge-tui-install"), fmt.Sprintf("verge-tui-install-v1\n%s\n%s\n%s\n", mode, f.unit, f.prefix))
	}
	return f
}
func (f *updateFixture) run(_ context.Context, mode string, args ...string) (string, error) {
	f.calls = append(f.calls, mode+" "+strings.Join(args, " "))
	switch args[0] {
	case "show-environment":
		return "", nil
	case "show":
		return f.active, nil
	case "stop":
		f.active = "inactive"
	case "start":
		if f.failedStarts > 0 {
			f.failedStarts--
			return "", errors.New("simulated service startup failure")
		}
		f.active = "active"
	default:
		return "", fmt.Errorf("unexpected service mutation: %s", args[0])
	}
	return "", nil
}
func (f *updateFixture) update(rc string) error {
	return update(context.Background(), f.bundle, f.prefix, rc, f.unit, 0, f.run, func(context.Context, *state.Store) error {
		if f.failedReadiness > 0 {
			f.failedReadiness--
			return errors.New("simulated core readiness failure")
		}
		return nil
	}, stageTestData)
}

func stageTestData(_ context.Context, _, stage, _ string) ([]string, error) {
	path := filepath.Join(stage, "data", "runtime", "Country.mmdb")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte("bundled geography bytes"), 0600); err != nil {
		return nil, err
	}
	return []string{"data/runtime/Country.mmdb"}, nil
}

func TestUpdatePreservesDataCoreAndServiceState(t *testing.T) {
	for _, mode := range []string{"user", "system"} {
		for _, active := range []string{"active", "inactive", "failed"} {
			t.Run(mode+"/"+active, func(t *testing.T) {
				f := newUpdate(t, mode, active, false)
				settings := content(t, filepath.Join(f.store.Root, "settings.json"))
				rc := content(t, f.rc)
				if err := f.update(f.rc); err != nil {
					t.Fatal(err)
				}
				if content(t, filepath.Join(f.prefix, "verge-tui")) != "new executable" {
					t.Fatal("executable was not replaced")
				}
				if content(t, filepath.Join(f.prefix, "docs/offline.md")) != "new docs/offline.md" {
					t.Fatal("documentation was not updated")
				}
				if content(t, filepath.Join(f.prefix, "img/home.png")) != "new img/home.png" {
					t.Fatal("README screenshots were not updated")
				}
				if content(t, filepath.Join(f.prefix, "shell/proxy.sh")) != content(t, filepath.Join(f.bundle, "shell/proxy.sh")) {
					t.Fatal("update did not install the bundled shell scripts")
				}
				for _, name := range []string{"shell", "shell/fish", "shell/fish/functions", "shell/proxy.sh", "shell/fish/init.fish"} {
					info, err := os.Stat(filepath.Join(f.prefix, name))
					if err != nil || info.Mode().Perm()&0044 != 0044 || (info.IsDir() && info.Mode().Perm()&0011 != 0011) {
						t.Fatal("updated shell assets are inaccessible to ordinary users", name, err)
					}
				}
				if content(t, filepath.Join(f.store.Root, "settings.json")) != settings {
					t.Fatal("update changed credentials, ports, TUN, core or subscriptions")
				}
				if content(t, filepath.Join(f.store.Root, "cores/custom-core/mihomo")) != "selected core bytes" {
					t.Fatal("selected core was replaced")
				}
				if content(t, filepath.Join(f.store.Root, "profiles/subscription.yaml")) != "proxies: []\n" {
					t.Fatal("subscription was replaced")
				}
				if content(t, filepath.Join(f.store.Root, "gnome-proxy-backup.ini")) != "desktop backup must remain untouched" {
					t.Fatal("update restored or removed GNOME backup")
				}
				if content(t, f.unit) != "original service configuration\n" || content(t, f.rc) != rc {
					t.Fatal("unit or rc changed unexpectedly")
				}
				calls := strings.Join(f.calls, "\n")
				if strings.Contains(calls, "enable") || strings.Contains(calls, "disable") {
					t.Fatal("service enablement changed")
				}
				if active == "active" {
					if !strings.Contains(calls, "stop verge-tui.service") || !strings.Contains(calls, "start verge-tui.service") || f.active != "active" {
						t.Fatal("running service was not restored")
					}
				} else if strings.Contains(calls, "start") || strings.Contains(calls, "stop") {
					t.Fatal("stopped service was started")
				}
				info, _ := os.Stat(filepath.Join(f.prefix, "verge-tui"))
				want := os.FileMode(0700)
				if mode == "system" {
					want = 0755
				}
				if info.Mode().Perm() != want {
					t.Fatal("incorrect executable permissions")
				}
				// Repeated updates must preserve the same registration and data.
				if err := f.update(f.rc); err != nil {
					t.Fatal(err)
				}
				if content(t, f.rc) != rc {
					t.Fatal("update duplicated shell functions")
				}
			})
		}
	}
}

func retainData(t *testing.T, f *updateFixture) {
	t.Helper()
	if err := shellenv.Remove(f.store); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(f.prefix, "verge-tui"), filepath.Join(f.prefix, "verge-tui.service"), f.unit} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	c, err := f.store.Read()
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(f.prefix, ".verge-tui-install"), fmt.Sprintf("verge-tui-install-v1\n%s\n\n%s\n", c.InstallMode, f.prefix))
}

func TestRestoreRetainedInstallationPreservesData(t *testing.T) {
	for _, mode := range []string{"user", "system"} {
		t.Run(mode, func(t *testing.T) {
			f := newUpdate(t, mode, "inactive", false)
			retainData(t, f)
			settings := content(t, filepath.Join(f.store.Root, "settings.json"))
			x, err := inspect(f.prefix, f.unit, 0)
			if err != nil || !x.Retained || x.Mode != mode || x.Unit != "" || x.Core != "custom-core" {
				t.Fatalf("retained inspection: %+v, %v", x, err)
			}
			if mode == "system" {
				if _, err := inspect(f.prefix, f.unit, 1002); err == nil || !strings.Contains(err.Error(), "requires root") {
					t.Fatalf("system reinstallation allowed without root: %v", err)
				}
			}
			if err := f.update(f.rc); err != nil {
				t.Fatal(err)
			}
			if content(t, filepath.Join(f.prefix, "verge-tui")) != "new executable" || len(f.calls) != 0 {
				t.Fatal("application was not restored or removed service was managed")
			}
			if content(t, filepath.Join(f.store.Root, "settings.json")) != settings || content(t, filepath.Join(f.store.Root, "cores/custom-core/mihomo")) != "selected core bytes" || content(t, filepath.Join(f.store.Root, "profiles/subscription.yaml")) != "proxies: []\n" {
				t.Fatal("reinstallation changed settings, selected core or subscription")
			}
			if strings.Count(content(t, f.rc), "# >>> verge-tui") != 1 {
				t.Fatal("shell functions were not restored exactly once")
			}
		})
	}
}

func TestRetainedInspectionRejectsIncompleteOrUnmarkedInstallation(t *testing.T) {
	for _, kind := range []string{"executable-present", "service-present", "service-recorded", "unmarked", "symlink-executable"} {
		t.Run(kind, func(t *testing.T) {
			f := newUpdate(t, "user", "inactive", false)
			retainData(t, f)
			switch kind {
			case "executable-present":
				put(t, filepath.Join(f.prefix, "verge-tui"), "unexpected executable")
			case "service-present":
				put(t, filepath.Join(f.prefix, "verge-tui.service"), "unexpected service")
			case "service-recorded":
				put(t, filepath.Join(f.prefix, ".verge-tui-install"), fmt.Sprintf("verge-tui-install-v1\nuser\n%s\n%s\n", f.unit, f.prefix))
			case "unmarked":
				if err := os.Remove(filepath.Join(f.prefix, ".verge-tui-install")); err != nil {
					t.Fatal(err)
				}
			case "symlink-executable":
				if err := os.Symlink(filepath.Join(f.bundle, "bin/verge-tui"), filepath.Join(f.prefix, "verge-tui")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := inspect(f.prefix, f.unit, 0); err == nil {
				t.Fatal("damaged or unmarked installation was accepted as retained data")
			}
		})
	}
}

func TestRestoreRetainedInstallationRollsBackShellFailure(t *testing.T) {
	f := newUpdate(t, "system", "inactive", false)
	var start string
	for _, line := range strings.Split(content(t, f.rc), "\n") {
		if strings.HasPrefix(line, "# >>> verge-tui ") {
			start = line
		}
	}
	retainData(t, f)
	put(t, f.rc, start+"\n")
	rc, settings := content(t, f.rc), content(t, filepath.Join(f.store.Root, "settings.json"))
	if err := f.update(f.rc); err == nil || !strings.Contains(err.Error(), "original installation restored") {
		t.Fatalf("restoration rollback: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.prefix, "verge-tui")); !os.IsNotExist(err) {
		t.Fatalf("failed restoration left an executable: %v", err)
	}
	if content(t, f.rc) != rc || content(t, filepath.Join(f.store.Root, "settings.json")) != settings {
		t.Fatal("failed restoration changed rc or retained settings")
	}
	x, err := inspect(f.prefix, f.unit, 0)
	if err != nil || !x.Retained {
		t.Fatalf("rollback did not restore retained state: %+v, %v", x, err)
	}
}

func TestUnmarkedInstallationIsRejectedWithoutWrites(t *testing.T) {
	for _, mode := range []string{"user", "system"} {
		t.Run(mode, func(t *testing.T) {
			f := newUpdate(t, mode, "active", true)
			settings := content(t, filepath.Join(f.store.Root, "settings.json"))
			_, err := inspect(f.prefix, f.unit, 0)
			if err == nil || !strings.Contains(err.Error(), "installation marker") {
				t.Fatalf("unmarked inspection: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.prefix, ".verge-tui-install")); !os.IsNotExist(err) {
				t.Fatal("inspection wrote a marker")
			}
			if _, err := os.Stat(filepath.Join(f.store.Root, "runtime/core.lock")); !os.IsNotExist(err) {
				t.Fatal("inspection created a core lock")
			}
			if content(t, filepath.Join(f.store.Root, "settings.json")) != settings {
				t.Fatal("inspection rewrote settings")
			}
			if err := f.update(f.rc); err == nil || !strings.Contains(err.Error(), "installation marker") {
				t.Fatalf("unmarked update: %v", err)
			}
			if len(f.calls) != 0 {
				t.Fatal("unmarked installation changed the service")
			}
		})
	}
}

func TestUpdateRollsBackStartupFailure(t *testing.T) {
	f := newUpdate(t, "system", "active", false)
	put(t, filepath.Join(f.store.Root, "runtime", "Country.mmdb"), "old geography bytes")
	f.failedStarts = 1
	settings, rc := content(t, filepath.Join(f.store.Root, "settings.json")), content(t, f.rc)
	script := content(t, filepath.Join(f.prefix, "shell/proxy.sh"))
	err := f.update(f.rc)
	if err == nil || !strings.Contains(err.Error(), "original installation restored") {
		t.Fatalf("missing rollback result: %v", err)
	}
	if f.active != "active" || content(t, filepath.Join(f.prefix, "verge-tui")) != "old executable" {
		t.Fatal("old executable or service was not restored")
	}
	if content(t, filepath.Join(f.prefix, "docs/offline.md")) != "old docs/offline.md" {
		t.Fatal("old documentation was not restored")
	}
	if content(t, filepath.Join(f.prefix, "shell/proxy.sh")) != script {
		t.Fatal("failed update did not restore the previous shell implementation")
	}
	if content(t, filepath.Join(f.store.Root, "settings.json")) != settings || content(t, f.rc) != rc {
		t.Fatal("rollback changed proxy data or shell rc")
	}
	if content(t, filepath.Join(f.store.Root, "runtime", "Country.mmdb")) != "old geography bytes" {
		t.Fatal("failed update did not restore its previous geography database")
	}
}

func TestUpdateRollsBackCoreReadinessFailure(t *testing.T) {
	f := newUpdate(t, "system", "active", false)
	f.failedReadiness = 1
	if err := f.update(f.rc); err == nil || !strings.Contains(err.Error(), "original installation restored") {
		t.Fatalf("readiness rollback: %v", err)
	}
	if f.active != "active" || content(t, filepath.Join(f.prefix, "verge-tui")) != "old executable" {
		t.Fatal("unhealthy core did not roll back to the old application")
	}
}

func TestIncompleteRollbackRetainsPrivateRecoveryFiles(t *testing.T) {
	f := newUpdate(t, "system", "active", false)
	f.failedStarts = 2
	err := f.update(f.rc)
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("rollback failure: %v", err)
	}
	stages, err := filepath.Glob(filepath.Join(f.prefix, ".update-*"))
	if err != nil || len(stages) != 1 {
		t.Fatalf("missing recovery directory: %v %v", stages, err)
	}
	if !strings.Contains(content(t, filepath.Join(stages[0], "recovery.json")), f.rc) {
		t.Fatal("recovery manifest is missing rc path")
	}
	info, err := os.Stat(stages[0])
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("recovery permissions: %v %v", info, err)
	}
}

func TestUpdateRollsBackMalformedShellRC(t *testing.T) {
	f := newUpdate(t, "user", "active", false)
	marker := content(t, filepath.Join(f.prefix, ".verge-tui-install"))
	lines := strings.Split(content(t, f.rc), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# <<< verge-tui") {
			lines[i] = "# Incomplete marker"
		}
	}
	put(t, f.rc, strings.Join(lines, "\n"))
	rc := content(t, f.rc)
	err := f.update(f.rc)
	if err == nil || !strings.Contains(err.Error(), "incomplete shell integration") {
		t.Fatalf("expected shell marker failure: %v", err)
	}
	if f.active != "active" || content(t, filepath.Join(f.prefix, "verge-tui")) != "old executable" || content(t, f.rc) != rc {
		t.Fatal("failed rc update was not rolled back")
	}
	if content(t, filepath.Join(f.prefix, ".verge-tui-install")) != marker {
		t.Fatal("failed update changed its marker")
	}
}

func TestUpdateRefusesChangedServiceAndForegroundCore(t *testing.T) {
	f := newUpdate(t, "system", "active", false)
	put(t, f.unit, "foreign service")
	if err := f.update(f.rc); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("expected ownership rejection: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("foreign service was touched")
	}
	f = newUpdate(t, "user", "inactive", false)
	lock, err := os.OpenFile(filepath.Join(f.store.Root, "runtime/core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := f.update(f.rc); err == nil || !strings.Contains(err.Error(), "core is still running") {
		t.Fatalf("expected foreground rejection: %v", err)
	}
	if content(t, filepath.Join(f.prefix, "verge-tui")) != "old executable" {
		t.Fatal("running foreground installation was modified")
	}
}

func TestSystemUpdateRequiresRoot(t *testing.T) {
	f := newUpdate(t, "system", "inactive", false)
	if _, err := inspect(f.prefix, f.unit, 1002); err == nil || !strings.Contains(err.Error(), "requires root") {
		t.Fatalf("root check failed: %v", err)
	}
}

func TestUpdateCancellationRestoresStoppedService(t *testing.T) {
	f := newUpdate(t, "system", "active", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := func(ctx context.Context, mode string, args ...string) (string, error) {
		out, err := f.run(ctx, mode, args...)
		if args[0] == "stop" {
			cancel()
		}
		return out, err
	}
	err := update(ctx, f.bundle, f.prefix, f.rc, f.unit, 0, runner, func(context.Context, *state.Store) error { return nil }, stageTestData)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "original installation restored") {
		t.Fatalf("cancelled update: %v", err)
	}
	if f.active != "active" || content(t, filepath.Join(f.prefix, "verge-tui")) != "old executable" {
		t.Fatal("interrupted update did not restore the running installation")
	}
}
