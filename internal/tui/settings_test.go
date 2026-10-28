package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
	"verge-tui/internal/state"
)

func TestSettingsMatchesUpstreamSectionsAndTerminalTheme(t *testing.T) {
	v := fixture()
	v.Settings.InstallMode = "system"
	v.Preferences = state.DefaultPreferences()
	m := New(v)
	m.Handle(tcell.NewEventKey(tcell.KeyRune, "7", tcell.ModNone))
	f := m.Render(120, 50)
	for _, label := range []string{"System Setting", "Clash Setting", "Verge Basic Setting", "Verge Advanced Setting", "DNS Overwrite", "IPv6", "Unified Delay", "Log Level", "Port Config", "Runtime Config"} {
		if !strings.Contains(f.Plain(), label) {
			t.Fatal("missing upstream setting", label)
		}
	}
	find := func(text string) (int, int) {
		for y := 0; y < f.H; y++ {
			var row strings.Builder
			for x := 0; x < f.W; x++ {
				row.WriteString(f.Cells[y*f.W+x].Text)
			}
			if x := strings.Index(row.String(), text); x >= 0 {
				return x, y
			}
		}
		t.Fatal("missing section", text)
		return 0, 0
	}
	sx, sy := find("System Setting")
	cx, cy := find("Clash Setting")
	bx, by := find("Verge Basic Setting")
	ax, ay := find("Verge Advanced Setting")
	if sx != cx || bx != ax || sx >= bx || sy != by || cy <= sy || ay <= by {
		t.Fatal("settings section layout differs from upstream")
	}
	for _, cell := range f.Cells {
		if cell.Style.GetBackground() != color.Default {
			t.Fatal("settings forces a terminal background")
		}
	}
	var target Hit
	loginToggle := false
	for _, hit := range f.Hits {
		if hit.Action.Kind == "preferences-save" && hit.Action.ID == "proxy-on-login" {
			loginToggle = hit.Action.Fields["proxy-on-login"] == "false"
		}
		if hit.Action.Kind == "settings-save" && hit.Action.ID == "ipv6" {
			target = hit
		}
	}
	if !loginToggle {
		t.Fatal("login proxy setting did not offer to disable its enabled default")
	}
	a := m.Handle(tcell.NewEventMouse(target.Rect.X, target.Rect.Y, tcell.Button1, tcell.ModNone))
	if a == nil || a.Kind != "settings-save" || a.ID != "ipv6" || a.Fields["enabled"] != "true" {
		t.Fatal("mouse did not activate settings toggle")
	}
}

func TestKeyboardEditsScrollableTUNFormAndWheelClearsFocus(t *testing.T) {
	v := fixture()
	v.Settings.InstallMode = "system"
	m := New(v)
	m.Tab = 3
	m.Activate(app.Action{Kind: "settings-form", ID: "tun"})
	m.Render(60, 18)
	for range 7 {
		m.Handle(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
		m.Render(60, 18)
	}
	if m.Focus != Key(app.Action{Kind: "input", ID: "route-exclude"}) || m.Modal.Scroll == 0 {
		t.Fatal("last TUN field not keyboard accessible")
	}
	m.Handle(tcell.NewEventKey(tcell.KeyRune, "192.168.0.0/16", tcell.ModNone))
	a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
	if a == nil || a.ID != "tun" || a.Fields["route-exclude"] != "192.168.0.0/16" || a.Fields["stack"] != "system" {
		t.Fatal("keyboard did not save TUN settings")
	}
	m.Render(60, 18)
	m.FocusNext(1)
	m.Handle(tcell.NewEventMouse(40, 10, tcell.WheelDown, tcell.ModNone))
	if m.Focus != "" || m.Scroll[3] == 0 {
		t.Fatal("settings wheel did not scroll and clear focus")
	}
}

func TestPreferencesAndDNSFormsCaptureProfileAndStartupPage(t *testing.T) {
	v := fixture()
	v.Preferences = state.DefaultPreferences()
	m := New(v)
	m.Activate(app.Action{Kind: "settings-form", ID: "dns"})
	a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
	if a == nil || a.Kind != "settings-save" || a.ID != "dns" || a.Node != "first" {
		t.Fatal("DNS form lost its profile scope")
	}
	m.Activate(app.Action{Kind: "settings-form", ID: "start-page"})
	m.Render(60, 18)
	for range 6 {
		m.Handle(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	}
	a = m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
	if a == nil || a.Kind != "preferences-save" || a.Fields["start-page"] != "settings" {
		t.Fatal("start page form did not save preferences")
	}
	v.Preferences.StartPage = "settings"
	v.Preferences.Theme = "dark"
	v.Preferences.DelayTimeout = 12000
	m.Complete(v, app.Action{Kind: "refresh"}, nil)
	if m.Tab != 3 || paletteFor(v.Preferences.Theme).Base.GetBackground() == color.Default {
		t.Fatal("saved startup page or palette not applied")
	}
	m.Tab = 1
	m.Complete(v, app.Action{Kind: "refresh"}, nil)
	if m.Tab != 1 {
		t.Fatal("refresh unexpectedly navigated to the startup page")
	}
	p := controller.Proxy{History: []controller.History{{Delay: 6000}}}
	if m.delayLabel(p) != "6000 ms" {
		t.Fatal("long successful delay rendered as default five-second timeout")
	}
}
