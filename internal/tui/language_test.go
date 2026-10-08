package tui

import (
	"github.com/gdamore/tcell/v3"
	"strings"
	"testing"
	"verge-tui/internal/app"
	"verge-tui/internal/i18n"
	"verge-tui/internal/state"
)

func TestLanguagePickerAndViewsPreserveUserData(t *testing.T) {
	v := fixture()
	v.Preferences = state.DefaultPreferences()
	v.Settings.Profiles[0].Name = "Settings"
	for _, language := range i18n.Languages {
		v.Preferences.Language = language
		m := New(v)
		for page := 0; page < 4; page++ {
			m.Tab = page
			text := m.Render(160, 60).Plain()
			if !strings.Contains(text, i18n.Text(language, "Home")) {
				t.Fatal("navigation not translated", language)
			}
			if page == 2 && !strings.Contains(text, "Settings") {
				t.Fatal("profile name translated as UI text", language)
			}
		}
		m.settingsForm("language")
		if len(m.Modal.Choices) != 13 || m.Modal.Choices[0].Label != i18n.Names["en"] {
			t.Fatal("language choices differ from Verge")
		}
		m.Render(160, 60)
		key := tcell.KeyUp
		if language == "en" {
			key = tcell.KeyDown
		}
		m.Handle(tcell.NewEventKey(key, "", tcell.ModNone))
		a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
		if a == nil || a.Kind != "preferences-save" || a.Fields["language"] == language {
			t.Fatal("keyboard cannot switch language")
		}
		v.Preferences.Language = "zh"
		m.Complete(v, app.Action{Kind: "preferences-save"}, nil)
		if !strings.Contains(m.Render(160, 60).Plain(), i18n.Text("zh", "Settings")) {
			t.Fatal("language change not applied immediately")
		}
	}
}
