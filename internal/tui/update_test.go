package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/updater"
)

func updateModel() *Model {
	v := fixture()
	v.Version = "0.1.18"
	m := New(v)
	m.AppUpdate = updater.Status{Supported: true, CurrentVersion: v.Version, Release: &updater.Release{Version: "0.1.19", Notes: "Update notes", Size: 100}, Phase: "available"}
	m.updateModal()
	return m
}

func TestUpdateDialogPoliciesPrecedeVersionAndUseKeyboardLists(t *testing.T) {
	m := updateModel()
	m.AppUpdate.Release.Notes = "Update notes\n\n* First\n* Second"
	m.updateModal()
	frame := m.Render(110, 38)
	plain := frame.Plain()
	if strings.Index(plain, "Auto Check Updates") < 0 || strings.Index(plain, "Auto Check Updates") > strings.Index(plain, "Auto Download Updates") || strings.Index(plain, "Auto Download Updates") > strings.Index(plain, "Auto Update") || strings.Index(plain, "Auto Update") > strings.Index(plain, "Current Version") || !strings.Contains(plain, "Update notes") {
		t.Fatalf("update dialog layout differs: %s", plain)
	}
	if !strings.Contains(strings.Join(m.Modal.Body, "\n"), "* First\n* Second") {
		t.Fatal("release-note paragraphs were flattened")
	}
	a := app.Action{Kind: "update-policy-list", ID: "check"}
	m.Focus = Key(a)
	m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if m.Modal.Kind != "selection-list" || !m.Modal.List || m.Modal.Choices[0].Label != "Manual" {
		t.Fatal("policy selection did not open a list")
	}
	m.Render(110, 38)
	m.Handle(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	selected := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if selected == nil || selected.Kind != "update-options" || selected.Fields["check-minutes"] != "15" {
		t.Fatalf("keyboard could not select an interval: %#v", selected)
	}
	m.completeUpdate(result{Action: *selected, Page: &app.PageData{Update: &updater.Status{Supported: true, CurrentVersion: m.View.Version, Options: updater.Options{CheckMinutes: 15}}}})
	if m.Modal.Kind != "app-update" || !strings.Contains(m.Modal.Header[0].Label, "15") {
		t.Fatal("saved interval was not displayed")
	}
	m.Activate(app.Action{Kind: "update-policy-list", ID: "download"})
	if m.Modal.Kind != "selection-list" || len(m.Modal.Choices) != 2 || m.Modal.Choices[1].Action.Fields["auto-download"] != "true" {
		t.Fatal("automatic download did not offer an On/Off list")
	}
	saved := m.Activate(m.Modal.Choices[1].Action)
	if saved == nil || saved.Fields["auto-download"] != "true" {
		t.Fatal("automatic download cannot be enabled after automatic checks")
	}
	m.AppUpdate.Options.AutoDownload = true
	m.updateModal()
	m.Activate(app.Action{Kind: "update-policy-list", ID: "idle"})
	if !strings.Contains(m.Modal.Choices[1].Label, "without active connections") {
		t.Fatal("automatic update choices did not describe continuous idle time")
	}
	m.Activate(app.Action{Kind: "update-custom", ID: "idle"})
	m.Modal.Fields[0].Text = "12"
	action := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
	if action == nil || action.Fields["idle-minutes"] != "12" {
		t.Fatal("custom idle time was not saved")
	}
}

func TestDownloadAppliesOnlyWhileTheUpdateDialogRemainsOpen(t *testing.T) {
	for _, closeDialog := range []bool{false, true} {
		m := updateModel()
		a := m.Activate(app.Action{Kind: "update-install"})
		if a == nil || a.Kind != "app-update" || a.ID != "download" {
			t.Fatal("Update did not start a download")
		}
		m.beginTask(*a)
		if closeDialog {
			m.Handle(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
		}
		s := m.AppUpdate
		s.Ready, s.Phase = true, "ready"
		m.completeUpdate(result{Action: *a, Page: &app.PageData{Update: &s}})
		if (m.UpdateRequest != nil) == closeDialog || m.Quit == closeDialog {
			t.Fatal("update continuation ignored whether its dialog was closed")
		}
	}
	m := updateModel()
	m.completeUpdate(result{Action: app.Action{Kind: "app-update", ID: "check"}, Err: errors.New("network failure")})
	if m.Modal.Kind != "app-update" || !strings.Contains(m.Render(100, 38).Plain(), "network failure") {
		t.Fatal("update failure replaced the dialog or was hidden")
	}
}

func TestSystemPolicyChangesRequireAdministratorFlow(t *testing.T) {
	m := updateModel()
	m.UpdateNeedsAdmin = true
	m.AppUpdate.Options = updater.Options{CheckMinutes: 60, AutoDownload: true}
	if a := m.Activate(app.Action{Kind: "update-options", Fields: map[string]string{"idle-minutes": "20"}}); a != nil || m.UpdateRequest == nil || m.UpdateRequest.Configure == nil || m.UpdateRequest.Configure.IdleMinutes != 20 {
		t.Fatal("system policy was sent to the unprivileged shared action endpoint")
	}
	m = updateModel()
	m.UpdateNeedsAdmin = true
	m.AppUpdate.Options = updater.Options{CheckMinutes: 60, AutoDownload: true}
	m.Activate(app.Action{Kind: "update-custom", ID: "idle"})
	m.Modal.Fields[0].Text = "17"
	if a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone)); a != nil || m.UpdateRequest == nil || m.UpdateRequest.Configure.IdleMinutes != 17 {
		t.Fatal("Ctrl+S bypassed administrator authorization for custom policies")
	}
}

func TestBackgroundInstalledVersionOffersUnprivilegedTUIRestart(t *testing.T) {
	m := updateModel()
	m.AppUpdate.CurrentVersion = "0.1.19"
	m.AppUpdate.Ready, m.AppUpdate.Phase = false, "latest"
	m.updateModal()
	if m.AppUpdate.Available() {
		t.Fatal("an already installed background update was offered for installation again")
	}
	m.Activate(app.Action{Kind: "update-restart"})
	if m.UpdateRequest == nil || !m.UpdateRequest.RestartOnly || !m.Quit {
		t.Fatal("old TUI cannot restart into a background-installed version")
	}
}

func TestUpdateButtonRemainsVisibleWhenReleaseNotesScroll(t *testing.T) {
	m := updateModel()
	m.AppUpdate.Release.Notes = strings.Repeat("A release-note paragraph\n", 100)
	m.updateModal()
	frame := m.Render(100, 26)
	for _, hit := range frame.Hits {
		if hit.Action.Kind == "update-install" && hit.Rect.Y > 0 && hit.Rect.Y < frame.H {
			return
		}
	}
	t.Fatal("Update scrolled off screen with the release notes")
}

func TestDependentUpdatePoliciesAreDisabledForMouseAndKeyboard(t *testing.T) {
	m := updateModel()
	frame := m.Render(110, 38)
	for _, hit := range append(frame.Hits, m.focusTargets()...) {
		if hit.Action.Kind == "update-policy-list" && hit.Action.ID != "check" {
			t.Fatal("a disabled child policy was clickable or focusable")
		}
	}
	m.Activate(app.Action{Kind: "update-policy-list", ID: "download"})
	if m.Modal.Kind != "app-update" || !strings.Contains(m.Message, "Enable Auto Check Updates") {
		t.Fatal("disabled automatic download could open its selector")
	}
	m.AppUpdate.Options.CheckMinutes = 60
	m.updateModal()
	m.Activate(app.Action{Kind: "update-policy-list", ID: "idle"})
	if m.Modal.Kind != "app-update" || !strings.Contains(m.Message, "Enable Auto Download Updates") {
		t.Fatal("automatic checks alone enabled automatic installation")
	}
}

func TestAvailableUpdateIsOnlyIndicatedInSettings(t *testing.T) {
	m := updateModel()
	m.Modal = nil
	m.AppUpdate.Release.Version = "99.88.77"
	s := m.AppUpdate
	m.completeUpdate(result{Action: app.Action{Kind: "app-update", ID: "status"}, Page: &app.PageData{Update: &s}})
	if m.Modal != nil {
		t.Fatal("background metadata opened a modal")
	}
	m.Tab = 0
	if plain := m.Render(120, 60).Plain(); strings.Contains(plain, "99.88.77") || strings.Contains(plain, "New Version") {
		t.Fatal("Home advertised the available update")
	}
	if m.updatePollingInterval() != 0 {
		t.Fatal("Home started update-interface polling")
	}
	m.Tab = 3
	if plain := m.Render(120, 60).Plain(); !strings.Contains(plain, "Check for Updates") || !strings.Contains(plain, "New Version") {
		t.Fatal("Settings did not indicate the available update")
	}
	if m.updatePollingInterval() != 5*time.Second {
		t.Fatal("Settings does not refresh cached update state")
	}
}

func TestManualUpdateAlwaysRechecksAPreparedArchive(t *testing.T) {
	m := updateModel()
	m.AppUpdate.Ready = true
	a := m.Activate(app.Action{Kind: "update-install"})
	if a == nil || a.Kind != "app-update" || a.ID != "download" || m.UpdateRequest != nil || m.Quit {
		t.Fatal("a prepared archive bypassed the latest-release check")
	}
}

type downloadTaskFixture struct {
	*taskFixture
	downloadStarted chan struct{}
	downloadGate    chan struct{}
}

func (b *downloadTaskFixture) ReadPage(ctx context.Context, kind, id string) (app.PageData, error) {
	if kind == "app-update" && id == "download" {
		close(b.downloadStarted)
		select {
		case <-b.downloadGate:
		case <-ctx.Done():
			return app.PageData{}, ctx.Err()
		}
	}
	return app.PageData{Update: &updater.Status{CurrentVersion: "0.1.18", Phase: "downloading"}}, nil
}

func TestAppDownloadDoesNotBlockProxyPreferencesOrProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &downloadTaskFixture{taskFixture: &taskFixture{view: fixture(), executed: make(chan app.Action, 4)}, downloadStarted: make(chan struct{}), downloadGate: make(chan struct{})}
	r := newTaskRunner(ctx, b)
	defer func() { cancel(); r.workers.Wait() }()
	r.submit(app.Action{Kind: "app-update", ID: "download"})
	awaitTask(t, b.downloadStarted)
	r.submit(app.Action{Kind: "mode", Value: "direct"})
	r.submit(app.Action{Kind: "preferences-save", ID: "theme", Fields: map[string]string{"theme": "dark"}})
	r.submit(app.Action{Kind: "app-update", ID: "status"})
	r.submit(app.Action{Kind: "update-options", Fields: map[string]string{"idle-minutes": "0"}})
	seen := map[string]bool{}
	for len(seen) < 4 {
		done := awaitTask(t, r.results)
		seen[done.Action.Kind+"/"+done.Action.ID] = true
		if done.Action.Kind == "app-update" && done.Action.ID == "download" {
			t.Fatal("download completed before releasing its fixture")
		}
	}
	if !seen["mode/"] || !seen["preferences-save/theme"] || !seen["app-update/status"] || !seen["update-options/"] {
		t.Fatalf("download blocked independent tasks: %#v", seen)
	}
}
