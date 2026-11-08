package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestShellUninstallRemovesRegisteredFunctionsAndPathOnly(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	testutil.ShellScripts(t, filepath.Dir(executable))
	dir := t.TempDir()
	s, err := state.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := shellenv.Write(s); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(dir, ".zshrc")
	original := "# custom configuration\nexport EDITOR=vim\n"
	if err := os.WriteFile(rc, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := run([]string{"--data-dir", s.Root, "shell", "install", "--rc-file", rc}); err != nil {
			t.Fatal(err)
		}
	}
	installed, err := os.ReadFile(rc)
	if err != nil || !strings.Contains(string(installed), "/shell/proxy.sh") || strings.Count(string(installed), "# >>> verge-tui") != 1 {
		t.Fatal("installation did not create one functions/PATH block", err)
	}
	if err := run([]string{"--data-dir", s.Root, "shell", "uninstall", "--rc-file", rc}); err == nil {
		t.Fatal("uninstall allowed an unregistered target rc")
	}
	unchanged, _ := os.ReadFile(rc)
	if string(unchanged) != string(installed) {
		t.Fatal("invalid uninstall modified rc")
	}
	// Cleanup must work even when the proxy environment file is already gone.
	if err := os.Remove(shellenv.FilePath(s)); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{}, {}, {"--all"}} {
		args := append([]string{"--data-dir", s.Root, "shell", "uninstall"}, flags...)
		if err := run(args); err != nil {
			t.Fatal("uninstall was not idempotent", err)
		}
	}
	data, err := os.ReadFile(rc)
	if err != nil || string(data) != original {
		t.Fatal("uninstall removed unrelated rc settings or retained integration", err)
	}
	info, err := os.Stat(rc)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("uninstall changed rc permissions", err)
	}
	if _, err := s.Read(); err != nil {
		t.Fatal("shell uninstall removed application settings", err)
	}
}

func shellSelectionFixture(t *testing.T) (*state.Store, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	testutil.ShellScripts(t, filepath.Dir(executable))
	root := testutil.Dir(t)
	s, err := state.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := shellenv.Write(s); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(root, "tools")
	if err := os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "ps"), []byte("#!/bin/sh\nprintf '%s\\n' \"$VERGE_TUI_TEST_PARENT_SHELL\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("ZDOTDIR", "")
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VERGE_TUI_TEST_PARENT_SHELL", "bash")
	for _, name := range []string{".bashrc", ".zshrc"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("# personal "+name+"\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	return s, home
}

func TestShellInstallDetectsZshAfterBashRegistration(t *testing.T) {
	s, home := shellSelectionFixture(t)
	invoke := func(args ...string) error {
		return run(append([]string{"--data-dir", s.Root, "shell", "install"}, args...))
	}
	if err := invoke(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VERGE_TUI_TEST_PARENT_SHELL", "zsh")
	for range 2 {
		if err := invoke(); err != nil {
			t.Fatal("install in the actual zsh reused the old Bash registration", err)
		}
	}
	for _, name := range []string{".bashrc", ".zshrc"} {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || !strings.HasPrefix(string(data), "# personal "+name+"\n") || strings.Count(string(data), "# >>> verge-tui") != 1 {
			t.Fatalf("switching shells removed or duplicated %s: %s %v", name, data, err)
		}
	}
	if err := run([]string{"--data-dir", s.Root, "shell", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".bashrc", ".zshrc"} {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || string(data) != "# personal "+name+"\n" {
			t.Fatal("shell uninstall did not remove all registered loaders", name, err)
		}
	}
}

func TestExplicitShellChoosesItsOwnRCAndRetainsCustomRegistration(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default rc", true: "custom rc"}[custom], func(t *testing.T) {
			s, home := shellSelectionFixture(t)
			invoke := func(args ...string) error {
				return run(append([]string{"--data-dir", s.Root, "shell", "install"}, args...))
			}
			if err := invoke(); err != nil {
				t.Fatal(err)
			}
			expected := filepath.Join(home, ".zshrc")
			args := []string{"--shell", "zsh"}
			if custom {
				expected = filepath.Join(home, "personal-zsh-startup")
				args = append(args, "--rc-file", expected)
			}
			if err := invoke(args...); err != nil {
				t.Fatal("explicit zsh selection reused the Bash rc", err)
			}
			t.Setenv("ZDOTDIR", filepath.Join(home, "new-zdotdir"))
			if err := invoke("--shell", "zsh"); err != nil {
				t.Fatal("same-shell refresh lost its existing rc selection", err)
			}
			if rc, err := shellenv.RegisteredRCForShell(s, "zsh"); err != nil || rc != expected {
				t.Fatal("saved zsh rc was replaced", rc, err)
			}
			data, err := os.ReadFile(expected)
			if err != nil || strings.Count(string(data), "# >>> verge-tui") != 1 {
				t.Fatal("same-shell refresh duplicated its loader", err)
			}
			if _, err := os.Stat(filepath.Join(home, "new-zdotdir", ".zshrc")); !os.IsNotExist(err) {
				t.Fatal("existing registration was copied into an unrelated zsh configuration directory")
			}
		})
	}
}
