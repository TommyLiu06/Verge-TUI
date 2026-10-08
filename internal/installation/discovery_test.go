package installation

import (
	"os"
	"path/filepath"
	"testing"

	"verge-tui/internal/state"
)

func TestPublicDiscoveryAndOwnedCommandLink(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "app")
	s, err := state.Open(filepath.Join(prefix, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(prefix, "verge-tui"), "binary")
	link := filepath.Join(root, "bin", "verge-tui")
	for range 2 {
		if err := InstallCommandLink(prefix, link); err != nil {
			t.Fatal(err)
		}
	}
	if err := Record(prefix, "test", link); err != nil {
		t.Fatal(err)
	}
	summary, err := Discover(prefix)
	if err != nil || summary.Mode != "system" || summary.Version != "test" || summary.CommandLink != link {
		t.Fatal(summary, err)
	}
	info, _ := os.Stat(filepath.Join(prefix, SummaryFile))
	if info.Mode().Perm() != 0644 {
		t.Fatal("discovery requires private state access")
	}
	if err := RemoveCommandLink(prefix, link); err != nil {
		t.Fatal(err)
	}
	put(t, link, "another application")
	if err := InstallCommandLink(prefix, link); err == nil {
		t.Fatal("overwrote an unrelated command")
	}
	if err := RemoveCommandLink(prefix, link); err != nil {
		t.Fatal(err)
	}
	if content(t, link) != "another application" {
		t.Fatal("uninstall removed another application's command")
	}
	os.Remove(link)
	if err := os.Symlink(filepath.Join(root, "different", "verge-tui"), link); err != nil {
		t.Fatal(err)
	}
	if err := InstallCommandLink(prefix, link); err == nil {
		t.Fatal("overwrote another installation's link")
	}
	RemoveCommandLink(prefix, link)
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("uninstall removed another installation's symlink")
	}
}
