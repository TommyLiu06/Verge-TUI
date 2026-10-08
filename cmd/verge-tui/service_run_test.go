package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"verge-tui/internal/app"
	"verge-tui/internal/core"
	"verge-tui/internal/management"
	"verge-tui/internal/state"
)

func TestSharedCoreReplacementDoesNotStopSupervisor(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	dir, err := os.MkdirTemp("../../.local", "svc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s, err := state.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := core.Import(ctx, s, "first", binary); err != nil {
		t.Fatal(err)
	}
	b := app.New(s, "test")
	if err := b.Manager.Use(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Manager.Stop()
	var mu sync.RWMutex
	server, err := management.Serve(ctx, s, b.SharedHandlers(&mu))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); server.Close() }()
	finished := make(chan error, 1)
	go func() { finished <- watchManaged(ctx, b.Manager, &mu, server.Done) }()
	client := app.NewRemote(s, "test")
	defer client.Close(ctx)
	if err := client.Remote.ImportCore(ctx, "second", binary); err != nil {
		t.Fatal("ordinary client could not import an offline core", err)
	}
	if err := client.Execute(ctx, app.Action{Kind: "core-use", ID: "second"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		t.Fatal("supervisor treated replacement as a crash", err)
	default:
	}
	view, err := client.Load(ctx)
	if err != nil || !view.Online || view.Settings.Core != "second" {
		t.Fatal("replacement did not restart the managed core", err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop on cancellation")
	}
}
