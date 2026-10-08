package tui

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
)

func TestWheelPacketsWithHeldButtonsCannotClick(t *testing.T) {
	for _, wheel := range []tcell.ButtonMask{tcell.WheelUp, tcell.WheelDown, tcell.WheelLeft, tcell.WheelRight} {
		m := New(fixture())
		frame := m.Render(60, 18)
		m.FocusNext(1)
		var target Hit
		for _, hit := range frame.Hits {
			if hit.Action.Kind == "navigate" && hit.Action.ID == "2" {
				target = hit
				break
			}
		}
		for range 3 {
			a := m.Handle(tcell.NewEventMouse(target.Rect.X, target.Rect.Y, wheel|tcell.Button1, tcell.ModNone))
			if a != nil || m.Focus != "" || m.Tab != 0 || !m.MouseDown {
				t.Fatal("wheel moved focus, activated a control or lost the held button state")
			}
			m.Render(60, 18)
		}
		// A subsequent mouse motion with the same held button is not a fresh
		// click. Releasing and deliberately clicking restores normal operation.
		m.Handle(tcell.NewEventMouse(target.Rect.X, target.Rect.Y, tcell.Button1, tcell.ModNone))
		if m.Focus != "" || m.Tab != 0 {
			t.Fatal("mouse motion after a wheel packet restored focus")
		}
		m.Handle(tcell.NewEventMouse(target.Rect.X, target.Rect.Y, tcell.ButtonNone, tcell.ModNone))
		m.Handle(tcell.NewEventMouse(target.Rect.X, target.Rect.Y, tcell.Button1, tcell.ModNone))
		if m.Tab != 2 {
			t.Fatal("a deliberate click no longer activates the control")
		}
	}
}

func TestWheelInHelpDoesNotRestoreHiddenInputFocus(t *testing.T) {
	m := New(fixture())
	m.newProfile()
	m.Focus = Key(app.Action{Kind: "input", ID: "name"})
	m.help()
	frame := m.Render(60, 18)
	var close Hit
	for _, hit := range frame.Hits {
		if hit.Action.Kind == "close" {
			close = hit
		}
	}
	m.Handle(tcell.NewEventMouse(close.Rect.X, close.Rect.Y, tcell.WheelRight|tcell.Button1, tcell.ModNone))
	if m.Modal == nil || m.Modal.Kind != "help" || m.Focus != "" {
		t.Fatal("horizontal wheel dismissed help or moved focus to a control")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
	m.Render(60, 18)
	if m.Modal == nil || m.Modal.Kind != "profile-new" || m.Focus != "" || m.Input() != nil {
		t.Fatal("closing help restored input focus cleared by the wheel")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
	if m.Focus == "" {
		t.Fatal("Tab could not restore keyboard navigation after scrolling")
	}
}
