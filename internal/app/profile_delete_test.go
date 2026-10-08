package app

import (
	"context"
	"os"
	"testing"

	"mihomo-tui/internal/profile"
	"mihomo-tui/internal/state"
)

func TestDeleteProfileRemovesYAMLAndSelection(t *testing.T) {
	for _, selected := range []bool{false, true} {
		s, err := state.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		p, err := profile.Local(s, "Delete me", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Update(func(c *state.Settings) error {
			c.HomeGroup[p.ID] = "Proxy"
			if selected {
				c.Current = p.ID
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		b := New(s, "test")
		if err := b.Execute(context.Background(), Action{Kind: "profile-delete", ID: p.ID}); err != nil {
			t.Fatal(err)
		}
		c, err := s.Read()
		if err != nil {
			t.Fatal(err)
		}
		path, _ := s.ProfilePath(p.ID)
		if _, err := os.Stat(path); !os.IsNotExist(err) || len(c.Profiles) != 0 || c.Current != "" || c.HomeGroup[p.ID] != "" {
			t.Fatal("deleted profile retained its YAML or settings")
		}
	}
}

func TestDeleteCurrentProfileKeepsDataIfSwitchFails(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Local(s, "Keep me", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *state.Settings) error { c.Current = p.ID; c.Core = "missing"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := New(s, "test").Execute(context.Background(), Action{Kind: "profile-delete", ID: p.ID}); err == nil {
		t.Fatal("deleted selected profile after failed core validation")
	}
	c, _ := s.Read()
	path, _ := s.ProfilePath(p.ID)
	if _, err := os.Stat(path); err != nil || c.Current != p.ID || len(c.Profiles) != 1 {
		t.Fatal("failed deletion did not preserve the original profile")
	}
}
