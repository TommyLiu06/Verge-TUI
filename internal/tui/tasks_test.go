package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/state"
)

type taskFixture struct {
	mu         sync.Mutex
	view       app.View
	started    chan struct{}
	release    chan struct{}
	finished   chan struct{}
	executed   chan app.Action
	normalGate <-chan struct{}
}

func (b *taskFixture) Execute(ctx context.Context, a app.Action) error {
	b.executed <- a
	if !privatePreference(a) && b.normalGate != nil {
		select {
		case <-b.normalGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if a.Kind == "mode" {
		b.view.Settings.Mode = a.Value
	}
	if privatePreference(a) {
		b.view.Preferences.Theme = a.Fields["theme"]
	}
	return nil
}

func TestPrivatePreferencesDoNotWaitForLongCommandsOrRestoreStaleTheme(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{})
	b := &taskFixture{view: fixture(), executed: make(chan app.Action, 4), normalGate: gate}
	r := newTaskRunner(ctx, b)
	defer func() { cancel(); r.workers.Wait() }()
	m := New(fixture())
	command := app.Action{Kind: "geo-update"}
	m.beginTask(command)
	r.submit(command)
	awaitTask(t, b.executed)
	preferences := app.Action{Kind: "preferences-save", ID: "theme", Fields: map[string]string{"theme": "dark"}}
	m.beginTask(preferences)
	r.submit(preferences)
	awaitTask(t, b.executed)
	done := awaitTask(t, r.results)
	if done.Preferences == nil {
		t.Fatal("theme waited for the unrelated command")
	}
	m.completeTask(done)
	if m.View.Preferences.Theme != "dark" || !m.Busy {
		t.Fatal("theme was not applied while the command remained pending")
	}
	close(gate)
	done = awaitTask(t, r.results)
	m.completeTask(done)
	if m.Busy {
		t.Fatal("finished command remained pending")
	}
	m.completeTask(result{Action: app.Action{Kind: "refresh"}, View: fixture(), PreferencesEpoch: 0})
	if m.View.Preferences.Theme != "dark" {
		t.Fatal("an older view restored the previous theme")
	}
}
func (b *taskFixture) TestDelay(ctx context.Context, a app.Action, progress func(app.DelayResult)) error {
	if b.finished != nil {
		defer close(b.finished)
	}
	close(b.started)
	progress(app.DelayResult{Name: "Node B", Delay: 260, Done: 1, Total: 2})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
	}
	progress(app.DelayResult{Name: "节点 A", Delay: 300, Done: 2, Total: 2})
	return nil
}
func (b *taskFixture) Load(context.Context) (app.View, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.view, nil
}
func (b *taskFixture) ReadPreferences() (state.Preferences, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.view.Preferences, nil
}
func (b *taskFixture) ReadPage(context.Context, string, string) (app.PageData, error) {
	return app.PageData{}, nil
}
func (b *taskFixture) Inspect(context.Context, string) ([]string, error) {
	return []string{"configuration"}, nil
}
func awaitTask[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("task blocked")
		var zero T
		return zero
	}
}
func TestDelayDoesNotBlockCommandsAndCompletionKeepsCurrentView(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &taskFixture{view: fixture(), started: make(chan struct{}), release: make(chan struct{}), executed: make(chan app.Action, 4)}
	r := newTaskRunner(ctx, b)
	defer func() { cancel(); r.workers.Wait() }()
	m := New(fixture())
	a := app.Action{Kind: "delay", ID: "Proxy"}
	m.beginTask(a)
	r.submit(a)
	awaitTask(t, b.started)
	first := awaitTask(t, r.results)
	if first.Delay == nil {
		t.Fatal("first node did not report progress")
	}
	m.ApplyDelay(*first.Delay)
	command := m.Activate(app.Action{Kind: "mode", Value: "direct"})
	if command == nil || !m.beginTask(*command) {
		t.Fatal("delay blocked mode selection")
	}
	r.submit(*command)
	if got := awaitTask(t, b.executed); got.Kind != "mode" {
		t.Fatal(got)
	}
	done := awaitTask(t, r.results)
	m.Complete(done.View, done.Action, done.Err)
	if m.View.Settings.Mode != "direct" || !m.Busy || !m.DelayActive || delayValue(m.View.Snapshot.Proxies["Node B"]) != 260 {
		t.Fatal("mode completion lost active delay or results")
	}
	close(b.release)
	for m.DelayActive {
		done := awaitTask(t, r.results)
		if done.Delay != nil {
			m.ApplyDelay(*done.Delay)
		} else {
			m.Complete(done.View, done.Action, done.Err)
		}
	}
	if m.Busy || m.View.Settings.Mode != "direct" {
		t.Fatal("delay completion restored old settings or busy state")
	}
	// A concurrent view request may have finished before the last latency arrived.
	m.Complete(fixture(), app.Action{Kind: "refresh"}, nil)
	if delayValue(m.View.Snapshot.Proxies["节点 A"]) != 300 {
		t.Fatal("a stale normal snapshot overwrote a fresh measurement")
	}
}
func TestSettingsFormsAndEditorsWorkWhileOtherTasksArePending(t *testing.T) {
	v := fixture()
	v.Settings.InstallMode = "system"
	m := New(v)
	m.beginTask(app.Action{Kind: "delay", ID: "Proxy"})
	m.settingsForm("theme")
	m.Render(120, 42)
	a := m.Activate(app.Action{Kind: "preferences-save", ID: "theme", Value: "dark", Fields: map[string]string{"theme": "dark"}})
	if a == nil {
		t.Fatal("theme blocked during delay")
	}
	m.beginTask(*a)
	m.settingsForm("tun")
	m.Render(120, 42)
	a = m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
	if a == nil || a.Kind != "settings-save" {
		t.Fatal("form cannot save while tasks are pending")
	}
	m.Modal = nil
	m.URL = "https://example.com/new"
	if a := m.Activate(app.Action{Kind: "import"}); a == nil {
		t.Fatal("import was globally locked")
	}
	m = editorModel("proxies: []\nrules: []\n")
	m.beginTask(app.Action{Kind: "delay", ID: "Proxy"})
	if a := m.saveEditor(); a == nil {
		t.Fatal("editor cannot save while delay runs")
	}
}
func TestOnlyConflictingTasksAreBlockedAndBusyTracksAllTasks(t *testing.T) {
	m := New(fixture())
	delay := app.Action{Kind: "delay", ID: "Proxy"}
	m.beginTask(delay)
	if m.Activate(delay) != nil {
		t.Fatal("a duplicate test started")
	}
	mode := app.Action{Kind: "mode", Value: "global"}
	if !m.beginTask(mode) {
		t.Fatal("unrelated action blocked")
	}
	m.Complete(app.View{}, delay, errors.New("test failed"))
	if !m.Busy || m.Modal != nil {
		t.Fatal("finishing delay cleared another task or opened a popup")
	}
	m.Complete(fixture(), mode, nil)
	if m.Busy {
		t.Fatal("task state never cleared")
	}
	m.beginTask(app.Action{Kind: "profile-select", ID: "second"})
	if m.Activate(delay) != nil {
		t.Fatal("testing started during profile replacement")
	}
	m.cancelDelay()
	m.Complete(fixture(), app.Action{Kind: "profile-select", ID: "second"}, nil)
	if len(m.Latencies) != 0 {
		t.Fatal("profile replacement kept old results")
	}
}
func TestQueueBoundPreservesAnOpenForm(t *testing.T) {
	m := New(fixture())
	for i := 0; i < maxPendingTasks; i++ {
		m.beginTask(app.Action{Kind: "node-select", ID: "Proxy", Node: string(rune('a' + i))})
	}
	m.settingsForm("dns")
	modal := m.Modal
	if a := m.submit(); a != nil || m.Modal != modal {
		t.Fatal("full queue lost the form or accepted more work")
	}
}

func TestCancelDelayStopsRequestsAndPreservesOtherTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &taskFixture{view: fixture(), started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}), executed: make(chan app.Action, 4)}
	r := newTaskRunner(ctx, b)
	defer func() { cancel(); r.workers.Wait() }()
	m := New(fixture())
	delay := app.Action{Kind: "delay", ID: "Proxy"}
	m.beginTask(delay)
	r.submit(delay)
	awaitTask(t, b.started)
	awaitTask(t, r.results)
	change := app.Action{Kind: "profile-select", ID: "second"}
	m.beginTask(change)
	if !invalidatesDelay(change, m.View) {
		t.Fatal("profile switch did not invalidate the running test")
	}
	r.cancelDelay()
	m.cancelDelay()
	awaitTask(t, b.finished)
	if r.DelayID != 0 || m.DelayActive || !m.Busy {
		t.Fatal("cancellation lost the configuration task or kept the delay active")
	}
}

func TestPreferenceResultsPreserveConcurrentDesktopProxyChanges(t *testing.T) {
	m := New(fixture())
	old := m.View.Preferences
	next := fixture()
	next.Preferences.SystemProxy.Host = "::1"
	m.completeTask(result{Action: app.Action{Kind: "preferences-save", ID: "system-proxy"}, View: next})
	old.Theme = "dark"
	m.completeTask(result{Action: app.Action{Kind: "preferences-save", ID: "theme"}, Preferences: &old, PreferencesEpoch: 1})
	if m.View.Preferences.SystemProxy.Host != "::1" || m.View.Preferences.Theme != "dark" {
		t.Fatal("theme result reverted the desktop proxy setting")
	}
	next.Preferences.SystemProxy.Host = "127.0.0.2"
	m.completeTask(result{Action: app.Action{Kind: "preferences-save", ID: "system-proxy"}, View: next, PreferencesEpoch: 0})
	if m.View.Preferences.SystemProxy.Host != "127.0.0.2" || m.View.Preferences.Theme != "dark" {
		t.Fatal("desktop result lost its setting or restored an older theme")
	}
}
