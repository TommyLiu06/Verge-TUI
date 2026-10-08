package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"mihomo-tui/internal/app"
)

func TestProfileOptionsDeleteWithMouseAndKeyboard(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		m := New(fixture())
		m.Tab = 2
		click := func(kind, id string) *app.Action {
			t.Helper()
			frame := m.Render(60, 18)
			a := app.Action{Kind: kind, ID: id}
			if keyboard {
				m.Focus = Key(a)
				m.revealFocus()
				m.Render(60, 18)
				return m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
			}
			for _, hit := range frame.Hits {
				if hit.Key == Key(a) {
					m.Handle(tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone))
					return m.Handle(tcell.NewEventMouse(hit.Rect.X+1, hit.Rect.Y, tcell.Button1, tcell.ModNone))
				}
			}
			t.Fatalf("missing control %s", kind)
			return nil
		}
		if a := click("profile-menu", "first"); a != nil || m.Modal == nil || m.Modal.Title != "Profile Options" {
			t.Fatal("options selected the profile instead of opening a menu")
		}
		if a := click("profile-delete-confirm", "first"); a != nil || m.Modal == nil || m.Modal.Title != "Delete Profile" {
			t.Fatal("delete skipped confirmation")
		}
		if !strings.Contains(m.Render(60, 18).Plain(), "Delete this profile?") {
			t.Fatal("confirmation prompt hidden")
		}
		if keyboard {
			// The default focus is Cancel; Enter cannot delete accidentally.
			if a := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone)); a != nil || m.Modal != nil {
				t.Fatal("confirmation did not default to cancellation")
			}
			m.Activate(app.Action{Kind: "profile-delete-confirm", ID: "first"})
		}
		a := click("profile-delete", "first")
		if a == nil || a.Kind != "profile-delete" || a.ID != "first" {
			t.Fatal("confirmation did not delete the chosen profile")
		}
		if len(m.View.Settings.Profiles) != 2 {
			t.Fatal("UI removed profile before backend success")
		}
	}
}
