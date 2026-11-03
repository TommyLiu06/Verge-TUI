package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"verge-tui/internal/app"
	"verge-tui/internal/core"
	"verge-tui/internal/management"
	"verge-tui/internal/state"
	"verge-tui/internal/updater"
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
	requests := make(chan *updater.Request, 1)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	workers.Go(func() {
		u := updater.Updater{Store: s, Version: version}
		err := u.Run(ctx, func(ctx context.Context, request *updater.Request) error {
			if os.Getenv("INVOCATION_ID") != "" {
				return u.Launch(ctx, request)
			}
			select {
			case requests <- request:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "Automatic updates:", err)
		}
	})
	return watchManaged(ctx, m, &mu, serverDone, requests)
}

func watchManaged(ctx context.Context, m *core.Manager, mu *sync.RWMutex, serverDone <-chan error, updates ...<-chan *updater.Request) error {
	var requests <-chan *updater.Request
	if len(updates) > 0 {
		requests = updates[0]
	}
	for {
		mu.RLock()
		done := m.Done()
		mu.RUnlock()
		select {
		case <-ctx.Done():
			return nil
		case request := <-requests:
			return request
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
