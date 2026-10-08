package shellenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestShellInstallAddsExecutableToPathExactlyOnceAndRemoveRestoresRC(t *testing.T) {
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(filepath.Dir(s.Root), "My ' tool $directory")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "verge-tui")
	testutil.ShellScripts(t, dir)
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(filepath.Dir(s.Root), "shell-rc")
	original := "# user preferences\n"
	os.WriteFile(rc, []byte(original), 0600)
	for range 2 {
		if _, err := Register(s, executable, rc); err != nil {
			t.Fatal(err)
		}
	}
	script := ` . "$1"; . "$1"; command -v verge-tui; printf '%s\n' "$PATH" `
	command := exec.Command("bash", "--noprofile", "--norc", "-c", script, "bash", rc)
	command.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 || lines[0] != executable || strings.Count(lines[1], dir) != 1 {
		t.Fatal("PATH resolution or idempotency failed", string(output))
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(rc)
	if string(data) != original {
		t.Fatal("uninstall left PATH configuration behind")
	}
	if _, err := os.Stat(filepath.Join(dir, "shell", "proxy.sh")); err != nil {
		t.Fatal("shell uninstall removed a bundled script", err)
	}
}

func TestSystemModeLoadersLeavePathUnchanged(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			s := store(t)
			if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := Write(s); err != nil {
				t.Fatal(err)
			}
			prefix := filepath.Dir(s.Root)
			testutil.ShellScripts(t, prefix)
			rc := filepath.Join(prefix, "config", "rc")
			if _, err := RegisterShell(s, filepath.Join(prefix, "verge-tui"), rc, shell); err != nil {
				t.Fatal(err)
			}
			args := []string{"--noprofile", "--norc", "-c", `. "$1"; type verge_on >/dev/null; printf '%s' "$PATH"`, "test", rc}
			program := shell
			if shell == "zsh" {
				args = []string{"-f", "-c", `. "$1"; type verge_on >/dev/null; printf '%s' "$PATH"`, "test", rc}
			}
			if shell == "fish" {
				program = os.Getenv("VERGE_TUI_TEST_FISH")
				if program == "" {
					t.Skip("set VERGE_TUI_TEST_FISH for fish tests")
				}
				loader := filepath.Join(filepath.Dir(rc), "conf.d", "verge-tui.fish")
				args = []string{"--no-config", "-c", "source " + FishQuote(loader) + "; functions -q verge_on; or exit 11; string join : $PATH"}
			}
			cmd := exec.Command(program, args...)
			cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin", "HOME="+prefix, "XDG_CONFIG_HOME="+filepath.Join(prefix, "config"))
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "/usr/bin:/bin" {
				t.Fatalf("system registration changed PATH: %v\n%s", err, out)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(rc), ".verge-tui")); !os.IsNotExist(err) {
				t.Fatal("registration created a generated script directory", err)
			}
		})
	}
}
