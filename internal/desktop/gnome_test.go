package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"verge-tui/internal/state"
)

func TestProxyCredentialsAndRestore(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.Read()
	enabled := false
	restored := false
	run := func(ctx context.Context, name string, args []string, input string) (string, error) {
		if strings.Contains(strings.Join(args, " "), c.ProxyPassword) {
			t.Fatal("credential exposed in process arguments")
		}
		if name == "dconf" {
			switch args[0] {
			case "dump":
				return "[/]\nmode='none'", nil
			case "reset":
				enabled = false
				return "", nil
			case "load":
				if strings.Contains(input, "mode='manual'") {
					enabled = true
					if !strings.Contains(input, c.ProxyPassword) {
						t.Fatal("authentication not configured")
					}
				} else {
					restored = input == "[/]\nmode='none'\n"
				}
				return "", nil
			}
		}
		switch args[2] {
		case "mode":
			if enabled {
				return "'manual'", nil
			}
			return "'none'", nil
		case "host":
			return "'127.0.0.1'", nil
		case "port":
			return fmt.Sprint(c.MixedPort), nil
		}
		return "", nil
	}
	g := GNOME{Store: s, Run: run}
	ctx := context.Background()
	if err := g.Enable(ctx, c); err != nil {
		t.Fatal(err)
	}
	if !g.Read(ctx, c).Owned {
		t.Fatal("observed proxy not owned")
	}
	info, _ := os.Stat(filepath.Join(s.Root, "gnome-proxy-backup.ini"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("proxy backup not private")
	}
	if err := g.Disable(ctx, c); err != nil {
		t.Fatal(err)
	}
	if !restored || enabled {
		t.Fatal("original desktop settings not restored")
	}
}
