package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
	"verge-tui/internal/state"
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
	m.Activate(app.Action{Kind: "select-option", ID: "type", Value: "Local"})
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

func TestMouseWheelClearsFocus(t *testing.T) {
	for _, wheel := range []tcell.ButtonMask{tcell.WheelDown, tcell.WheelUp, tcell.WheelLeft, tcell.WheelRight} {
		for _, dialog := range []bool{false, true} {
			m := New(fixture())
			m.Tab = 2
			m.URL = "https://example.com/profile"
			id := "url"
			if dialog {
				m.Activate(app.Action{Kind: "new"})
				id = "name"
			}
			m.Activate(app.Action{Kind: "input", ID: id})
			m.Render(120, 42)
			input := m.Input()
			if input == nil {
				t.Fatal("input did not receive focus")
			}
			before := *input
			m.Handle(tcell.NewEventMouse(40, 10, wheel, tcell.ModNone))
			f := m.Render(120, 42)
			if m.Focus != "" || m.Input() != nil {
				t.Fatal("wheel retained input focus, including at a scroll boundary")
			}
			for _, cell := range f.Cells {
				if cell.Style.HasReverse() {
					t.Fatal("wheel retained a focus highlight")
				}
			}
			m.Handle(tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone))
			if *input != before {
				t.Fatal("typing after scrolling edited the old input")
			}
			m.Handle(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
			if m.Focus == "" {
				t.Fatal("Tab could not restore keyboard focus")
			}
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

func TestKeyboardCreatesLocalProfileInSmallTerminal(t *testing.T) {
	m := New(fixture())
	key := func(k tcell.Key, text string) *app.Action {
		a := m.Handle(tcell.NewEventKey(k, text, tcell.ModNone))
		m.Render(60, 18)
		return a
	}
	m.Render(60, 18)
	key(tcell.KeyRune, "3")
	key(tcell.KeyRune, "n")
	key(tcell.KeyLeft, "")
	key(tcell.KeyDown, "")
	key(tcell.KeyEnter, "")
	key(tcell.KeyTab, "")
	key(tcell.KeyRune, "Keyboard profile")
	key(tcell.KeyF1, "")
	if m.Modal.Kind != "help" {
		t.Fatal("F1 did not show keyboard help")
	}
	key(tcell.KeyEscape, "")
	if m.Input() == nil || *m.Input() != "Keyboard profile" {
		t.Fatal("help discarded the profile form")
	}
	key(tcell.KeyTab, "")
	key(tcell.KeyRune, "Created without a mouse")
	key(tcell.KeyTab, "")
	key(tcell.KeyTab, "")
	a := key(tcell.KeyEnter, "")
	if a == nil || a.Kind != "profile-new" || a.Profile.Type != "local" || a.Profile.Name != "Keyboard profile" || a.File != "" {
		t.Fatal("keyboard-only profile creation failed")
	}
}

func TestKeyboardScrollsThroughAllModalChoices(t *testing.T) {
	v := fixture()
	g := v.Snapshot.Proxies["Proxy"]
	g.All = nil
	for i := range 30 {
		g.All = append(g.All, fmt.Sprintf("Node %02d", i))
	}
	v.Snapshot.Proxies["Proxy"] = g
	m := New(v)
	m.Activate(app.Action{Kind: "home-nodes"})
	m.Render(60, 18)
	for i, name := range g.All {
		m.Handle(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
		f := m.Render(60, 18)
		expected := Key(app.Action{Kind: "node-select", ID: "Proxy", Node: name})
		visible := false
		for _, hit := range f.Hits {
			if hit.Key == expected {
				visible = true
			}
		}
		if m.Focus != expected || !visible {
			t.Fatalf("choice %d is not keyboard-accessible", i)
		}
	}
	a := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if a == nil || a.Kind != "node-select" || a.Node != "Node 29" {
		t.Fatal("Enter did not select the final node")
	}
}

func TestInputCursorEditsWholeGraphemes(t *testing.T) {
	m := New(fixture())
	m.Tab = 2
	m.Focus = Key(app.Action{Kind: "input", ID: "url"})
	m.Render(80, 24)
	key := func(k tcell.Key, text string) { m.Handle(tcell.NewEventKey(k, text, tcell.ModNone)); m.Render(80, 24) }
	key(tcell.KeyRune, "ab👩‍💻cd")
	key(tcell.KeyLeft, "")
	key(tcell.KeyLeft, "")
	key(tcell.KeyDelete, "")
	key(tcell.KeyBackspace, "")
	if m.URL != "abd" {
		t.Fatal("cursor editing split an emoji grapheme")
	}
	key(tcell.KeyHome, "")
	key(tcell.KeyRune, "X")
	if m.URL != "Xabd" {
		t.Fatal("Home did not move the insertion point")
	}
	key(tcell.KeyEnd, "")
	key(tcell.KeyCtrlW, "")
	if m.URL != "" {
		t.Fatal("Ctrl+W did not delete the preceding word")
	}
}

func TestArrowNavigationAndModalIsolation(t *testing.T) {
	m := New(fixture())
	m.Render(120, 42)
	m.Handle(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	m.Render(120, 42)
	m.Handle(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	m.Render(120, 42)
	m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if m.Tab != 1 {
		t.Fatal("arrows and Enter did not navigate to Proxies")
	}
	m.newProfile()
	m.Render(120, 42)
	m.Handle(tcell.NewEventKey(tcell.KeyRune, "1", tcell.ModNone))
	if m.Tab != 1 {
		t.Fatal("page shortcut escaped the active dialog")
	}
}
