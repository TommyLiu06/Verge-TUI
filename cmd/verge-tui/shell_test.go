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
