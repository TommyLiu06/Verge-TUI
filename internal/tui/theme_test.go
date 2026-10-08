package tui

import (
	"github.com/gdamore/tcell/v3/color"
	"math"
	"strings"
	"testing"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
	"verge-tui/internal/state"
)

func luminance(c color.Color) float64 {
	r, g, b := c.RGB()
	linear := func(v int32) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return .2126*linear(r) + .7152*linear(g) + .0722*linear(b)
}
func TestThemesApplyToPagesAndDialogsWithoutChangingDelayColors(t *testing.T) {
	t.Setenv("COLORFGBG", "15;0")
	v := fixture()
	v.Preferences = state.DefaultPreferences()
	m := New(v)
	node := controller.Proxy{History: []controller.History{{Delay: 300}}}
	wantDelay := m.delayColor(node)
	for _, id := range state.Themes {
		t.Run(id, func(t *testing.T) {
			v.Preferences.Theme = id
			m.Complete(v, app.Action{Kind: "refresh"}, nil)
			for page := 0; page < 4; page++ {
				m.Tab = page
				frame := m.Render(120, 50)
				if frame.Cells[0].Style.GetBackground() != paletteFor(id).Sidebar.GetBackground() {
					t.Fatal("sidebar theme not applied")
				}
				if frame.Cells[119].Style.GetBackground() != paletteFor(id).Base.GetBackground() {
					t.Fatal("page background not applied")
				}
			}
			m.settingsForm("theme")
			frame := m.Render(120, 50)
			if !strings.Contains(frame.Plain(), themeLabel(id)) {
				t.Fatal("theme picker does not show readable theme name")
			}
			m.Modal = nil
			if m.delayColor(node) != wantDelay {
				t.Fatal("interface theme changed latency colors")
			}
			palette := paletteFor(id)
			if id == "terminal" {
				if palette.Base.GetBackground() != color.Default || palette.Text != color.Default {
					t.Fatal("terminal defaults overwritten")
				}
				return
			}
			for _, bg := range []color.Color{palette.Base.GetBackground(), palette.Card.GetBackground(), palette.Sidebar.GetBackground()} {
				for _, fg := range []color.Color{palette.Text, palette.Muted, palette.Accent} {
					a, b := luminance(fg), luminance(bg)
					if a < b {
						a, b = b, a
					}
					if (a+.05)/(b+.05) < 4.5 {
						t.Fatalf("insufficient text contrast: %v on %v", fg, bg)
					}
				}
			}
		})
	}
}
