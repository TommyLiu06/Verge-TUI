package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"mihomo-tui/internal/shellenv"
	"mihomo-tui/internal/state"
)

func TestPerUserDesktopRestoreUsesSavedSession(t *testing.T) {
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
	user := UserStore(s)
	if err := PrepareUser(user); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Read()
	tools := filepath.Join(filepath.Dir(s.Root), "tools")
	if err := os.Mkdir(tools, 0700); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(filepath.Dir(s.Root), "restored.ini")
	bus := "unix:path=" + filepath.Join(filepath.Dir(s.Root), "saved-session-bus")
	gsettings := "#!/bin/sh\n[ \"$DBUS_SESSION_BUS_ADDRESS\" = " + shellenv.Quote(bus) + " ] || exit 1\ncase \"$3\" in\nmode) printf \"'manual'\\n\" ;;\nhost) printf \"'127.0.0.1'\\n\" ;;\nport) printf '%s\\n' " + strconv.Itoa(c.MixedPort) + " ;;\nesac\n"
	dconf := "#!/bin/sh\n[ \"$DBUS_SESSION_BUS_ADDRESS\" = " + shellenv.Quote(bus) + " ] || exit 1\nif [ \"$1\" = load ]; then cat > " + shellenv.Quote(restored) + "; fi\n"
	for name, body := range map[string]string{"gsettings": gsettings, "dconf": dconf} {
		if err := os.WriteFile(filepath.Join(tools, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools+":"+os.Getenv("PATH"))
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", bus)
	if err := SaveSession(user); err != nil {
		t.Fatal(err)
	}
	backup := "[/]\nmode='none'\n"
	if err := state.Write(filepath.Join(user.Root, "gnome-proxy-backup.ini"), []byte(backup), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=uninstaller-session")
	if err := RestoreUser(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(restored)
	if err != nil || string(data) != backup {
		t.Fatal("original desktop settings were not restored", err)
	}
	if _, err := os.Stat(user.Root); !os.IsNotExist(err) {
		t.Fatal("restored desktop kept its registration")
	}
}
