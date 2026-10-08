package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
)

func TestShellCommandsDoNotOpenPrivateSystemSettings(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary permission enforcement")
	}
	s, err := state.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := shellenv.Write(s); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(s.Root, "settings.json")
	if err := os.Chmod(settings, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(settings, 0600)
	rc := filepath.Join(filepath.Dir(s.Root), "zsh-config", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil {
		t.Fatal(err)
	}
	psDir := filepath.Join(filepath.Dir(s.Root), "tools")
	if err := os.Mkdir(psDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(psDir, "ps"), []byte("#!/bin/sh\nprintf '%s\\n' zsh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", psDir)
	t.Setenv("ZDOTDIR", filepath.Dir(rc))
	t.Setenv("SHELL", "/bin/zsh")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = w
	err = run([]string{"--data-dir", s.Root, "shell", "install"})
	os.Stdout = previous
	w.Close()
	io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal("shell registration needed private settings", err)
	}
	data, err := os.ReadFile(rc)
	if err != nil || !strings.Contains(string(data), ".verge-tui/") {
		t.Fatal("current zsh rc was not registered", err)
	}
	if err := run([]string{"--data-dir", s.Root, "shell", "uninstall"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(rc)
	if strings.Contains(string(data), "verge_on") {
		t.Fatal("shell uninstall retained functions")
	}
	info, _ := os.Stat(settings)
	if info.Mode().Perm() != 0000 {
		t.Fatal("shell commands changed private settings permissions")
	}
	b, err := sharedBackend(s.Root)
	if err != nil || b == nil || b.Remote == nil {
		t.Fatal("private system settings did not select remote backend", err)
	}
	defer b.Close(context.Background())
	if err := runShared([]string{"run"}, b); err == nil || !strings.Contains(err.Error(), "require root") {
		t.Fatal("ordinary CLI started a system service", err)
	}
}
