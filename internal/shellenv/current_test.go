package shellenv

import (
	"os"
	"path/filepath"
	"testing"
	"verge-tui/internal/testutil"
)

func TestCurrentRCUsesParentShellAndZdotdir(t *testing.T) {
	root := testutil.Dir(t)
	ps := filepath.Join(root, "ps")
	if err := os.WriteFile(ps, []byte("#!/bin/sh\nprintf '%s\\n' zsh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("ZDOTDIR", filepath.Join(root, "zsh-config"))
	rc, err := CurrentRC()
	if err != nil || rc != filepath.Join(root, "zsh-config", ".zshrc") {
		t.Fatalf("ignored current shell or ZDOTDIR: %s %v", rc, err)
	}
	t.Setenv("PATH", filepath.Join(root, "missing"))
	rc, err = CurrentRC()
	if err != nil || rc != filepath.Join(os.Getenv("HOME"), ".bashrc") {
		t.Fatalf("missing ps did not use SHELL: %s %v", rc, err)
	}
}

func TestResolveRCSupportedShells(t *testing.T) {
	for _, item := range []struct{ shell, home, zdot, want string }{
		{"bash", "/example/home", "", "/example/home/.bashrc"},
		{"zsh", "/example/home", "", "/example/home/.zshrc"},
		{"zsh", "/example/home", "/example/zsh", "/example/zsh/.zshrc"},
		{"dash", "/example/home", "", ""},
		{"bash", "relative", "", ""},
	} {
		rc, err := resolveRC(item.shell, item.home, item.zdot)
		if item.want == "" && err == nil || item.want != "" && (err != nil || rc != item.want) {
			t.Fatalf("wrong rc resolution: %+v: %s %v", item, rc, err)
		}
	}
}
