package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"mihomo-tui/internal/app"
	"mihomo-tui/internal/controller"
	"mihomo-tui/internal/state"
)

func fixture() app.View {
	return app.View{Online: true, OS: "Linux / amd64", Version: "0.1.0", Settings: state.Settings{Mode: "rule", Core: "v1.19.32", MixedPort: 23456, Current: "first", HomeGroup: map[string]string{}, Profiles: []state.Profile{{ID: "first", Type: "remote", Name: "Example", URL: "https://example.com/profile", Updated: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Usage: &state.Usage{Total: 10000, Download: 2000}}, {ID: "second", Type: "local", Name: "Local YAML"}}}, Snapshot: controller.Snapshot{Config: controller.Config{Mode: "rule"}, Version: "v1.19.32", Proxies: map[string]controller.Proxy{"Proxy": {Name: "Proxy", Type: "Selector", All: []string{"节点 A", "Node B"}, Now: "Node B"}, "GLOBAL": {Name: "GLOBAL", Type: "Selector", All: []string{"Proxy", "DIRECT"}}, "节点 A": {Name: "节点 A", Type: "Shadowsocks"}, "Node B": {Name: "Node B", Type: "VMess"}}}}
}

func TestHomeMatchesRequestedSourceLayout(t *testing.T) {
	m := New(fixture())
	f := m.Render(120, 42)
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
		t.Fatalf("missing home card %q", text)
		return 0, 0
	}
	px, py := find("Example")
	nx, ny := find("Current Node")
	_, networkY := find("Network Settings")
	_, modeY := find("Proxy Mode")
	_, clashY := find("Clash Info")
	_, systemY := find("System Info")
	if px >= nx || py != ny || networkY != modeY || clashY != systemY || py >= networkY || networkY >= clashY {
		t.Fatal("home card order differs from upstream")
	}
	if m.MaxScroll != 0 {
		t.Fatal("six cards should fit in a 120x42 terminal")
	}
}

func TestMouseProfileSelectionAndImportInput(t *testing.T) {
	m := New(fixture())
	m.Tab = 2
	f := m.Render(120, 42)
	var card Hit
	for _, h := range f.Hits {
		if h.Action.Kind == "profile-select" && h.Action.ID == "second" {
			card = h
			break
		}
	}
	action := m.Handle(tcell.NewEventMouse(card.Rect.X+1, card.Rect.Y+1, tcell.Button1, tcell.ModNone))
	if action == nil || action.Kind != "profile-select" || action.ID != "second" {
		t.Fatal("mouse selected the wrong profile")
	}
	m.Handle(tcell.NewEventMouse(0, 0, tcell.ButtonNone, tcell.ModNone))
	m.Activate(app.Action{Kind: "input", ID: "url"})
	m.Handle(tcell.NewEventPaste(true))
	m.Handle(tcell.NewEventKey(tcell.KeyRune, "https://example.com/new", tcell.ModNone))
	m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	m.Handle(tcell.NewEventPaste(false))
	if m.URL != "https://example.com/new" {
		t.Fatal("bracketed paste changed the URL")
	}
	action = m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if action == nil || action.Kind != "profile-import" || action.Profile.URL != m.URL {
		t.Fatal("Enter did not import URL")
	}
	if m.View.Settings.Current != "first" {
		t.Fatal("UI changed profile state before backend success")
	}
}

func TestCreateLocalProfileAndKeyboardFocus(t *testing.T) {
	m := New(fixture())
	m.Tab = 2
	m.Activate(app.Action{Kind: "new"})
	m.Activate(app.Action{Kind: "input", ID: "type"})
	for i := range m.Modal.Fields {
		if m.Modal.Fields[i].Key == "name" {
			m.Modal.Fields[i].Text = "Local example"
		}
	}
	m.Render(100, 30)
	m.FocusNext(1)
	a := m.Activate(app.Action{Kind: "submit"})
	if a == nil || a.Kind != "profile-new" || a.Profile.Type != "local" || a.Profile.Name != "Local example" {
		t.Fatal("Local form does not create a profile")
	}
	m.Render(100, 30)
	seen := map[string]bool{}
	for range 8 {
		m.FocusNext(1)
		if seen[m.Focus] {
			t.Fatal("duplicate hit regions trapped keyboard focus")
		}
		seen[m.Focus] = true
	}
}

func TestNarrowTerminalScrollAndUnicode(t *testing.T) {
	m := New(fixture())
	f := m.Render(60, 18)
	if m.MaxScroll == 0 {
		t.Fatal("narrow home cannot be scrolled")
	}
	m.Handle(tcell.NewEventMouse(40, 10, tcell.WheelDown, tcell.ModNone))
	if m.Scroll[0] == 0 {
		t.Fatal("wheel did not scroll")
	}
	m.Tab = 1
	f = m.Render(80, 24)
	for _, line := range strings.Split(f.Plain(), "\n") {
		if displaywidth.String(line) > 80 {
			t.Fatal("Unicode rendering exceeded terminal width")
		}
	}
	for _, hit := range f.Hits {
		if hit.Rect.X < 0 || hit.Rect.Y < 0 || hit.Rect.X+hit.Rect.W > 80 || hit.Rect.Y+hit.Rect.H > 24 {
			t.Fatal("offscreen mouse hit")
		}
	}
	m.Activate(app.Action{Kind: "home-groups"})
	f = m.Render(80, 24)
	for _, hit := range f.Hits {
		if hit.Action.Kind == "navigate" {
			t.Fatal("modal allowed a click through to the sidebar")
		}
	}
}

func TestDirectAndGlobalProxyModes(t *testing.T) {
	m := New(fixture())
	m.View.Snapshot.Config.Mode = "direct"
	m.View.Settings.Mode = "direct"
	m.Tab = 1
	if !strings.Contains(m.Render(120, 42).Plain(), "Direct Mode") {
		t.Fatal("direct mode still shows proxy groups")
	}
	m.View.Snapshot.Config.Mode = "global"
	m.View.Settings.Mode = "global"
	groups := app.Groups(m.View)
	if len(groups) != 1 || groups[0].Name != "GLOBAL" {
		t.Fatal("global mode displays rule groups")
	}
}

func TestPagesAndFocusInheritTerminalTheme(t *testing.T) {
	m := New(fixture())
	for page := range 3 {
		m.Tab = page
		m.Render(120, 42)
		m.FocusNext(1)
		f := m.Render(120, 42)
		reverse := false
		for _, cell := range f.Cells {
			if cell.Style.GetBackground() != color.Default || cell.Style.GetForeground().IsRGB() {
				t.Fatal("page forces a theme-specific background or RGB foreground")
			}
			reverse = reverse || cell.Style.HasReverse()
		}
		if !reverse {
			t.Fatal("keyboard focus is not visible through terminal reverse video")
		}
	}
	m.Activate(app.Action{Kind: "new"})
	m.Render(120, 42)
	m.FocusNext(1)
	for _, cell := range m.Render(120, 42).Cells {
		if cell.Style.GetBackground() != color.Default {
			t.Fatal("dialog overrides the terminal's background")
		}
	}
}
