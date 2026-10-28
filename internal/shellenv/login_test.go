package shellenv

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func loginFixture(t *testing.T) (*state.Store, string, string) {
	t.Helper()
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Dir(s.Root)
	testutil.ShellScripts(t, prefix)
	rc := filepath.Join(prefix, "custom-home", ".bashrc")
	if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte("# existing rc\n"), 0640); err != nil {
		t.Fatal(err)
	}
	return s, filepath.Join(prefix, "verge-tui"), rc
}

func loginText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func putLoginText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
}

func TestBashLoginRegistrationChoosesEffectiveProfileAndPreservesContent(t *testing.T) {
	for _, tc := range []struct {
		name, expected string
		files          []string
	}{
		{"create-profile", ".profile", nil},
		{"profile", ".profile", []string{".profile"}},
		{"bash-login", ".bash_login", []string{".profile", ".bash_login"}},
		{"bash-profile", ".bash_profile", []string{".profile", ".bash_login", ".bash_profile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, executable, rc := loginFixture(t)
			originals := map[string]string{rc: loginText(t, rc)}
			for _, name := range tc.files {
				path := filepath.Join(filepath.Dir(rc), name)
				originals[path] = "# existing " + name + "\n"
				putLoginText(t, path, originals[path])
			}
			login := filepath.Join(filepath.Dir(rc), tc.expected)
			before, err := ManagedPaths(s, executable, rc, "bash")
			if err != nil || !containsPath(before, login) {
				t.Fatal("login profile missing from pre-registration rollback paths", before, err)
			}
			for range 2 {
				if _, err := RegisterShell(s, executable, rc, "bash"); err != nil {
					t.Fatal(err)
				}
			}
			start, _ := markers(blockID(s))
			if text := loginText(t, login); strings.Count(text, start) != 1 || !strings.Contains(text, "${BASH_VERSION-}") {
				t.Fatal("login loader was duplicated or not restricted to Bash")
			}
			var reg registration
			if err := json.Unmarshal([]byte(loginText(t, registrationPath(s))), &reg); err != nil {
				t.Fatal(err)
			}
			if len(reg.ExtraRCs) != 1 || reg.ExtraRCs[0] != login || containsPath(reg.Files, login) {
				t.Fatal("user login profile was recorded as a disposable generated file", reg)
			}
			for _, shell := range []string{"bash", "sh"} {
				program := `. "$1"; command -v verge-on >/dev/null`
				if shell == "sh" {
					program = `. "$1"; ! command -v verge-on >/dev/null`
				}
				args := []string{"-c", program, "test", login}
				if shell == "bash" {
					args = append([]string{"--noprofile", "--norc"}, args...)
				}
				cmd := exec.Command(shell, args...)
				cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(rc))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s login loader: %v\n%s", shell, err, out)
				}
			}
			if err := Remove(s); err != nil {
				t.Fatal(err)
			}
			for path, text := range originals {
				if got := loginText(t, path); got != text {
					t.Fatal("uninstallation changed user content", path, got)
				}
			}
			if _, existed := originals[login]; !existed && loginText(t, login) != "" {
				t.Fatal("generated login profile retained a loader")
			}
		})
	}
}

func TestBashLoginRegistrationFollowsSymlinksWithoutMovingLogicalHome(t *testing.T) {
	s, executable, rc := loginFixture(t)
	dotfiles := filepath.Join(filepath.Dir(s.Root), "dotfiles")
	if err := os.Mkdir(dotfiles, 0700); err != nil {
		t.Fatal(err)
	}
	targetRC, targetLogin := filepath.Join(dotfiles, "rc"), filepath.Join(dotfiles, "profile")
	putLoginText(t, targetRC, "# source rc\n")
	putLoginText(t, targetLogin, "# source profile\n")
	if err := os.Remove(rc); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetRC, rc); err != nil {
		t.Fatal(err)
	}
	login := filepath.Join(filepath.Dir(rc), ".profile")
	if err := os.Symlink(targetLogin, login); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterShell(s, executable, rc, "bash"); err != nil {
		t.Fatal(err)
	}
	paths, err := ManagedPaths(s, executable, targetRC, "bash")
	if err != nil || !containsPath(paths, targetLogin) || containsPath(paths, filepath.Join(dotfiles, ".profile")) {
		t.Fatal("registered symlink lost the logical login directory", paths, err)
	}
	if _, err := RegisterShell(s, executable, targetRC, "bash"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{rc, login} {
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("registration replaced a user symlink", link, err)
		}
	}
	if loginText(t, targetRC) != "# source rc\n" || loginText(t, targetLogin) != "# source profile\n" {
		t.Fatal("symlink target content was not restored")
	}
}

func TestBashLoginRegistrationTracksChangedPriorityForRollbackAndCleanup(t *testing.T) {
	s, executable, rc := loginFixture(t)
	profile := filepath.Join(filepath.Dir(rc), ".profile")
	putLoginText(t, profile, "# profile\n")
	if _, err := RegisterShell(s, executable, rc, "bash"); err != nil {
		t.Fatal(err)
	}
	higher := filepath.Join(filepath.Dir(rc), ".bash_profile")
	putLoginText(t, higher, "# higher priority\n")
	paths, err := ManagedPaths(s, executable, rc, "bash")
	if err != nil || !containsPath(paths, profile) || !containsPath(paths, higher) {
		t.Fatal("old and new login entry points missing from rollback snapshots", paths, err)
	}
	if _, err := RegisterShell(s, executable, rc, "bash"); err != nil {
		t.Fatal(err)
	}
	if loginText(t, profile) != "# profile\n" || !strings.Contains(loginText(t, higher), ScriptPath(executable, "bash")) {
		t.Fatal("changing login priority retained obsolete references")
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	if loginText(t, higher) != "# higher priority\n" {
		t.Fatal("uninstallation changed higher-priority profile contents")
	}
}

func TestBrokenBashLoginMarkersRejectRegistrationAndRemovalWithoutChangingRC(t *testing.T) {
	s, executable, rc := loginFixture(t)
	login := filepath.Join(filepath.Dir(rc), ".profile")
	start, _ := markers(blockID(s))
	putLoginText(t, login, "# preserved\n"+start+"\n")
	beforeRC := loginText(t, rc)
	if _, err := RegisterShell(s, executable, rc, "bash"); err == nil {
		t.Fatal("registration accepted incomplete login markers")
	}
	if loginText(t, rc) != beforeRC {
		t.Fatal("failed login validation changed the main rc")
	}
	putLoginText(t, login, "# preserved\n")
	if _, err := RegisterShell(s, executable, rc, "bash"); err != nil {
		t.Fatal(err)
	}
	beforeRC = loginText(t, rc)
	beforeMeta := loginText(t, registrationPath(s))
	putLoginText(t, login, "# preserved\n"+start+"\n")
	if err := Remove(s); err == nil {
		t.Fatal("uninstallation accepted incomplete login markers")
	}
	if loginText(t, rc) != beforeRC || loginText(t, registrationPath(s)) != beforeMeta {
		t.Fatal("failed login cleanup changed another registration file")
	}
}

func TestBashLoginRCUsedAsMainRCHasOnlyOneLoader(t *testing.T) {
	s, executable, rc := loginFixture(t)
	rc = filepath.Join(filepath.Dir(rc), ".profile")
	putLoginText(t, rc, "# user profile\n")
	if _, err := RegisterShell(s, executable, rc, "bash"); err != nil {
		t.Fatal(err)
	}
	start, _ := markers(blockID(s))
	if strings.Count(loginText(t, rc), start) != 1 {
		t.Fatal("main rc and login rc received duplicate loaders")
	}
	if err := Remove(s); err != nil || loginText(t, rc) != "# user profile\n" {
		t.Fatal("main login rc was not preserved", err)
	}
}
