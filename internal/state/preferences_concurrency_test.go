package state_test

import (
	"sync"
	"testing"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestConcurrentPreferenceFieldsAreBothPreserved(t *testing.T) {
	root := testutil.Dir(t)
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for _, update := range []func(*state.Preferences) error{
		func(p *state.Preferences) error { p.Theme = "dark"; return nil },
		func(p *state.Preferences) error { p.SystemProxy.Host = "::1"; return nil },
	} {
		workers.Go(func() { <-start; errors <- state.UpdatePreferences(root, update) })
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := state.ReadPreferences(root)
	if err != nil || p.Theme != "dark" || p.SystemProxy.Host != "::1" {
		t.Fatal("independent changes overwrote each other", p, err)
	}
}
