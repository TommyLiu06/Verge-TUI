package tui

import (
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
)

func (m *Model) cursor(value string) int {
	pos, ok := m.Cursors[m.Focus]
	if !ok {
		return len(value)
	}
	return max(0, min(pos, len(value)))
}
func previousBoundary(value string, pos int) int {
	it := graphemes.FromString(value)
	previous, offset := 0, 0
	for it.Next() {
		offset += len(it.Value())
		if offset >= pos {
			return previous
		}
		previous = offset
	}
	return previous
}
func nextBoundary(value string, pos int) int {
	it := graphemes.FromString(value)
	offset := 0
	for it.Next() {
		offset += len(it.Value())
		if offset > pos {
			return offset
		}
	}
	return len(value)
}
func (m *Model) editInput(e *tcell.EventKey, input *string) {
	pos := m.cursor(*input)
	switch e.Key() {
	case tcell.KeyLeft:
		pos = previousBoundary(*input, pos)
	case tcell.KeyRight:
		pos = nextBoundary(*input, pos)
	case tcell.KeyHome, tcell.KeyCtrlA:
		pos = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		pos = len(*input)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		previous := previousBoundary(*input, pos)
		*input = (*input)[:previous] + (*input)[pos:]
		pos = previous
	case tcell.KeyDelete:
		*input = (*input)[:pos] + (*input)[nextBoundary(*input, pos):]
	case tcell.KeyCtrlU:
		*input, pos = "", 0
	case tcell.KeyCtrlW:
		start := pos
		for start > 0 {
			r, size := utf8.DecodeLastRuneInString((*input)[:start])
			if !unicode.IsSpace(r) {
				break
			}
			start -= size
		}
		for start > 0 {
			r, size := utf8.DecodeLastRuneInString((*input)[:start])
			if unicode.IsSpace(r) {
				break
			}
			start -= size
		}
		*input = (*input)[:start] + (*input)[pos:]
		pos = start
	case tcell.KeyUp:
		m.FocusNext(-1)
		return
	case tcell.KeyDown:
		m.FocusNext(1)
		return
	case tcell.KeyRune:
		m.appendText(e.Str())
		return
	}
	m.Cursors[m.Focus] = pos
}
func (m *Model) FrameHeight() int {
	if m.Frame == nil {
		return 42
	}
	return m.Frame.H
}
func (m *Model) visibleFields() []Field {
	if m.Modal == nil {
		return nil
	}
	local := false
	for _, f := range m.Modal.Fields {
		if f.Key == "type" && f.Text == "Local" {
			local = true
		}
	}
	var fields []Field
	for _, f := range m.Modal.Fields {
		if !local || f.Key != "agent" && f.Key != "timeout" {
			fields = append(fields, f)
		}
	}
	return fields
}
func (m *Model) modalContentHeight() int {
	return len(m.visibleFields())*2 + len(m.Modal.Choices)*2 + len(m.Modal.Body)
}
func (m *Model) modalViewport() int {
	return max(1, min(m.FrameHeight()-4, max(9, 7+m.modalContentHeight()))-5)
}
func (m *Model) focusTargets() []Hit {
	if m.Modal == nil {
		return m.Frame.Hits
	}
	var hits []Hit
	for _, f := range m.visibleFields() {
		if m.fieldDisabled(f.Key) {
			continue
		}
		a := app.Action{Kind: "input", ID: f.Key}
		hits = append(hits, Hit{Action: a, Key: Key(a)})
	}
	for _, c := range m.Modal.Choices {
		hits = append(hits, Hit{Action: c.Action, Key: Key(c.Action)})
	}
	if len(m.Modal.Fields) > 0 {
		a := app.Action{Kind: "submit"}
		hits = append(hits, Hit{Action: a, Key: Key(a)})
	}
	if m.Modal.Title == "Proxy Environment" {
		a := app.Action{Kind: "copy-environment"}
		hits = append(hits, Hit{Action: a, Key: Key(a)})
	}
	a := app.Action{Kind: "close"}
	return append(hits, Hit{Action: a, Key: Key(a)})
}
func (m *Model) revealFocus() {
	if m.Modal == nil {
		return
	}
	pos := -1
	fields := m.visibleFields()
	for i, f := range fields {
		if m.Focus == Key(app.Action{Kind: "input", ID: f.Key}) {
			pos = i*2 + 1
		}
	}
	for i, c := range m.Modal.Choices {
		if m.Focus == Key(c.Action) {
			pos = len(fields)*2 + len(m.Modal.Body) + i*2
		}
	}
	if pos < 0 {
		return
	}
	if pos < m.Modal.Scroll {
		m.Modal.Scroll = max(0, pos-1)
	}
	if pos >= m.Modal.Scroll+m.modalViewport() {
		m.Modal.Scroll = pos - m.modalViewport() + 1
	}
}
func (m *Model) moveFocus(dx, dy int) {
	if m.Modal != nil {
		if len(m.Modal.Fields)+len(m.Modal.Choices) == 0 {
			m.scroll(dy)
			return
		}
		m.FocusNext(dx + dy)
		return
	}
	if m.Frame == nil {
		return
	}
	var current *Hit
	for i := range m.Frame.Hits {
		h := &m.Frame.Hits[i]
		if h.Key == m.Focus && (current == nil || h.Rect.W*h.Rect.H < current.Rect.W*current.Rect.H) {
			current = h
		}
	}
	if current == nil {
		m.FocusNext(1)
		return
	}
	cx, cy := current.Rect.X+current.Rect.W/2, current.Rect.Y+current.Rect.H/2
	best, score := "", math.MaxInt
	for _, h := range m.Frame.Hits {
		if h.Key == m.Focus {
			continue
		}
		x, y := h.Rect.X+h.Rect.W/2-cx, h.Rect.Y+h.Rect.H/2-cy
		primary, secondary := x*dx+y*dy, abs(x*dy-y*dx)
		if primary <= 0 {
			continue
		}
		candidate := primary*2 + secondary*4
		if candidate < score {
			best, score = h.Key, candidate
		}
	}
	if best != "" {
		m.Focus = best
	} else {
		m.scroll(dy * 3)
	}
}
func abs(n int) int { return max(n, -n) }
func (m *Model) cycleOption(key string, delta int) {
	for i := range m.Modal.Fields {
		f := &m.Modal.Fields[i]
		if f.Key != key || len(f.Options) == 0 {
			continue
		}
		for j, option := range f.Options {
			if f.Text == option {
				f.Text = f.Options[(j+delta+len(f.Options))%len(f.Options)]
				return
			}
		}
	}
}
func (m *Model) closeModal() {
	if m.Modal != nil && m.Modal.Kind == "help" {
		m.Modal, m.Focus = m.HelpBack, m.HelpFocus
		m.HelpBack = nil
		return
	}
	m.Modal, m.Focus = nil, ""
}
func (m *Model) help() {
	if m.Modal != nil && m.Modal.Kind == "help" {
		return
	}
	m.HelpBack, m.HelpFocus = m.Modal, m.Focus
	m.Modal = &Modal{Title: "Keyboard Shortcuts", Kind: "help", Body: []string{
		"Tab / Shift+Tab or arrows: move focus", "Enter / Space: activate; Left / Right: change options",
		"PageUp / PageDown: scroll; Home / End: page edges", "1 / 2 / 3 / 4: Home / Proxies / Profiles / Settings",
		"r: Refresh; n: New profile; i: Profile URL", "Input: arrows, Home/End, Backspace, Delete",
		"Ctrl+A/E: input start/end; Ctrl+U/W: clear / delete word", "Ctrl+S: save dialog; Esc: back; q / Ctrl+C: quit",
	}}
	m.Focus = Key(app.Action{Kind: "close"})
}
