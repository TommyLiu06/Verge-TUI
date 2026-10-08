package shellenv

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"verge-tui/internal/state"
)

func TestUninstallCleansRootAndPerUserRCsWithoutDuplicates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("fixture models root and an ordinary account separately")
	}
	s := store(t)
	if err := s.Update(func(c *state.Settings) error { c.InstallMode = "system"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	rootRC, userRC := filepath.Join(filepath.Dir(s.Root), "root-rc"), filepath.Join(filepath.Dir(s.Root), "user-rc")
	for _, rc := range []string{rootRC, userRC} {
		if err := os.WriteFile(rc, []byte("# unrelated settings\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Register(s, "/shared/verge-tui", rootRC); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(state.ShellUsersPath(s), strconv.Itoa(os.Geteuid())+".json")
	for range 2 {
		if _, err := registerAt(s, "/shared/verge-tui", userRC, meta); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(userRC)
	if strings.Count(string(data), "# >>> verge-tui") != 1 {
		t.Fatal("ordinary registration duplicated functions")
	}
	called := false
	if err := removeAll(s, 0, func(uid, gid uint32) error {
		called = true
		if int(uid) != os.Geteuid() || uid == 0 {
			t.Fatal("cleanup did not target the registration's ordinary uid")
		}
		return removeAt(s, meta)
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("uninstall ignored the ordinary user's registration")
	}
	for _, rc := range []string{rootRC, userRC} {
		data, _ := os.ReadFile(rc)
		info, _ := os.Stat(rc)
		if string(data) != "# unrelated settings\n" || info.Mode().Perm() != 0640 {
			t.Fatal("cleanup changed unrelated rc text or permissions")
		}
	}
	if err := removeAll(s, 0, func(uint32, uint32) error { t.Fatal("repeated cleanup invoked a removed user"); return nil }); err != nil {
		t.Fatal("repeated cleanup failed", err)
	}
}

func TestUninstallRejectsMismatchedUIDRegistration(t *testing.T) {
	s := store(t)
	if err := state.PrepareShared(s); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(state.ShellUsersPath(s), "999999.json")
	if err := os.WriteFile(meta, []byte(`{"rc":"/administrator/rc"}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := removeAll(s, 0, func(uint32, uint32) error { t.Fatal("uninstall executed mismatched registration"); return nil })
	if err == nil {
		t.Fatal("registration filename was not checked against its owner")
	}
}
