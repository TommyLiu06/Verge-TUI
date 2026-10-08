package packaging_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"mihomo-tui/internal/core"
	"mihomo-tui/internal/shellenv"
	"mihomo-tui/internal/state"
)

type fixture struct {
	bundle, root, prefix, unit, log, rc string
	env                                 []string
}

func setup(t *testing.T, manager, linger string) fixture {
	t.Helper()
	bundle := os.Getenv("MIHOMO_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("set MIHOMO_TEST_BUNDLE to an extracted offline bundle")
	}
	root := t.TempDir()
	f := fixture{bundle: bundle, root: root, prefix: filepath.Join(root, "My Proxy"), unit: filepath.Join(root, "config", "systemd", "user", "mihomo-tui.service"), log: filepath.Join(root, "service.log")}
	f.rc = filepath.Join(root, "shellrc")
	write(t, f.rc, "# Existing user configuration\n", 0644)
	bin := filepath.Join(root, "tools")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "systemctl"), `#!/bin/sh
printf '%s\n' "$*" >> "$MIHOMO_SERVICE_LOG"
[ "${1:-}" != --user ] || shift
start_test_service() {
    [ "${MIHOMO_MANAGED_TEST_SERVICE:-no}" = yes ] || return 0
    "$MIHOMO_SERVICE_PREFIX/mihomo-tui" run > "$MIHOMO_SERVICE_PREFIX/test-service.log" 2>&1 &
    echo "$!" > "$MIHOMO_SERVICE_PREFIX/test-supervisor.pid"
    attempts=0
    while [ ! -f "$MIHOMO_SERVICE_PREFIX/data/runtime/status.json" ]; do
        attempts=$((attempts + 1))
        [ "$attempts" -lt 100 ] || return 1
        sleep 0.05
    done
}
stop_test_service() {
    [ "${MIHOMO_MANAGED_TEST_SERVICE:-no}" = yes ] || return 0
    [ -f "$MIHOMO_SERVICE_PREFIX/test-supervisor.pid" ] || return 0
    kill "$(cat "$MIHOMO_SERVICE_PREFIX/test-supervisor.pid")" 2>/dev/null || true
    attempts=0
    while [ -f "$MIHOMO_SERVICE_PREFIX/data/runtime/status.json" ]; do
        attempts=$((attempts + 1))
        [ "$attempts" -lt 100 ] || return 1
        sleep 0.05
    done
    rm -f "$MIHOMO_SERVICE_PREFIX/test-supervisor.pid"
}
case "${1:-}" in
    show-environment) [ "$MIHOMO_MANAGER_AVAILABLE" = yes ] ;;
    show) printf '%s\n' "${MIHOMO_ACTIVE_STATE:-active}" ;;
    enable|start) start_test_service ;;
    disable|stop) stop_test_service ;;
    *) exit 0 ;;
esac
`, 0700)
	write(t, filepath.Join(bin, "loginctl"), "#!/bin/sh\nprintf '%s\\n' \"$MIHOMO_LINGER\"\n", 0700)
	// All service files stay inside the project test directory. Real host
	// systemd and GNOME tools are never invoked by these integration tests.
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "PATH", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CURRENT_DESKTOP", "DBUS_SESSION_BUS_ADDRESS", "MIHOMO_SERVICE_LOG", "MIHOMO_MANAGER_AVAILABLE", "MIHOMO_LINGER":
			continue
		}
		f.env = append(f.env, value)
	}
	f.env = append(f.env, "PATH="+bin+":"+os.Getenv("PATH"), "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_DATA_HOME="+filepath.Join(root, "data-home"), "XDG_CURRENT_DESKTOP=", "DBUS_SESSION_BUS_ADDRESS=", "MIHOMO_SERVICE_LOG="+f.log, "MIHOMO_MANAGER_AVAILABLE="+manager, "MIHOMO_LINGER="+linger)
	return f
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func (f fixture) run(t *testing.T, script, input string, succeeds bool, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", append([]string{filepath.Join(f.bundle, script), "--prefix", f.prefix}, args...)...)
	if script == "uninstall.sh" {
		cmd.Args = append(cmd.Args, "--yes")
	}
	if script == "install.sh" {
		specified := false
		for _, arg := range args {
			if arg == "--shell" {
				specified = true
			}
		}
		if !specified {
			cmd.Args = append(cmd.Args, "--shell", "bash", "--rc-file", f.rc)
		}
	}
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(input)
	b, err := cmd.CombinedOutput()
	if (err == nil) != succeeds {
		t.Fatalf("%s: %v\n%s", script, err, b)
	}
	return string(b)
}
func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path still exists: %s (%v)", path, err)
	}
}

func TestConfirmationBeforeInstallation(t *testing.T) {
	for _, input := range []string{"n\n", ""} {
		t.Run(strings.TrimSpace(input), func(t *testing.T) {
			f := setup(t, "yes", "no")
			out := f.run(t, "install.sh", input, true)
			for _, want := range []string{"Mode:         user", "User systemd: yes", "lingering disabled", "Proceed with installation?", "Installation cancelled."} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing installation information %q: %s", want, out)
				}
			}
			absent(t, f.prefix)
			if read(t, f.rc) != "# Existing user configuration\n" {
				t.Fatal("cancelled installation modified the shell rc")
			}
			if strings.Contains(read(t, f.log), "enable") || strings.Contains(out, "Verifying offline files") {
				t.Fatal("installation started before confirmation")
			}
		})
	}
}

func TestAutomaticUserServiceAndDataPreservingUninstall(t *testing.T) {
	f := setup(t, "yes", "yes")
	out := f.run(t, "install.sh", "yes\n", true)
	if !strings.Contains(out, "lingering enabled") || !strings.Contains(read(t, f.log), "--user enable --now mihomo-tui.service") || strings.Contains(out, ": OK") {
		t.Fatal("service detection or concise installation output failed")
	}
	settings := read(t, filepath.Join(f.prefix, "data", "settings.json"))
	unit := read(t, f.unit)
	write(t, f.unit, unit+"\n# Replaced externally\n", 0600)
	out = f.run(t, "uninstall.sh", "", false)
	if !strings.Contains(out, "differs from this installation") || strings.Contains(read(t, f.log), "disable") {
		t.Fatal("uninstaller changed an externally modified service")
	}
	write(t, f.unit, unit, 0600)
	f.run(t, "uninstall.sh", "", true)
	if read(t, f.rc) != "# Existing user configuration\n" {
		t.Fatal("uninstall did not preserve unrelated shell configuration")
	}
	absent(t, filepath.Join(f.prefix, "mihomo-tui"))
	absent(t, f.unit)
	if read(t, filepath.Join(f.prefix, "data", "settings.json")) != settings {
		t.Fatal("data-preserving uninstall changed subscription settings or credentials")
	}
	if _, err := os.Stat(filepath.Join(f.prefix, "data", "cores", "v1.19.32", "mihomo")); err != nil {
		t.Fatal("data-preserving uninstall removed its managed core")
	}
	if !strings.Contains(read(t, f.log), "--user disable --now mihomo-tui.service") {
		t.Fatal("service was not stopped and disabled")
	}
	// A later installation's service must survive a purge of retained data.
	write(t, f.unit, "another installation's unit", 0600)
	f.run(t, "uninstall.sh", "", true, "--purge")
	absent(t, f.prefix)
	if read(t, f.unit) != "another installation's unit" {
		t.Fatal("purging retained data affected a new service")
	}
}

func TestUnavailableManagerAndExplicitServiceChoice(t *testing.T) {
	f := setup(t, "no", "no")
	out := f.run(t, "install.sh", "", false, "--service", "--yes")
	if !strings.Contains(out, "Requested service manager is unavailable") {
		t.Fatal(out)
	}
	absent(t, f.prefix)
	out = f.run(t, "install.sh", "", true, "--yes")
	if !strings.Contains(out, "User systemd: no") || !strings.Contains(out, "no service") {
		t.Fatal(out)
	}
	absent(t, f.unit)
	f.run(t, "uninstall.sh", "", false, "--mode", "system", "--purge")
	if _, err := os.Stat(filepath.Join(f.prefix, "mihomo-tui")); err != nil {
		t.Fatal("mode mismatch removed the application")
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
	absent(t, f.prefix)
}

func TestNoServiceOverride(t *testing.T) {
	f := setup(t, "yes", "yes")
	f.run(t, "install.sh", "", true, "--yes", "--no-service")
	absent(t, f.unit)
	if strings.Contains(read(t, f.log), "enable") {
		t.Fatal("--no-service still enabled a service")
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
}

func TestUninstallRefusesUnmarkedDirectory(t *testing.T) {
	f := setup(t, "no", "no")
	if err := os.Mkdir(f.prefix, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.prefix, "unrelated.txt")
	write(t, file, "keep", 0600)
	f.run(t, "uninstall.sh", "", false, "--purge")
	f.run(t, "uninstall.sh", "", false, "--purge", "--legacy")
	if read(t, file) != "keep" {
		t.Fatal("unmarked directory was modified")
	}
}

func TestUnmarkedInstallationCannotBeUpdatedOrUninstalled(t *testing.T) {
	f := setup(t, "no", "no")
	f.run(t, "install.sh", "", true, "--yes")
	marker := read(t, filepath.Join(f.prefix, ".mihomo-tui-install"))
	if err := os.Remove(filepath.Join(f.prefix, ".mihomo-tui-install")); err != nil {
		t.Fatal(err)
	}
	f.run(t, "uninstall.sh", "", false, "--purge")
	out := f.run(t, "uninstall.sh", "", false, "--purge", "--legacy")
	if !strings.Contains(out, "Unknown option: --legacy") {
		t.Fatal(out)
	}
	out = f.run(t, "install.sh", "", false, "--yes")
	if !strings.Contains(out, "installation marker") {
		t.Fatal(out)
	}
	write(t, filepath.Join(f.prefix, ".mihomo-tui-install"), marker, 0600)
	f.run(t, "uninstall.sh", "", true, "--purge")
	absent(t, f.prefix)
}

func TestUninstallPreservesInaccessibleDesktopBackup(t *testing.T) {
	f := setup(t, "no", "no")
	f.run(t, "install.sh", "", true, "--yes")
	backup := filepath.Join(f.prefix, "data", "gnome-proxy-backup.ini")
	write(t, backup, "[/]\nmode='none'\n", 0600)
	f.run(t, "uninstall.sh", "", false, "--purge")
	if read(t, backup) != "[/]\nmode='none'\n" {
		t.Fatal("desktop backup was discarded")
	}
	if _, err := os.Stat(filepath.Join(f.prefix, "mihomo-tui")); err != nil {
		t.Fatal("application was removed before proxy restoration")
	}
}

func TestUninstallRequiresForegroundCoreToStop(t *testing.T) {
	f := setup(t, "no", "no")
	f.run(t, "install.sh", "", true, "--yes")
	s, err := state.Open(filepath.Join(f.prefix, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(f.prefix, "mihomo-tui"), "run")
	cmd.Env = f.env
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if !core.ReadStatus(s).Started.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("foreground core did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	statusCmd := exec.Command("bash", "--noprofile", "--norc", "-c", `. "$1"; mihomo_on; mihomo_status`, "proxy-status-test", f.rc)
	statusCmd.Env = f.env
	statusOutput, err := statusCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(statusOutput), "Core: running") || !strings.Contains(string(statusOutput), "loaded and matches") {
		t.Fatalf("shell status with a running core: %v\n%s", err, statusOutput)
	}
	out := f.run(t, "uninstall.sh", "", false, "--purge")
	if !strings.Contains(out, "core is still running") {
		t.Fatal(out)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	stopped = true
	if err != nil {
		t.Fatalf("foreground shutdown: %v\n%s", err, logs.String())
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
	absent(t, f.prefix)
}

func TestProxyFunctionsInBashAndZsh(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			f := setup(t, "no", "no")
			f.prefix = filepath.Join(f.root, "Proxy's $literal `name`")
			f.run(t, "install.sh", "", true, "--yes", "--shell", shell, "--rc-file", f.rc)
			args := []string{"--noprofile", "--norc", "-c"}
			if shell == "zsh" {
				args = []string{"-f", "-c"}
			}
			script := `. "$1"
mihomo_off
mihomo_status
mihomo_on
[ -n "${http_proxy:-}" ] && [ "$http_proxy" = "$https_proxy" ] && [ "$http_proxy" = "$ALL_PROXY" ] || exit 21
[ "$no_proxy" = "$NO_PROXY" ] || exit 22
mihomo_status
mihomo_off
[ -z "${http_proxy+x}${HTTPS_PROXY+x}${ALL_PROXY+x}${NO_PROXY+x}" ] || exit 23
mihomo_status`
			cmd := exec.Command(shell, append(args, script, "proxy-functions-test", f.rc)...)
			cmd.Env = f.env
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s shell functions: %v\n%s", shell, err, out)
			}
			if strings.Count(string(out), "Shell environment: not loaded") != 2 || !strings.Contains(string(out), "loaded and matches") {
				t.Fatal("shell functions did not change the caller's exported variables")
			}
			s := &state.Store{Root: filepath.Join(f.prefix, "data")}
			c, err := s.Read()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), c.ProxyPassword) || strings.Contains(read(t, f.rc), c.ProxyPassword) {
				t.Fatal("shell status or rc leaked credentials")
			}
			info, _ := os.Stat(filepath.Join(f.prefix, "proxy.env"))
			if info.Mode().Perm() != 0600 {
				t.Fatal("private env file permissions changed")
			}
			newPort, err := state.FreePort(c.APIPort)
			if err != nil {
				t.Fatal(err)
			}
			configure := exec.Command(filepath.Join(f.prefix, "mihomo-tui"), "configure", "--mixed-port", strconv.Itoa(newPort))
			configure.Env = f.env
			if out, err := configure.CombinedOutput(); err != nil {
				t.Fatalf("configure: %v\n%s", err, out)
			}
			env, err := shellenv.ReadFile(filepath.Join(f.prefix, "proxy.env"))
			if err != nil || env.MixedPort != newPort {
				t.Fatal("configure did not update the prepared env file")
			}
			f.run(t, "uninstall.sh", "", true, "--purge")
			if read(t, f.rc) != "# Existing user configuration\n" {
				t.Fatal("uninstall left shell functions in the rc")
			}
		})
	}
}

func TestShellIntegrationCanBeDisabled(t *testing.T) {
	f := setup(t, "no", "no")
	f.run(t, "install.sh", "", true, "--yes", "--shell", "none")
	if read(t, f.rc) != "# Existing user configuration\n" {
		t.Fatal("--shell none modified an rc file")
	}
	if _, err := os.Stat(filepath.Join(f.prefix, "proxy.env")); err != nil {
		t.Fatal("--shell none did not prepare the proxy env")
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
}

func TestInstallerUpdatesExistingInstallationAfterConfirmation(t *testing.T) {
	f := setup(t, "yes", "yes")
	f.env = append(f.env, "MIHOMO_MANAGED_TEST_SERVICE=yes", "MIHOMO_SERVICE_PREFIX="+f.prefix)
	t.Cleanup(func() {
		cmd := exec.Command(filepath.Join(f.root, "tools/systemctl"), "--user", "stop", "mihomo-tui.service")
		cmd.Env = f.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("test supervisor cleanup: %v\n%s", err, out)
		}
	})
	f.run(t, "install.sh", "", true, "--yes")
	settingsPath := filepath.Join(f.prefix, "data/settings.json")
	settings, rc, unit := read(t, settingsPath), read(t, f.rc), read(t, f.unit)
	// Changes to owned documentation make actual replacement observable even
	// when the test bundle and installed app have the same version string.
	write(t, filepath.Join(f.prefix, "README.md"), "old documentation", 0600)
	write(t, f.log, "", 0600)
	for _, input := range []string{"n\n", ""} {
		out := f.run(t, "install.sh", input, true)
		if !strings.Contains(out, "mihomo-tui update") || !strings.Contains(out, "Update cancelled.") {
			t.Fatal(out)
		}
		if read(t, settingsPath) != settings || read(t, f.rc) != rc || read(t, filepath.Join(f.prefix, "README.md")) != "old documentation" {
			t.Fatal("cancelled update modified installation")
		}
		if strings.Contains(read(t, f.log), "stop") || strings.Contains(read(t, f.log), "start") {
			t.Fatal("cancelled update touched the service")
		}
	}
	out := f.run(t, "install.sh", "yes\n", true)
	if !strings.Contains(out, "Updated in") || read(t, filepath.Join(f.prefix, "README.md")) != read(t, filepath.Join(f.bundle, "README.md")) {
		t.Fatal(out)
	}
	if read(t, settingsPath) != settings || read(t, f.rc) != rc || read(t, f.unit) != unit {
		t.Fatal("update changed proxy data, shell integration or service configuration")
	}
	if !strings.Contains(read(t, f.log), "--user stop mihomo-tui.service") || !strings.Contains(read(t, f.log), "--user start mihomo-tui.service") {
		t.Fatal("update did not restart the running service")
	}
	if strings.Contains(read(t, f.log), "enable") || strings.Contains(read(t, f.log), "disable") {
		t.Fatal("update changed enablement")
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
}

func TestInstallerUpdatesForegroundInstallationAndAddsShellFunctions(t *testing.T) {
	f := setup(t, "no", "no")
	f.run(t, "install.sh", "", true, "--yes", "--shell", "none")
	settings := read(t, filepath.Join(f.prefix, "data/settings.json"))
	f.run(t, "install.sh", "", true, "--yes")
	if read(t, filepath.Join(f.prefix, "data/settings.json")) != settings {
		t.Fatal("foreground update reinitialized settings")
	}
	if !strings.Contains(read(t, f.rc), "mihomo_on()") {
		t.Fatal("foreground update did not register new shell functions")
	}
	if strings.Contains(read(t, f.log), "enable") {
		t.Fatal("foreground update installed a service")
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
}

func TestUpdateRejectsInstallationModeAndServiceChanges(t *testing.T) {
	f := setup(t, "yes", "yes")
	f.run(t, "install.sh", "", true, "--yes", "--no-service")
	settings := read(t, filepath.Join(f.prefix, "data/settings.json"))
	out := f.run(t, "install.sh", "", false, "--yes", "--mode", "system")
	if !strings.Contains(out, "System mode requires root") && !strings.Contains(out, "does not match") {
		t.Fatal(out)
	}
	out = f.run(t, "install.sh", "", false, "--yes", "--service")
	if !strings.Contains(out, "preserves the existing service") {
		t.Fatal(out)
	}
	if read(t, filepath.Join(f.prefix, "data/settings.json")) != settings {
		t.Fatal("rejected update changed data")
	}
	f.run(t, "uninstall.sh", "", true, "--purge")
}

func (f fixture) uninstallFromBundle(t *testing.T, input string, succeeds bool, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", append([]string{filepath.Join(f.bundle, "uninstall.sh")}, args...)...)
	cmd.Env, cmd.Stdin = f.env, strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if (err == nil) != succeeds {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	return string(out)
}

func TestUninstallFromBundleDetectsUserPrefixAndConfirmsFirst(t *testing.T) {
	f := setup(t, "yes", "yes")
	f.prefix = filepath.Join(f.root, "data-home", "mihomo-tui")
	f.run(t, "install.sh", "", true, "--yes")
	settings := read(t, filepath.Join(f.prefix, "data/settings.json"))
	rc := read(t, f.rc)
	for _, input := range []string{"n\n", ""} {
		out := f.uninstallFromBundle(t, input, true, "--mode", "user")
		if !strings.Contains(out, "Mode:         user") || !strings.Contains(out, "Uninstallation cancelled.") {
			t.Fatal(out)
		}
		if read(t, filepath.Join(f.prefix, "data/settings.json")) != settings || read(t, f.rc) != rc {
			t.Fatal("cancelled uninstall modified data or rc")
		}
		if strings.Contains(read(t, f.log), "disable") || strings.Contains(read(t, f.log), "stop") {
			t.Fatal("cancelled uninstall stopped the service")
		}
	}
	f.uninstallFromBundle(t, "yes\n", true, "--mode", "user")
	absent(t, filepath.Join(f.prefix, "mihomo-tui"))
	if read(t, filepath.Join(f.prefix, "data/settings.json")) != settings {
		t.Fatal("default uninstall changed retained data")
	}
	if _, err := os.Stat(filepath.Join(f.prefix, "data/runtime/Country.mmdb")); err != nil {
		t.Fatal("default uninstall removed geography database")
	}
	f.uninstallFromBundle(t, "yes\n", true, "--mode", "user", "--purge")
	absent(t, f.prefix)
}

func TestInstalledUninstallerUsesCustomDirectory(t *testing.T) {
	f := setup(t, "no", "no")
	f.run(t, "install.sh", "", true, "--yes")
	f.bundle = f.prefix
	f.uninstallFromBundle(t, "", true, "--mode", "user", "--purge", "--yes")
	absent(t, f.prefix)
}

func TestSystemModeUninstallWithMockRootAndCustomPrefix(t *testing.T) {
	f := setup(t, "yes", "yes")
	f.run(t, "install.sh", "", true, "--yes", "--no-service")
	// Model a stopped system installation under the test directory. The root
	// identity check is mocked; no /opt, /etc or host systemd files are touched.
	s := &state.Store{Root: filepath.Join(f.prefix, "data")}
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.prefix, ".mihomo-tui-install"), "mihomo-tui-install-v1\nsystem\n\n"+f.prefix+"\n", 0600)
	write(t, filepath.Join(f.root, "tools", "id"), "#!/bin/sh\nprintf '%s\\n' 0\n", 0700)
	out := f.uninstallFromBundle(t, "", true, "--mode", "system", "--prefix", f.prefix, "--purge", "--yes")
	if !strings.Contains(out, "Mode:         system") {
		t.Fatal(out)
	}
	absent(t, f.prefix)
}

func TestRequestedPortConflictStopsBeforeInstallation(t *testing.T) {
	f := setup(t, "no", "no")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	original := read(t, f.rc)
	out := f.run(t, "install.sh", "yes\n", false, "--mixed-port", strconv.Itoa(port))
	if !strings.Contains(out, "Requested ports are unavailable") {
		t.Fatal(out)
	}
	absent(t, f.prefix)
	if read(t, f.rc) != original {
		t.Fatal("port preflight changed the rc")
	}
}
