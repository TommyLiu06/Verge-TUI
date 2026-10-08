package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3/color"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
)

func TestDelayProgressRendersBeforeCompletionAndSurvivesReload(t *testing.T) {
	m := New(fixture())
	m.Tab, m.Busy = 1, true
	m.ApplyDelay(app.DelayResult{Name: "节点 A", Delay: 260, Done: 1, Total: 2})
	f := m.Render(120, 42)
	if !m.Busy || !strings.Contains(f.Plain(), "260 ms") || !strings.Contains(f.Plain(), "Delay check: 1/2") {
		t.Fatal("first result was not rendered during the batch")
	}
	m.ApplyDelay(app.DelayResult{Name: "Node B", Delay: app.DelayError, Done: 2, Total: 2})
	if !strings.Contains(m.Render(120, 42).Plain(), "Error") {
		t.Fatal("failed node retained an old latency")
	}
	stale := fixture()
	node := stale.Snapshot.Proxies["Node B"]
	node.History = []controller.History{{Delay: 10}}
	stale.Snapshot.Proxies["Node B"] = node
	m.Complete(stale, app.Action{Kind: "delay"}, errors.New("1 of 2 nodes failed delay testing"))
	if m.Busy || delayValue(m.View.Snapshot.Proxies["Node B"]) != app.DelayError || delayValue(m.View.Snapshot.Proxies["节点 A"]) != 260 {
		t.Fatal("completion snapshot overwrote the actual test results")
	}
	if len(m.Delays) != 0 {
		t.Fatal("completed batch retained a progress cache")
	}
	if m.Modal != nil {
		t.Fatal("failed delay check opened a modal")
	}
	m.Tab = 0
	if !strings.Contains(m.Render(120, 42).Plain(), "Error") {
		t.Fatal("Home did not update the current node's latency")
	}
	m.Complete(fixture(), app.Action{Kind: "profile-select"}, nil)
	if delayValue(m.View.Snapshot.Proxies["Node B"]) != -1 {
		t.Fatal("previous profile's results leaked after switching")
	}
}

func TestDelayFailurePreservesOpenDialog(t *testing.T) {
	m := New(fixture())
	m.Busy = true
	m.help()
	help := m.Modal
	m.Complete(fixture(), app.Action{Kind: "delay"}, errors.New("core API unavailable"))
	if m.Busy || m.Modal != help || !strings.Contains(m.Message, "core API unavailable") {
		t.Fatal("delay failure interrupted the open dialog or lost its status message")
	}
	m.Complete(fixture(), app.Action{Kind: "profile-select"}, errors.New("profile rejected"))
	if m.Modal == nil || m.Modal.Title != "Action Failed" {
		t.Fatal("other action failures no longer show their error dialog")
	}
}

func TestDelayColorsMatchClashVergeRev(t *testing.T) {
	for _, dark := range []bool{false, true} {
		m := New(fixture())
		m.Dark = dark
		palette := [4]int32{0x06943D, 0x007AFF, 0xFF9500, 0xFF3B30}
		if dark {
			palette = [4]int32{0x30D158, 0x0A84FF, 0xFF9F0A, 0xFF453A}
		}
		for _, test := range []struct {
			delay int
			label string
			grade int
		}{{-1, "Not tested", -1}, {-2, "Testing", -1}, {0, "Timeout", 3}, {1, "1 ms", 0}, {249, "249 ms", 0}, {250, "250 ms", 1}, {399, "399 ms", 1}, {400, "400 ms", 2}, {4999, "4999 ms", 2}, {5000, "Timeout", 3}, {app.DelayError, "Error", 3}} {
			p := controller.Proxy{History: []controller.History{{Delay: test.delay}}}
			expected := color.Default
			if test.grade >= 0 {
				expected = color.NewHexColor(palette[test.grade])
			}
			if delayLabel(p) != test.label || m.delayColor(p) != expected {
				t.Fatalf("dark=%v delay=%d: label=%s color=%v", dark, test.delay, delayLabel(p), m.delayColor(p))
			}
			m.ApplyDelay(app.DelayResult{Name: "Node B", Delay: test.delay})
			m.Tab = 1
			action := app.Action{Kind: "delay", ID: "Proxy", Node: "Node B"}
			m.Focus = Key(action)
			frame := m.Render(120, 42)
			found := false
			for _, hit := range frame.Hits {
				if hit.Key != m.Focus {
					continue
				}
				found = true
				style := frame.Cells[hit.Rect.Y*frame.W+hit.Rect.X+1].Style
				if style.GetForeground() != expected || style.GetBackground() != color.Default || !style.HasUnderline() || !style.HasBold() {
					t.Fatal("focused delay button lost its color or terminal background")
				}
			}
			if !found {
				t.Fatal("missing delay button")
			}
		}
	}
}

func TestDelayPaletteFollowsTerminalBackgroundHint(t *testing.T) {
	for _, test := range []struct {
		hint string
		dark bool
	}{{"15;0", true}, {"0;15", false}, {"15;default;0", true}, {"0;231", false}, {"15;232", true}, {"", false}, {"garbage", false}, {"15;999", false}} {
		t.Setenv("COLORFGBG", test.hint)
		if New(fixture()).Dark != test.dark {
			t.Fatalf("incorrect terminal palette for COLORFGBG=%s", test.hint)
		}
	}
}
