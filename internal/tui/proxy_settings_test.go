package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/desktop"
	"verge-tui/internal/i18n"
	"verge-tui/internal/state"
)

func TestProxySettingsCombineConfigurationAndStatusWithSeparateControls(t *testing.T) {
	for _, width := range []int{60, 120} {
		for _, enabled := range []bool{false, true} {
			v := fixture()
			v.Preferences = state.DefaultPreferences()
			v.Settings.InstallMode = "system"
			v.Settings.Tun = !enabled // The observed core status takes precedence.
			v.Snapshot.Config.Tun.Enable = enabled
			v.Desktop = desktop.Status{Available: true, Enabled: enabled}
			m := New(v)
			m.Tab = 3
			frame := m.Render(width, 42)
			status := "Disabled"
			if enabled {
				status = "Enabled"
			}
			if strings.Count(frame.Plain(), "Configure | "+status) != 2 || strings.Count(frame.Plain(), "TUN Mode") != 1 {
				t.Fatal("duplicate TUN rows or incorrect control layout", width, enabled)
			}
			for _, kind := range []string{"tun", "system-proxy"} {
				var configure, toggle Hit
				for _, hit := range m.Frame.Hits {
					if hit.Action.Kind == "settings-form" && hit.Action.ID == kind {
						configure = hit
					}
					if hit.Action.Kind == kind {
						toggle = hit
					}
				}
				if configure.Rect.W == 0 || toggle.Rect.W == 0 || configure.Rect.Y != toggle.Rect.Y || configure.Rect.X+configure.Rect.W+3 != toggle.Rect.X {
					t.Fatal("configuration and switch have overlapping or missing targets")
				}
				m.Handle(tcell.NewEventMouse(configure.Rect.X, configure.Rect.Y, tcell.Button1, tcell.ModNone))
				m.Handle(tcell.NewEventMouse(configure.Rect.X, configure.Rect.Y, tcell.ButtonNone, tcell.ModNone))
				if m.Modal == nil || m.Modal.Section != kind || len(m.Modal.Fields) == 0 {
					t.Fatal("Configure did not open its editable settings", kind)
				}
				if kind == "tun" && m.tr(m.Modal.Title) != "TUN Settings" {
					t.Fatal("TUN settings title still says TUN Mode")
				}
				m.Handle(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
				m.Render(width, 42)
				a := m.Handle(tcell.NewEventMouse(toggle.Rect.X, toggle.Rect.Y, tcell.Button1, tcell.ModNone))
				m.Handle(tcell.NewEventMouse(toggle.Rect.X, toggle.Rect.Y, tcell.ButtonNone, tcell.ModNone))
				if a == nil || a.Kind != kind || m.Modal != nil {
					t.Fatal("switch opened configuration instead of toggling", kind)
				}
				m.Focus = Key(app.Action{Kind: "settings-form", ID: kind})
				m.Render(width, 42)
				if a := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone)); a != nil || m.Modal == nil || m.Modal.Section != kind {
					t.Fatal("Enter did not open the focused configuration")
				}
				m.Handle(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
				m.Focus = configure.Key
				m.Render(width, 42)
				m.Handle(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
				if m.Focus != toggle.Key {
					t.Fatal("keyboard cannot focus the two controls separately")
				}
				m.Render(width, 42)
				if a := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone)); a == nil || a.Kind != kind {
					t.Fatal("Enter did not activate the focused switch")
				}
				m.Render(width, 42)
			}
		}
	}
}

func TestSystemProxyFormUsesListAndPreservesHostAndBypassEdits(t *testing.T) {
	v := fixture()
	v.Preferences = state.DefaultPreferences()
	m := New(v)
	m.settingsForm("system-proxy")
	m.Render(60, 18)
	if !m.fieldDisabled("proxy-bypass") || m.Input() == nil {
		t.Fatal("default bypass is editable or proxy host is inaccessible")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyCtrlU, "", tcell.ModNone))
	m.Handle(tcell.NewEventKey(tcell.KeyRune, "localhost", tcell.ModNone))
	m.Handle(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
	m.Render(60, 18)
	m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if !m.Modal.List || m.Modal.Parent == nil {
		t.Fatal("default bypass option did not open a list")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyUp, "", tcell.ModNone))
	m.Render(60, 18)
	m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if m.fieldDisabled("proxy-bypass") {
		t.Fatal("custom bypass cannot be edited")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
	m.Render(60, 18)
	m.Handle(tcell.NewEventKey(tcell.KeyCtrlU, "", tcell.ModNone))
	m.Handle(tcell.NewEventKey(tcell.KeyRune, "*.example.com, 10.0.0.0/8", tcell.ModNone))
	a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
	if a == nil || a.Kind != "preferences-save" || a.ID != "system-proxy" || a.Fields["proxy-host"] != "localhost" || a.Fields["use-default-bypass"] != "false" || a.Fields["proxy-bypass"] != "*.example.com, 10.0.0.0/8" {
		t.Fatal("saving proxy configuration lost form edits")
	}
}

func TestLocalizedProxyControlsRemainIndependentlyAccessible(t *testing.T) {
	for _, language := range i18n.Languages {
		for _, width := range []int{60, 120} {
			v := fixture()
			v.Preferences = state.DefaultPreferences()
			v.Preferences.Language = language
			v.Settings.InstallMode = "system"
			v.Desktop.Available = true
			m := New(v)
			m.Tab = 3
			f := m.Render(width, 42)
			for _, kind := range []string{"tun", "system-proxy"} {
				configure, status := false, false
				for _, hit := range f.Hits {
					if hit.Action.Kind == "settings-form" && hit.Action.ID == kind {
						configure = hit.Rect.W > 0
					}
					if hit.Action.Kind == kind {
						status = hit.Rect.W > 0
					}
				}
				if !configure || !status {
					t.Fatal("translation hid a proxy control", language, width, kind)
				}
			}
		}
	}
}
