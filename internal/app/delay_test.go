package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/state"
)

func delayBackend(t *testing.T, names []string, handler http.HandlerFunc) *Backend {
	t.Helper()
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+c.Secret {
			t.Error("delay API request lost authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body any
		switch r.URL.Path {
		case "/version":
			body = map[string]string{"version": "test"}
		case "/configs":
			body = controller.Config{Mode: "rule"}
		case "/proxies":
			proxies := map[string]controller.Proxy{"Proxy": {Name: "Proxy", All: names, TestURL: "https://example.com/group"}}
			for _, name := range names {
				proxies[name] = controller.Proxy{Name: name, History: []controller.History{{Delay: 99}}}
			}
			body = map[string]any{"proxies": proxies}
		case "/rules":
			body = map[string]any{"rules": []any{}}
		default:
			handler(w, r)
			return
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *state.Settings) error { c.APIPort = n; return nil }); err != nil {
		t.Fatal(err)
	}
	return New(s, "test")
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delay worker")
		var zero T
		return zero
	}
}

func TestDelayBoundedConcurrencyAndIncrementalResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fast, slow := make(chan struct{}), make(chan struct{})
	var releaseFast, releaseSlow sync.Once
	defer releaseFast.Do(func() { close(fast) })
	defer releaseSlow.Do(func() { close(slow) })
	started := make(chan string, 8)
	var active, peak, calls atomic.Int32
	names := []string{"n0", "n1", "n2", "n3", "n4", "n5", "n6", "节点 / 7", "n0"}
	b := delayBackend(t, names, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/proxies/"), "/delay")
		if r.URL.Query().Get("url") != "https://example.com/test?q=1&x=2" || r.URL.Query().Get("timeout") != strconv.Itoa(controller.DelayTimeoutMillis) {
			t.Error("delay test URL or timeout was changed")
		}
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		started <- name
		gate := slow
		if name == "n0" {
			gate = fast
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		if name == "n3" {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		delay := 123
		if name == "n4" {
			delay = 0
		}
		json.NewEncoder(w).Encode(map[string]int{"delay": delay})
	})
	progress := make(chan DelayResult, 8)
	finished := make(chan error, 1)
	go func() {
		finished <- b.TestDelay(ctx, Action{Kind: "delay", ID: "Proxy", Value: "https://example.com/test?q=1&x=2"}, func(update DelayResult) { progress <- update })
	}()
	for range DelayConcurrency {
		receive(t, started)
	}
	if peak.Load() != DelayConcurrency {
		t.Fatal("group tests are not concurrent")
	}
	releaseFast.Do(func() { close(fast) })
	first := receive(t, progress)
	if first.Name != "n0" || first.Delay != 123 || first.Done != 1 || first.Total != 8 {
		t.Fatalf("first node did not produce an immediate result: %+v", first)
	}
	select {
	case err := <-finished:
		t.Fatalf("test completed before remaining nodes: %v", err)
	default:
	}
	releaseSlow.Do(func() { close(slow) })
	seen := map[string]int{first.Name: first.Delay}
	for done := 2; done <= 8; done++ {
		update := receive(t, progress)
		if _, duplicate := seen[update.Name]; duplicate || update.Done != done || update.Total != 8 {
			t.Fatalf("duplicate or unordered progress: %+v", update)
		}
		seen[update.Name] = update.Delay
	}
	if err := receive(t, finished); err == nil || !strings.Contains(err.Error(), "2 of 8") {
		t.Fatalf("failed nodes were not reported: %v", err)
	}
	if seen["n3"] != DelayError || seen["n4"] != 0 || calls.Load() != 8 || peak.Load() > DelayConcurrency {
		t.Fatalf("wrong failures or unbounded requests: results=%v calls=%d peak=%d", seen, calls.Load(), peak.Load())
	}
}

func TestDelayCancellationStopsWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, stopped := make(chan struct{}, 4), make(chan struct{}, 4)
	b := delayBackend(t, []string{"a", "b", "c", "d", "e", "f"}, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
		stopped <- struct{}{}
	})
	finished := make(chan error, 1)
	var updates atomic.Int32
	go func() {
		finished <- b.TestDelay(ctx, Action{ID: "Proxy"}, func(DelayResult) { updates.Add(1) })
	}()
	for range DelayConcurrency {
		receive(t, started)
	}
	cancel()
	if err := receive(t, finished); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	for range DelayConcurrency {
		receive(t, stopped)
	}
	if updates.Load() != 0 || len(started) != 0 {
		t.Fatal("cancellation emitted failures or started queued nodes")
	}
}

func TestSingleDelayUsesGroupURLAndReportsResult(t *testing.T) {
	b := delayBackend(t, []string{"Node"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != "https://example.com/group" {
			t.Error("single-node test ignored the group's URL")
		}
		json.NewEncoder(w).Encode(map[string]int{"delay": 321})
	})
	var results []DelayResult
	err := b.TestDelay(context.Background(), Action{ID: "Proxy", Node: "Node"}, func(r DelayResult) { results = append(results, r) })
	if err != nil || len(results) != 1 || results[0] != (DelayResult{"Node", 321, 1, 1}) {
		t.Fatalf("single test result missing: %v, %v", results, err)
	}
}
