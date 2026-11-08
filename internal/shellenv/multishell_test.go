package shellenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func multiShellFixture(t *testing.T) (*state.Store, string, map[string]string, map[string]string) {
	t.Helper()
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Dir(s.Root)
	testutil.ShellScripts(t, prefix)
	rcs := map[string]string{
		"bash": filepath.Join(prefix, "home", ".bashrc"),
		"zsh":  filepath.Join(prefix, "zsh-config", ".zshrc"),
		"fish": filepath.Join(prefix, "fish-config", "config.fish"),
	}
	originals := map[string]string{}
	for shell, rc := range rcs {
		if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil {
			t.Fatal(err)
		}
		originals[rc] = "# personal " + shell + " configuration\n"
		if err := os.WriteFile(rc, []byte(originals[rc]), 0640); err != nil {
			t.Fatal(err)
		}
	}
	return s, filepath.Join(prefix, "verge-tui"), rcs, originals
}

func TestMultipleShellRegistrationsCoexistAndUninstallPreservesUserContent(t *testing.T) {
	s, executable, rcs, originals := multiShellFixture(t)
	start, _ := markers(blockID(s))
	for range 2 {
		for _, shell := range []string{"bash", "zsh", "fish"} {
			if _, err := RegisterShell(s, executable, rcs[shell], shell); err != nil {
				t.Fatalf("register %s alongside other shells: %v", shell, err)
			}
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		rc, err := RegisteredRCForShell(s, shell)
		if err != nil || rc != rcs[shell] {
			t.Fatalf("lost %s registration: %s %v", shell, rc, err)
		}
		path := rc
		if shell == "fish" {
			path = filepath.Join(filepath.Dir(rc), "conf.d", "verge-tui.fish")
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.Count(string(data), start) != 1 {
			t.Fatalf("missing or duplicate loader for %s: %s %v", shell, data, err)
		}
	}
	if rc, err := RegisteredRC(s); err != nil || rc != rcs["bash"] {
		t.Fatal("adding shells changed the installation's primary registration", rc, err)
	}
	if got := loginText(t, rcs["fish"]); got != originals[rcs["fish"]] {
		t.Fatal("fish registration edited config.fish", got)
	}
	for range 2 {
		if err := Remove(s); err != nil {
			t.Fatal("remove all shell registrations", err)
		}
	}
	for rc, expected := range originals {
		if got := loginText(t, rc); got != expected {
			t.Fatalf("uninstall changed user content in %s: %q", rc, got)
		}
		info, err := os.Stat(rc)
		if err != nil || info.Mode().Perm() != 0640 {
			t.Fatal("uninstall changed rc permissions", rc, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(rcs["fish"]), "conf.d", "verge-tui.fish")); !os.IsNotExist(err) {
		t.Fatal("fish autoload loader survived uninstall")
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if _, err := os.Stat(ScriptPath(executable, shell)); err != nil {
			t.Fatal("uninstall removed a bundled script", shell, err)
		}
	}
}

func TestRefreshingMultipleShellsTracksEveryLoginPath(t *testing.T) {
	s, executable, rcs, originals := multiShellFixture(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if _, err := RegisterShell(s, executable, rcs[shell], shell); err != nil {
			t.Fatal(err)
		}
	}
	oldLogin := filepath.Join(filepath.Dir(rcs["bash"]), ".profile")
	newLogin := filepath.Join(filepath.Dir(rcs["bash"]), ".bash_profile")
	putLoginText(t, newLogin, "# preferred login profile\n")
	paths, err := RegistrationPaths(s, executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{registrationPath(s), rcs["bash"], rcs["zsh"], rcs["fish"], oldLogin, newLogin, filepath.Join(filepath.Dir(rcs["fish"]), "conf.d", "verge-tui.fish")} {
		if !containsPath(paths, path) {
			t.Fatal("rollback paths omitted a shell entry point", path, paths)
		}
	}
	if err := RefreshRegistrations(s, executable); err != nil {
		t.Fatal(err)
	}
	if got := loginText(t, oldLogin); got != "" {
		t.Fatal("refresh left the obsolete Bash login loader", got)
	}
	if got := loginText(t, newLogin); !strings.Contains(got, ScriptPath(executable, "bash")) || !strings.HasPrefix(got, "# preferred login profile\n") {
		t.Fatal("refresh did not preserve and update the effective Bash login file", got)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if rc, err := RegisteredRCForShell(s, shell); err != nil || rc != rcs[shell] {
			t.Fatal("refresh lost a registered shell", shell, rc, err)
		}
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	for rc, expected := range originals {
		if loginText(t, rc) != expected {
			t.Fatal("refresh/uninstall changed unrelated user content", rc)
		}
	}
	if loginText(t, newLogin) != "# preferred login profile\n" {
		t.Fatal("uninstall removed personal login configuration")
	}
}

func TestDifferentShellsCannotShareTheSameRC(t *testing.T) {
	s, executable, rcs, _ := multiShellFixture(t)
	if _, err := RegisterShell(s, executable, rcs["bash"], "bash"); err != nil {
		t.Fatal(err)
	}
	beforeRC := loginText(t, rcs["bash"])
	beforeMeta := loginText(t, registrationPath(s))
	if _, err := RegisterShell(s, executable, rcs["bash"], "zsh"); err == nil {
		t.Fatal("different shell syntaxes were allowed to own the same rc")
	}
	if loginText(t, rcs["bash"]) != beforeRC || loginText(t, registrationPath(s)) != beforeMeta {
		t.Fatal("a conflicting registration changed the existing shell")
	}
}

func TestFishConfigSymlinkRetainsLogicalAutoloadDirectoryAcrossRefresh(t *testing.T) {
	s, executable, rcs, originals := multiShellFixture(t)
	target := filepath.Join(filepath.Dir(s.Root), "dotfile-config.fish")
	if err := os.WriteFile(target, []byte(originals[rcs["fish"]]), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rcs["fish"]); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, rcs["fish"]); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := RegisterShell(s, executable, rcs["fish"], "fish"); err != nil {
			t.Fatal("register symlinked fish config", err)
		}
	}
	if err := RefreshRegistrations(s, executable); err != nil {
		t.Fatal("refresh symlinked fish config", err)
	}
	loader := filepath.Join(filepath.Dir(rcs["fish"]), "conf.d", "verge-tui.fish")
	if _, err := os.Stat(loader); err != nil {
		t.Fatal("fish autoload moved away from its configured directory", err)
	}
	if info, err := os.Lstat(rcs["fish"]); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("refresh replaced the user's fish config symlink", err)
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	if loginText(t, target) != originals[rcs["fish"]] {
		t.Fatal("fish config target was modified")
	}
}

func TestInvalidSecondaryRegistrationDoesNotPartiallyUninstallOtherShells(t *testing.T) {
	s, executable, rcs, _ := multiShellFixture(t)
	for _, shell := range []string{"bash", "zsh"} {
		if _, err := RegisterShell(s, executable, rcs[shell], shell); err != nil {
			t.Fatal(err)
		}
	}
	start, _ := markers(blockID(s))
	putLoginText(t, rcs["zsh"], "# user content\n"+start+"\n")
	beforeBash := loginText(t, rcs["bash"])
	beforeLogin := loginText(t, filepath.Join(filepath.Dir(rcs["bash"]), ".profile"))
	beforeMeta := loginText(t, registrationPath(s))
	if err := Remove(s); err == nil {
		t.Fatal("invalid secondary markers were ignored")
	}
	if loginText(t, rcs["bash"]) != beforeBash || loginText(t, filepath.Join(filepath.Dir(rcs["bash"]), ".profile")) != beforeLogin || loginText(t, registrationPath(s)) != beforeMeta {
		t.Fatal("secondary validation failure partially removed another registered shell")
	}
}

func TestRegistrationUpdateSerializesShellRegistrationAndRefresh(t *testing.T) {
	s, executable, rcs, originals := multiShellFixture(t)
	if _, err := RegisterShell(s, executable, rcs["bash"], "bash"); err != nil {
		t.Fatal(err)
	}
	tx, err := BeginRegistrationUpdate(s, executable)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			tx.Close()
		}
	}()
	started := make(chan struct{})
	registered := make(chan error, 1)
	go func() {
		close(started)
		_, err := RegisterShell(s, executable, rcs["zsh"], "zsh")
		registered <- err
	}()
	<-started
	select {
	case err := <-registered:
		t.Fatal("another shell registration completed while the update held its lock", err)
	case <-time.After(100 * time.Millisecond):
	}
	refreshed := make(chan error, 1)
	go func() { refreshed <- tx.Refresh("") }()
	select {
	case err := <-refreshed:
		if err != nil {
			t.Fatal("refresh under the update lock", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("refresh attempted to reacquire the transaction's lock")
	}
	select {
	case err := <-registered:
		t.Fatal("refresh released the transaction's lock before Close", err)
	default:
	}
	err = tx.Close()
	closed = true
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-registered:
		if err != nil {
			t.Fatal("register zsh after the update released its lock", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shell registration remained blocked after the transaction closed")
	}
	for _, shell := range []string{"bash", "zsh"} {
		if rc, err := RegisteredRCForShell(s, shell); err != nil || rc != rcs[shell] {
			t.Fatal("concurrent update lost a registered shell", shell, rc, err)
		}
	}
	if err := Remove(s); err != nil {
		t.Fatal("remove registrations after the concurrent update", err)
	}
	for rc, expected := range originals {
		if got := loginText(t, rc); got != expected {
			t.Fatal("uninstall changed user content after the concurrent update", rc, got)
		}
	}
	if _, err := os.Stat(registrationPath(s)); !os.IsNotExist(err) {
		t.Fatal("uninstall retained registration metadata", err)
	}
}
