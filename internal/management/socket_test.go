package management

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mihomo-tui/internal/state"
)

func socketStore(t *testing.T) *state.Store {
	t.Helper()
	path, err := os.MkdirTemp("../../.local", "rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(path) })
	s, err := state.Open(filepath.Join(path, "data"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUnixSocketStreamsEachResultAndCloses(t *testing.T) {
	s := socketStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := make(chan struct{})
	server, err := Serve(ctx, s, Handlers{
		View:   func(context.Context) (any, error) { return map[string]string{"name": "shared"}, nil },
		Action: func(context.Context, json.RawMessage) error { return errors.New("service management requires root") },
		Delay: func(ctx context.Context, _ json.RawMessage, progress func(any)) error {
			progress(map[string]int{"delay": 123})
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
			progress(map[string]int{"delay": 0})
			return errors.New("one node timed out")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); server.Close() })
	client := NewClient(s)
	defer client.Close()
	var view map[string]string
	if err := client.Call(ctx, "GET", "/view", nil, &view); err != nil || view["name"] != "shared" {
		t.Fatal(view, err)
	}
	if err := client.Call(ctx, "POST", "/action", map[string]string{"Kind": "stop"}, nil); err == nil {
		t.Fatal("service action was accepted")
	}
	progress, finished := make(chan int, 2), make(chan error, 1)
	go func() {
		finished <- client.Delay(ctx, map[string]string{"Kind": "delay"}, func(data json.RawMessage) error {
			var result struct{ Delay int }
			err := json.Unmarshal(data, &result)
			progress <- result.Delay
			return err
		})
	}()
	select {
	case delay := <-progress:
		if delay != 123 {
			t.Fatal(delay)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first result buffered until the end")
	}
	select {
	case <-finished:
		t.Fatal("stream completed before remaining nodes")
	default:
	}
	close(gate)
	select {
	case delay := <-progress:
		if delay != 0 {
			t.Fatal(delay)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout result missing")
	}
	select {
	case err := <-finished:
		if err == nil || err.Error() != "one node timed out" {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not complete")
	}
	info, err := os.Stat(SocketPath(s))
	if err != nil || info.Mode().Perm() != 0666 {
		t.Fatal("socket does not permit ordinary clients")
	}
	cancel()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SocketPath(s)); !os.IsNotExist(err) {
		t.Fatal("closed server left its socket")
	}
}

func TestStreamingClientCancellationReleasesHandler(t *testing.T) {
	s := socketStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, stopped := make(chan struct{}), make(chan struct{})
	server, err := Serve(ctx, s, Handlers{Delay: func(ctx context.Context, _ json.RawMessage, _ func(any)) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := NewClient(s)
	defer client.Close()
	requestCtx, stopRequest := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { finished <- client.Delay(requestCtx, map[string]string{"Kind": "delay"}, nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	stopRequest()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled stream succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request stayed blocked")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("handler survived client cancellation")
	}
}
