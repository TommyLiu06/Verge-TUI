package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

func editorModel(text string) *Model {
	m := New(fixture())
	m.Tab = 2
	m.showProfileDocument("profile-file", &app.ProfileDocument{Profile: state.Profile{ID: "first", Name: "Example"}, Data: []byte(text), Revision: "original"})
	m.Render(120, 42)
	return m
}
func keyEditor(m *Model, key tcell.Key, text string) *app.Action {
	return m.Handle(tcell.NewEventKey(key, text, tcell.ModNone))
}
func TestEditorMultilinePasteUndoRedoAndSave(t *testing.T) {
	m := editorModel("")
	keyEditor(m, tcell.KeyRune, "proxies: []")
	keyEditor(m, tcell.KeyEnter, "")
	keyEditor(m, tcell.KeyRune, "rules: []")
	keyEditor(m, tcell.KeyCtrlZ, "")
	if strings.Join(m.Editor.Lines, "\n") != "proxies: []\n" {
		t.Fatal("undo lost earlier edits")
	}
	keyEditor(m, tcell.KeyCtrlY, "")
	m.Handle(tcell.NewEventPaste(true))
	keyEditor(m, tcell.KeyEnter, "")
	keyEditor(m, tcell.KeyRune, "# 中文")
	m.Handle(tcell.NewEventPaste(false))
	if strings.Join(m.Editor.Lines, "\n") != "proxies: []\nrules: []\n# 中文" {
		t.Fatal(m.Editor.Lines)
	}
	keyEditor(m, tcell.KeyCtrlZ, "")
	if len(m.Editor.Lines) != 2 {
		t.Fatal("paste undo was not atomic")
	}
	keyEditor(m, tcell.KeyCtrlY, "")
	a := keyEditor(m, tcell.KeyCtrlS, "")
	if a == nil || a.Kind != "profile-file-save" || a.ID != "first" || a.Value != "original" || m.PendingEditor == nil {
		t.Fatal(a)
	}
	m.Complete(fixture(), *a, errors.New("core rejected profile"))
	if m.Editor == nil || !strings.Contains(strings.Join(m.Editor.Lines, "\n"), "中文") || m.Modal != nil {
		t.Fatal("save failure discarded the editor")
	}
	keyEditor(m, tcell.KeyCtrlS, "")
	m.Complete(fixture(), *a, nil)
	if m.Editor != nil || m.PendingEditor != nil {
		t.Fatal("successful save retained editor")
	}
}
func TestEditorMouseWheelUnicodeAndDiscardConfirmation(t *testing.T) {
	m := editorModel("# 中文🙂\n" + strings.Repeat("# more\n", 50) + profile.DefaultConfig)
	keyEditor(m, tcell.KeyEnd, "")
	keyEditor(m, tcell.KeyBackspace2, "")
	if m.Editor.Lines[0] != "# 中文" {
		t.Fatal("unicode deletion broke rune boundaries", m.Editor.Lines[0])
	}
	keyEditor(m, tcell.KeyEscape, "")
	if m.Modal == nil || m.Modal.Title != "Unsaved Changes" {
		t.Fatal("dirty editor closed without confirmation")
	}
	m.Activate(app.Action{Kind: "close"})
	if m.Editor == nil {
		t.Fatal("cancel discarded data")
	}
	m.Render(60, 18)
	m.Handle(tcell.NewEventMouse(40, 10, tcell.WheelDown, tcell.ModNone))
	top := m.Editor.Top
	m.Render(60, 18)
	if top != 3 || m.Editor.Top != top || m.Focus != "" {
		t.Fatal("wheel changed focus or viewport")
	}
	for _, hit := range m.Frame.Hits {
		if hit.Action.Kind == "editor-input" {
			m.Handle(tcell.NewEventMouse(hit.Rect.X+2, hit.Rect.Y, tcell.Button1, tcell.ModNone))
			break
		}
	}
	if m.Editor.Row != top || m.Focus != Key(app.Action{Kind: "editor-input"}) {
		t.Fatal("mouse caret positioning failed")
	}
	keyEditor(m, tcell.KeyRune, "changed")
	keyEditor(m, tcell.KeyEscape, "")
	m.Activate(app.Action{Kind: "editor-discard"})
	if m.Editor != nil || m.Modal != nil {
		t.Fatal("discard did not close editor")
	}
}
func TestProfileEditInfoUsesOriginalURLAndRetainsFailedForm(t *testing.T) {
	m := New(fixture())
	p := m.View.Settings.Profiles[0]
	p.URL = "https://example.com/profile?token=private"
	p.UserAgent = "CustomClient/1.0"
	p.Timeout = 30
	m.showProfileDocument("profile-info", &app.ProfileDocument{Profile: p, Revision: "rev"})
	for i := range m.Modal.Fields {
		if m.Modal.Fields[i].Key == "name" {
			m.Modal.Fields[i].Text = "Renamed"
		}
	}
	a := keyEditor(m, tcell.KeyCtrlS, "")
	if a == nil || a.Profile.URL != p.URL || a.Profile.UserAgent != p.UserAgent || a.Profile.Name != "Renamed" || a.Value != "rev" {
		t.Fatal("editor used redacted public metadata", a)
	}
	m.Complete(fixture(), *a, errors.New("subscription URL invalid"))
	if m.Modal == nil || m.Modal.Kind != "profile-info-save" {
		t.Fatal("save failure discarded profile information")
	}
}
