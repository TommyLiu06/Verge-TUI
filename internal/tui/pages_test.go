package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func pageFixture() *Model {
	m := New(fixture())
	m.Connections = controller.Connections{DownloadTotal: 2048, Connections: []controller.Connection{
		{ID: "a", Start: time.Now(), Chains: []string{"Node", "Proxy"}, Rule: "DomainSuffix", RulePayload: "example.com", Download: 1000, Metadata: controller.Metadata{Host: "example.com", DestinationPort: "443", Process: "curl", Type: "HTTP", Network: "tcp"}},
		{ID: "b", Start: time.Now().Add(-time.Second), Metadata: controller.Metadata{Host: "other.com", DestinationPort: "80"}},
	}}
	m.Rules = controller.Rules{Rules: []controller.Rule{{Type: "DomainSuffix", Payload: "example.com", Proxy: "Proxy"}, {Type: "Match", Proxy: "DIRECT"}}, Providers: map[string]controller.RuleProvider{"Example": {Behavior: "domain", VehicleType: "HTTP", RuleCount: 123, UpdatedAt: time.Now()}}}
	m.Logs = []controller.Log{{Time: "12:00:01", Type: "info", Payload: "Connection accepted"}, {Time: "12:00:02", Type: "warning", Payload: "Provider timeout"}}
	return m
}
func activateVisible(t *testing.T, m *Model, a app.Action) *app.Action {
	t.Helper()
	m.Focus = Key(a)
	m.revealFocus()
	f := m.Render(120, 42)
	for _, hit := range f.Hits {
		if hit.Key == Key(a) {
			m.MouseDown = false
			return m.Handle(tcell.NewEventMouse(hit.Rect.X, hit.Rect.Y, tcell.Button1, tcell.ModNone))
		}
	}
	t.Fatalf("control not visible: %v", a)
	return nil
}
func TestNavigationAndNewPageControls(t *testing.T) {
	m := pageFixture()
	for _, size := range [][2]int{{60, 18}, {80, 25}, {120, 42}} {
		f := m.Render(size[0], size[1])
		count := 0
		for _, hit := range f.Hits {
			if hit.Action.Kind == "navigate" && hit.Rect.X == 1 {
				count++
				if hit.Rect.Y+hit.Rect.H > size[1]-7 && size[1] >= 25 {
					t.Fatal("navigation overlaps core status")
				}
			}
		}
		if count != 7 {
			t.Fatal("missing page navigation", size, count)
		}
	}
	for i, page := range navigation {
		m.Handle(tcell.NewEventKey(tcell.KeyRune, string(rune('1'+i)), tcell.ModNone))
		if m.Tab != page.Tab {
			t.Fatal("incorrect shortcut", i, m.Tab)
		}
		if !strings.Contains(m.Render(120, 42).Plain(), page.Name) {
			t.Fatal("missing page title")
		}
	}
	m.Tab = 4
	text := m.Render(120, 42).Plain()
	for _, want := range []string{"Active (2)", "Closed (0)", "example.com:443", "Proxy / Node", "DomainSuffix(example.com)", "Close All"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing connection field", want)
		}
	}
	activateVisible(t, m, app.Action{Kind: "connection-detail", ID: "a"})
	if !strings.Contains(m.Render(120, 42).Plain(), "Close Connection") {
		t.Fatal("details cannot close connection")
	}
	if a := activateVisible(t, m, app.Action{Kind: "connection-close", ID: "a"}); a == nil || a.ID != "a" {
		t.Fatal(a)
	}
	m.Tab = 5
	m.Modal = nil
	activateVisible(t, m, app.Action{Kind: "rule-providers"})
	activateVisible(t, m, app.Action{Kind: "rule-provider", ID: "Example"})
	if !strings.Contains(m.Render(120, 42).Plain(), "123") {
		t.Fatal("missing provider count")
	}
	if a := activateVisible(t, m, app.Action{Kind: "provider-update", ID: "Example"}); a == nil {
		t.Fatal("update did not reach backend")
	}
	m.Tab = 6
	m.Modal = nil
	activateVisible(t, m, app.Action{Kind: "logs-level"})
	activateVisible(t, m, app.Action{Kind: "page-option", ID: "logs-level", Value: "Warn"})
	text = m.Render(120, 42).Plain()
	if !strings.Contains(text, "Provider timeout") || strings.Contains(text, "Connection accepted") {
		t.Fatal("log severity filter failed")
	}
	activateVisible(t, m, app.Action{Kind: "logs-pause"})
	if !m.LogsPaused {
		t.Fatal("pause failed")
	}
	activateVisible(t, m, app.Action{Kind: "logs-clear"})
	if len(m.Logs) != 0 {
		t.Fatal("clear failed")
	}
}
func TestConnectionSpeedsHistorySearchAndLogBounds(t *testing.T) {
	m := pageFixture()
	m.ConnectionTime = time.Unix(10, 0)
	next := controller.Connections{Connections: []controller.Connection{m.Connections.Connections[0]}}
	next.Connections[0].Download = 3000
	next.Connections[0].Upload = 400
	m.ApplyConnections(next, time.Unix(12, 0))
	if m.Connections.Connections[0].DownloadSpeed != 1000 || m.Connections.Connections[0].UploadSpeed != 200 || len(m.ClosedConnections) != 1 {
		t.Fatal("incorrect counters or closed history")
	}
	m.Search[4] = "CURL"
	if len(m.connectionRows()) != 1 {
		t.Fatal("process search failed")
	}
	m.Search[4] = "other"
	if len(m.connectionRows()) != 0 {
		t.Fatal("unexpected search result")
	}
	m.Tab = 6
	m.MaxScroll = 20
	m.Scroll[6] = 0
	entries := make([]controller.Log, 1005)
	for i := range entries {
		entries[i] = controller.Log{Payload: "sample"}
	}
	m.AppendLogs(entries)
	if len(m.Logs) != 1000 || m.Scroll[6] != 0 {
		t.Fatal("unbounded logs or scroll away ignored")
	}
	m.Scroll[6] = 20
	m.AppendLogs([]controller.Log{{Payload: "new"}})
	m.Render(120, 42)
	if m.Scroll[6] != m.MaxScroll || m.Focus != "" {
		t.Fatal("tail follow moved focus or did not follow")
	}
	m.Focus = "focused"
	m.Handle(tcell.NewEventMouse(50, 14, tcell.WheelUp, tcell.ModNone))
	before := m.Scroll[6]
	m.Render(120, 42)
	if m.Focus != "" || m.Scroll[6] != before {
		t.Fatal("redraw undid wheel scrolling")
	}
}
func TestRulesSearchRetainsOriginalLineNumbers(t *testing.T) {
	m := pageFixture()
	m.Tab = 5
	m.Rules.Rules = append([]controller.Rule{{Payload: "hidden", Type: "Domain"}}, m.Rules.Rules...)
	m.Search[5] = "EXAMPLE.COM"
	text := m.Render(120, 42).Plain()
	if !strings.Contains(text, "2     example.com") || strings.Contains(text, "hidden") {
		t.Fatal(text)
	}
}
func TestPageWatchCancellationAndSingleRulesFetch(t *testing.T) {
	s, err := state.Open(testutil.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	testutil.CoreServer(t, s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/logs" {
			w.(http.Flusher).Flush()
			started <- struct{}{}
			<-r.Context().Done()
			stopped <- struct{}{}
			return
		}
		json.NewEncoder(w).Encode(controller.Rules{Rules: []controller.Rule{{Type: "Match", Proxy: "DIRECT"}}})
	}))
	b := app.New(s, "test")
	updates := make(chan pageUpdate, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchPage(ctx, b, 6, 1, updates); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("log stream did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not cancel")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stream left open")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	watchPage(ctx, b, 5, 2, updates)
	if u := <-updates; u.Generation != 2 || u.Err != nil || len(u.Data.Rules.Rules) != 1 {
		t.Fatal(u)
	}
}
