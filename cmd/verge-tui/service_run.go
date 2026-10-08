package main

import (
	"context"
	"fmt"
	"sync"

	"verge-tui/internal/app"
	"verge-tui/internal/core"
	"verge-tui/internal/management"
	"verge-tui/internal/state"
)

func runManaged(ctx context.Context, m *core.Manager, s *state.Store) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.RWMutex
	if err := m.Start(ctx); err != nil {
		return err
	}
	defer func() { mu.Lock(); defer mu.Unlock(); m.Stop() }()
	c, err := s.Read()
	if err != nil {
		return err
	}
	var server *management.Server
	var serverDone <-chan error
	if c.InstallMode == "system" {
		b := app.New(s, version)
		b.Manager = m
		server, err = management.Serve(ctx, s, b.SharedHandlers(&mu))
		if err != nil {
			return err
		}
		defer func() { cancel(); server.Close() }()
		serverDone = server.Done
	}
	fmt.Println("Managed core running")
	return watchManaged(ctx, m, &mu, serverDone)
}

func watchManaged(ctx context.Context, m *core.Manager, mu *sync.RWMutex, serverDone <-chan error) error {
	for {
		mu.RLock()
		done := m.Done()
		mu.RUnlock()
		select {
		case <-ctx.Done():
			return nil
		case err := <-serverDone:
			return fmt.Errorf("system management server stopped: %v", err)
		case err := <-done:
			mu.RLock()
			current := m.Done()
			mu.RUnlock()
			if current != done {
				continue
			}
			return fmt.Errorf("core stopped unexpectedly: %v", err)
		}
	}
}
