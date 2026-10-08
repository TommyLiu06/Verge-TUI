package shellenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	command := exec.Command("sh", "-c", script, "sh", rc)
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
}
