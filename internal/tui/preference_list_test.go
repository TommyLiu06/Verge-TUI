package tui

import (
	"github.com/gdamore/tcell/v3"
	"strings"
	"testing"
	"verge-tui/internal/app"
	"verge-tui/internal/i18n"
	"verge-tui/internal/state"
)

func TestPreferenceListsShowAllChoicesAndSelectWithMouse(t *testing.T) {
	for _, section := range []string{"theme", "language", "start-page", "log-level"} {
		t.Run(section, func(t *testing.T) {
			v := fixture()
			v.Preferences = state.DefaultPreferences()
			m := New(v)
			m.settingsForm(section)
			frame := m.Render(120, 50)
			options, current := state.Themes, v.Preferences.Theme
			kind, field := "preferences-save", section
			switch section {
			case "language":
				options, current = i18n.Languages, v.Preferences.Language
			case "start-page":
				options, current = []string{"home", "proxies", "profiles", "settings"}, v.Preferences.StartPage
			case "log-level":
				options, current = []string{"debug", "info", "warning", "error", "silent"}, "info"
				kind, field = "settings-save", "level"
			}
			if len(m.Modal.Fields) != 0 || len(m.Modal.Choices) != len(options) {
				t.Fatal("picker still cycles one field")
			}
			for _, choice := range m.Modal.Choices {
				if !strings.Contains(frame.Plain(), choice.Label) {
					t.Fatal("list does not show every option", choice.Label)
				}
				if m.choiceSelected(choice.Action) != (choice.Action.Value == current) {
					t.Fatal("incorrect current-selection indicator")
				}
			}
			var target Hit
			wanted := options[len(options)-1]
			for _, hit := range frame.Hits {
				if hit.Action.Kind == kind && hit.Action.Value == wanted {
					target = hit
					break
				}
			}
			if target.Action.Kind == "" {
				t.Fatal("list choice is not clickable")
			}
			action := m.Handle(tcell.NewEventMouse(target.Rect.X, target.Rect.Y, tcell.Button1, tcell.ModNone))
			if action == nil || action.Kind != kind || action.Fields[field] != wanted || m.Modal != nil {
				t.Fatal("click did not apply the selected choice")
			}
		})
	}
}

func TestNarrowPreferenceListRevealsFocusAndPreservesWheelBehavior(t *testing.T) {
	v := fixture()
	v.Preferences = state.DefaultPreferences()
	v.Preferences.Language = "zhtw"
	m := New(v)
	m.settingsForm("language")
	frame := m.Render(60, 18)
	visible := func(action app.Action) bool {
		for _, hit := range m.Frame.Hits {
			if hit.Key == Key(action) {
				return true
			}
		}
		return false
	}
	selected := m.Modal.Choices[len(m.Modal.Choices)-1].Action
	if !visible(selected) || m.Focus != Key(selected) || m.Modal.Scroll == 0 {
		t.Fatal("initial selection is off screen")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyUp, "", tcell.ModNone))
	m.Render(60, 18)
	previous := m.Modal.Choices[len(m.Modal.Choices)-2].Action
	if !visible(previous) || m.Focus != Key(previous) {
		t.Fatal("keyboard list navigation does not reveal the choice")
	}
	action := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if action == nil || action.Fields["language"] != previous.Value {
		t.Fatal("Enter did not select the focused language")
	}
	m.settingsForm("language")
	m.Render(60, 18)
	m.Handle(tcell.NewEventMouse(frame.W/2, frame.H/2, tcell.WheelUp, tcell.ModNone))
	if m.Focus != "" {
		t.Fatal("wheel retained focus")
	}
	scroll := m.Modal.Scroll
	m.Render(60, 18)
	if m.Focus != "" || m.Modal.Scroll != scroll {
		t.Fatal("redraw moved focus or undid wheel scrolling")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
	if m.Modal != nil || m.View.Preferences.Language != "zhtw" {
		t.Fatal("closing the list changed preferences")
	}
}

func TestFormOptionListsPreserveEditsAndRequireSave(t *testing.T) {
	for _, section := range []string{"tun", "dns", "misc", "controller", "new"} {
		t.Run(section, func(t *testing.T) {
			v := fixture()
			v.Settings.InstallMode = "system"
			v.Preferences = state.DefaultPreferences()
			m := New(v)
			switch section {
			case "new":
				m.newProfile()
			case "controller":
				m.showSettingsView("controller", []string{"false", "127.0.0.1:9090", "secret"})
			default:
				m.settingsForm(section)
			}
			parent := m.Modal
			var editedKey, editedText string
			for i := range parent.Fields {
				if len(parent.Fields[i].Options) == 0 {
					parent.Fields[i].Text += " unsaved"
					editedKey, editedText = parent.Fields[i].Key, parent.Fields[i].Text
					break
				}
			}
			for _, field := range parent.Fields {
				if len(field.Options) == 0 {
					continue
				}
				m.Render(60, 18)
				if a := m.Activate(app.Action{Kind: "input", ID: field.Key}); a != nil {
					t.Fatal("opening picker saved the form")
				}
				picker := m.Modal
				if !picker.List || picker.Parent != parent || len(picker.Choices) != len(field.Options) {
					t.Fatal("enum field did not open a complete list", field.Key)
				}
				for _, choice := range picker.Choices {
					if m.choiceSelected(choice.Action) != (choice.Action.Value == field.Text) {
						t.Fatal("wrong current form value", field.Key)
					}
				}
				// Cancelling must restore the form, including its unsaved values.
				m.Handle(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
				if m.Modal != parent || m.Focus != Key(app.Action{Kind: "input", ID: field.Key}) {
					t.Fatal("cancel discarded the form")
				}
				m.Activate(app.Action{Kind: "input", ID: field.Key})
				choice := m.Modal.Choices[len(m.Modal.Choices)-1]
				m.Focus = Key(choice.Action)
				m.Render(60, 18)
				if a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone)); a != nil || m.Modal != parent {
					t.Fatal("choosing an option persisted the form")
				}
				for _, edited := range parent.Fields {
					if edited.Key == field.Key && edited.Text != choice.Action.Value {
						t.Fatal("choice not copied into form")
					}
					if edited.Key == editedKey && edited.Text != editedText {
						t.Fatal("list discarded unrelated unsaved input")
					}
				}
			}
			if parent.ProfileID != "first" {
				if section != "new" {
					t.Fatal("list lost profile scope")
				}
			}
			if section == "dns" {
				m.View.Settings.Current = "second"
				a := m.submit()
				if a == nil || a.Node != "first" || a.Fields["mode"] != "redir-host" || a.Fields["nameservers"] == "" {
					t.Fatal("saving lost list selection, edits or original profile scope")
				}
			}
		})
	}
}
