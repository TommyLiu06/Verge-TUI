package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemSharedPathsKeepSettingsPrivateAcrossReopen(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := PrepareShared(s); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(s.Root); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{
		{s.Root, 0711}, {SharedPath(s), 0755}, {filepath.Join(s.Root, "profiles"), 0700}, {filepath.Join(s.Root, "cores"), 0700}, {filepath.Join(s.Root, "runtime"), 0700}, {filepath.Join(s.Root, "settings.json"), 0600},
	} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.mode {
			t.Fatalf("incorrect shared/private permissions: %s %v", item.path, err)
		}
	}
	info, err := os.Stat(ShellUsersPath(s))
	if err != nil || info.Mode().Perm() != 0777 || info.Mode()&os.ModeSticky == 0 {
		t.Fatal("per-user registrations lack sticky directory protection")
	}
}
