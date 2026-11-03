package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"verge-tui/internal/installation"
	"verge-tui/internal/state"
)

func TestUpdateOptionsBelongToInstallationAndAreNotSharedActions(t *testing.T) {
	prefix := t.TempDir()
	s, err := state.Open(filepath.Join(prefix, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "verge-tui"), []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := installation.Record(prefix, "0.1.19", ""); err != nil {
		t.Fatal(err)
	}
	b := New(s, "0.1.19")
	a := Action{Kind: "update-options", Fields: map[string]string{"check-minutes": "60", "idle-minutes": "10"}}
	if err := b.Execute(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	page, err := b.ReadPage(context.Background(), "app-update", "status")
	if err != nil || page.Update == nil || page.Update.Options.CheckMinutes != 60 || page.Update.Options.IdleMinutes != 10 {
		t.Fatalf("policy did not persist: %#v %v", page.Update, err)
	}
	if err := b.Execute(context.Background(), Action{Kind: "update-options", Fields: map[string]string{"idle-minutes": "-1"}}); err == nil {
		t.Fatal("invalid idle time accepted")
	}
	if sharedAction(a) == nil || sharedAction(Action{Kind: "app-update-apply"}) == nil {
		t.Fatal("shared service allowed privilege-sensitive update operations")
	}
	remote := NewRemote(s, "0.1.19")
	defer remote.Remote.Close()
	if err := remote.Execute(context.Background(), a); err == nil {
		t.Fatal("an ordinary shared client changed the installation policy")
	}
}
