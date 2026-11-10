package tui

import (
	"errors"
	"strings"
	"testing"

	"verge-tui/internal/app"
)

func updateStatusLine(m *Model) string {
	lines := strings.Split(strings.TrimSuffix(m.Render(120, 38).Plain(), "\n"), "\n")
	return lines[len(lines)-1]
}

func TestUpdateActionsSettleTheirStatusAfterCompletion(t *testing.T) {
	for _, action := range []struct {
		name    string
		action  app.Action
		message string
	}{
		{"check", app.Action{Kind: "app-update", ID: "check"}, "Ready"},
		{"download", app.Action{Kind: "app-update", ID: "download"}, "Ready"},
		{"options", app.Action{Kind: "update-options", Fields: map[string]string{"check-minutes": "60"}}, "Settings saved"},
	} {
		for _, failed := range []bool{false, true} {
			for _, closeDialog := range []bool{false, true} {
				name := action.name + "/success/open"
				if failed {
					name = action.name + "/failure/open"
				}
				if closeDialog {
					name = strings.TrimSuffix(name, "open") + "closed"
				}
				t.Run(name, func(t *testing.T) {
					m := updateModel()
					if !m.beginTask(action.action) || !m.Busy {
						t.Fatal("manual update action did not start visibly")
					}
					if closeDialog {
						m.Activate(app.Action{Kind: "close"})
					}
					status := m.AppUpdate
					status.Phase = "available"
					done := result{Action: action.action, Page: &app.PageData{Update: &status}}
					want := action.message
					if failed {
						done.Err = errors.New("update request failed")
						want = "Error: update request failed"
					}
					m.completeUpdate(done)
					if m.Busy || len(m.Tasks) != 0 || m.Message != want {
						t.Fatalf("finished update action left stale status: busy=%t tasks=%d message=%q, want %q", m.Busy, len(m.Tasks), m.Message, want)
					}
					line := updateStatusLine(m)
					if strings.Contains(line, "Checking for Updates...") || strings.Contains(line, "Downloading...") || strings.Contains(line, "Working") || !strings.Contains(line, want) {
						t.Fatalf("finished update action left a pending footer: %s", line)
					}
					if closeDialog && m.Modal != nil {
						t.Fatal("update completion reopened a closed dialog")
					}
				})
			}
		}
	}
}

func TestUpdateStatusPollingLeavesFooterUnchanged(t *testing.T) {
	for _, dialog := range []bool{false, true} {
		name := "settings"
		if dialog {
			name = "dialog"
		}
		t.Run(name, func(t *testing.T) {
			m := updateModel()
			m.Tab, m.Message = 3, "Copied"
			if !dialog {
				m.Modal = nil
			}
			before := updateStatusLine(m)
			for _, failed := range []bool{false, true, false} {
				a := app.Action{Kind: "app-update", ID: "status"}
				if !m.beginTask(a) || len(m.Tasks) != 1 {
					t.Fatal("status polling was not registered for task tracking")
				}
				if m.Busy || m.Message != "Copied" || updateStatusLine(m) != before {
					t.Fatal("starting a status poll changed the footer")
				}
				status := m.AppUpdate
				done := result{Action: a, Page: &app.PageData{Update: &status}}
				if failed {
					done.Err = errors.New("status unavailable")
				}
				m.completeUpdate(done)
				if m.Busy || len(m.Tasks) != 0 || m.Message != "Copied" || updateStatusLine(m) != before {
					t.Fatal("completing a status poll changed the footer")
				}
			}
		})
	}
}

func TestUpdateStatusPollingKeepsForegroundTaskStatus(t *testing.T) {
	for _, foreground := range []app.Action{
		{Kind: "mode", Value: "global"},
		{Kind: "delay", ID: "Proxy"},
	} {
		for _, pollFinishesFirst := range []bool{false, true} {
			name := foreground.Kind + "/foreground-finishes-first"
			if pollFinishesFirst {
				name = foreground.Kind + "/poll-finishes-first"
			}
			t.Run(name, func(t *testing.T) {
				m := updateModel()
				poll := app.Action{Kind: "app-update", ID: "status"}
				if !m.beginTask(foreground) {
					t.Fatal("foreground task did not start")
				}
				message, before := m.Message, updateStatusLine(m)
				if !m.beginTask(poll) || !m.Busy || m.Message != message || updateStatusLine(m) != before {
					t.Fatal("status polling hid or replaced a foreground task")
				}
				status := m.AppUpdate
				if pollFinishesFirst {
					m.completeUpdate(result{Action: poll, Page: &app.PageData{Update: &status}})
					if !m.Busy || m.Message != message || updateStatusLine(m) != before {
						t.Fatal("completed polling cleared the running foreground task")
					}
				}
				m.Complete(fixture(), foreground, nil)
				if m.Busy {
					t.Fatal("background polling kept the footer busy after the foreground task finished")
				}
				if !pollFinishesFirst {
					message, before = m.Message, updateStatusLine(m)
					m.completeUpdate(result{Action: poll, Page: &app.PageData{Update: &status}})
					if m.Message != message || updateStatusLine(m) != before {
						t.Fatal("polling completion overwrote the foreground result")
					}
				}
				if m.Busy || len(m.Tasks) != 0 {
					t.Fatal("completed tasks remained active")
				}
			})
		}
	}
}

func TestUpdateCompletionDoesNotReplaceANewerTaskMessage(t *testing.T) {
	m := updateModel()
	check := app.Action{Kind: "app-update", ID: "check"}
	other := app.Action{Kind: "mode", Value: "global"}
	if !m.beginTask(check) || !m.beginTask(other) {
		t.Fatal("independent tasks did not start")
	}
	message := m.Message
	status := m.AppUpdate
	m.completeUpdate(result{Action: check, Page: &app.PageData{Update: &status}, Err: errors.New("check failed")})
	if !m.Busy || m.Message != message {
		t.Fatal("update completion overwrote a newer foreground task's message")
	}
	m.Complete(fixture(), other, nil)
	if m.Busy || len(m.Tasks) != 0 {
		t.Fatal("finished foreground tasks remained active")
	}
}
