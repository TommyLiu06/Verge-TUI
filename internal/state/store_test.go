package state

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPrivatePersistentSettings(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if c.MixedPort < 20000 || c.MixedPort == c.APIPort || len(c.Secret) != 48 || c.Secret == c.ProxyPassword {
		t.Fatalf("invalid initialization: ports %d/%d", c.MixedPort, c.APIPort)
	}
	for _, path := range []string{s.Root, filepath.Join(s.Root, "settings.json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("private data exposed: %s", path)
		}
	}
	s2, err := Open(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := s2.Read()
	if c2.Secret != c.Secret {
		t.Fatal("credentials changed on reopen")
	}
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			err := s.Update(func(c *Settings) error {
				c.Profiles = append(c.Profiles, Profile{ID: string(rune('a' + i))})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	c, _ = s.Read()
	if len(c.Profiles) != 12 {
		t.Fatal("concurrent updates lost")
	}
}

func TestPortConflictAndIdentifiers(t *testing.T) {
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	if CheckPort(udp.LocalAddr().(*net.UDPAddr).Port) == nil {
		t.Fatal("UDP collision was missed")
	}
	for _, id := range []string{"../escape", "/etc/passwd", "..", "a/b", ""} {
		if ValidID(id) {
			t.Errorf("accepted unsafe identifier %q", id)
		}
	}
}
