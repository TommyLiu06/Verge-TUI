package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/profile"
)

type editChange struct {
	Line                                     int
	Before, After                            []string
	BeforeRow, BeforeCol, AfterRow, AfterCol int
	BeforeRevision, AfterRevision            int
}
type Editor struct {
	ID, Name, Revision                                   string
	Lines                                                []string
	Row, Col, Top, Left                                  int
	Changes                                              []editChange
	HistoryIndex, HistoryBytes, Counter, CurrentRevision int
	Paste                                                strings.Builder
	Pasting                                              bool
	Size                                                 int
	Error                                                string
	PasteTooLarge                                        bool
}

func newEditor(doc *app.ProfileDocument) *Editor {
	return &Editor{ID: doc.Profile.ID, Name: doc.Profile.Name, Revision: doc.Revision, Size: len(strings.ReplaceAll(string(doc.Data), "\r\n", "\n")), Lines: strings.Split(strings.ReplaceAll(string(doc.Data), "\r\n", "\n"), "\n")}
}
func (e *Editor) dirty() bool { return e.CurrentRevision != 0 }
func editorText(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}
func (e *Editor) replace(startRow, startCol, endRow, endCol int, text string) {
	text = editorText(text)
	removed := 0
	for row := startRow; row <= endRow; row++ {
		from, to := 0, len(e.Lines[row])
		if row == startRow {
			from = startCol
		}
		if row == endRow {
			to = endCol
		}
		removed += to - from
		if row < endRow {
			removed++
		}
	}
	if e.Size-removed+len(text) > profile.MaxSize {
		e.Error = "Profile exceeds 16 MiB"
		return
	}
	e.Error = ""
	before := append([]string(nil), e.Lines[startRow:endRow+1]...)
	after := strings.Split(e.Lines[startRow][:startCol]+text+e.Lines[endRow][endCol:], "\n")
	insert := strings.Split(text, "\n")
	row, col := startRow+len(insert)-1, len(insert[len(insert)-1])
	if len(insert) == 1 {
		col += startCol
	}
	e.Counter++
	change := editChange{startRow, before, after, e.Row, e.Col, row, col, e.CurrentRevision, e.Counter}
	e.Changes = e.Changes[:e.HistoryIndex]
	e.Changes = append(e.Changes, change)
	e.HistoryIndex = len(e.Changes)
	e.apply(change, false)
	e.HistoryBytes = 0
	for _, c := range e.Changes {
		for _, line := range c.Before {
			e.HistoryBytes += len(line)
		}
		for _, line := range c.After {
			e.HistoryBytes += len(line)
		}
	}
	for len(e.Changes) > 0 && (len(e.Changes) > 200 || e.HistoryBytes > 16<<20) {
		c := e.Changes[0]
		for _, line := range c.Before {
			e.HistoryBytes -= len(line)
		}
		for _, line := range c.After {
			e.HistoryBytes -= len(line)
		}
		e.Changes = append([]editChange(nil), e.Changes[1:]...)
		e.HistoryIndex--
	}
}
func (e *Editor) apply(c editChange, undo bool) {
	remove, add := c.Before, c.After
	e.Row, e.Col, e.CurrentRevision = c.AfterRow, c.AfterCol, c.AfterRevision
	if undo {
		remove, add = c.After, c.Before
		e.Row, e.Col, e.CurrentRevision = c.BeforeRow, c.BeforeCol, c.BeforeRevision
	}
	for _, line := range remove {
		e.Size -= len(line) + 1
	}
	for _, line := range add {
		e.Size += len(line) + 1
	}
	if len(remove) == 1 && len(add) == 1 {
		e.Lines[c.Line] = add[0]
		return
	}
	next := make([]string, 0, len(e.Lines)-len(remove)+len(add))
	next = append(next, e.Lines[:c.Line]...)
	next = append(next, add...)
	next = append(next, e.Lines[c.Line+len(remove):]...)
	e.Lines = next
}
func (e *Editor) insert(text string) {
	if text != "" {
		e.replace(e.Row, e.Col, e.Row, e.Col, text)
	}
}
func (e *Editor) moveVertical(delta int) {
	column := displaywidth.String(strings.ReplaceAll(e.Lines[e.Row][:e.Col], "\t", "  "))
	e.Row = max(0, min(len(e.Lines)-1, e.Row+delta))
	e.Col = columnByte(e.Lines[e.Row], column)
}
func columnByte(line string, column int) int {
	pos, width := 0, 0
	for pos < len(line) {
		next := nextBoundary(line, pos)
		w := displaywidth.String(line[pos:next])
		if line[pos:next] == "\t" {
			w = 2
		}
		if width+w > column {
			break
		}
		width += w
		pos = next
	}
	return pos
}
func (m *Model) saveEditor() *app.Action {
	if m.Editor == nil || m.Busy {
		return nil
	}
	e := m.Editor
	data := []byte(strings.Join(e.Lines, "\n"))
	if len(data) > profile.MaxSize {
		m.Message = "Profile exceeds 16 MiB"
		return nil
	}
	if _, err := profile.Parse(data); err != nil {
		m.Message = "Error: " + err.Error()
		return nil
	}
	m.PendingEditor, e.Pasting = m.Editor, false
	m.Editor = nil
	return &app.Action{Kind: "profile-file-save", ID: e.ID, Value: e.Revision, Data: data}
}
func (m *Model) closeEditor() {
	if m.Editor == nil {
		return
	}
	if !m.Editor.dirty() {
		m.Editor = nil
		m.Focus = ""
		return
	}
	m.Modal = &Modal{Title: "Unsaved Changes", Body: []string{"Discard unsaved changes?"}, Choices: []Choice{{"Cancel", app.Action{Kind: "close"}}, {"Discard", app.Action{Kind: "editor-discard"}}}}
	m.Focus = Key(app.Action{Kind: "close"})
}
func (m *Model) handleEditor(ev tcell.Event) *app.Action {
	e := m.Editor
	switch v := ev.(type) {
	case *tcell.EventPaste:
		e.Pasting = v.Start()
		if v.Start() {
			e.PasteTooLarge = false
			e.Paste.Reset()
		}
		if !v.Start() {
			if !e.PasteTooLarge {
				e.insert(e.Paste.String())
			} else {
				m.Message = "Profile exceeds 16 MiB"
			}
			e.Paste.Reset()
		}
	case *tcell.EventClipboard:
		e.insert(string(v.Data()))
	case *tcell.EventMouse:
		buttons := v.Buttons()
		if buttons&mouseWheelMask != 0 {
			m.Focus = ""
			m.MouseDown = buttons&tcell.Button1 != 0
			if buttons&tcell.WheelDown != 0 {
				e.Top = min(max(0, len(e.Lines)-1), e.Top+3)
			}
			if buttons&tcell.WheelUp != 0 {
				e.Top = max(0, e.Top-3)
			}
			return nil
		}
		pressed := buttons&tcell.Button1 != 0
		if pressed && !m.MouseDown && m.Frame != nil {
			x, y := v.Position()
			for i := len(m.Frame.Hits) - 1; i >= 0; i-- {
				h := m.Frame.Hits[i]
				if h.Rect.Contains(x, y) {
					m.Focus = h.Key
					m.MouseDown = true
					if h.Action.Kind == "editor-input" {
						e.Row = min(len(e.Lines)-1, e.Top+y-h.Rect.Y)
						e.Col = columnByte(e.Lines[e.Row], e.Left+max(0, x-h.Rect.X))
						return nil
					}
					return m.Activate(h.Action)
				}
			}
		}
		m.MouseDown = pressed
	case *tcell.EventKey:
		if e.Pasting {
			if e.PasteTooLarge {
				return nil
			}
			if e.Paste.Len()+len(v.Str())+1 > profile.MaxSize {
				e.PasteTooLarge = true
				e.Paste.Reset()
				return nil
			}
			switch v.Key() {
			case tcell.KeyRune:
				e.Paste.WriteString(v.Str())
			case tcell.KeyEnter:
				e.Paste.WriteByte('\n')
			case tcell.KeyTab:
				e.Paste.WriteByte('\t')
			}
			return nil
		}
		switch v.Key() {
		case tcell.KeyCtrlS:
			return m.saveEditor()
		case tcell.KeyEscape:
			m.closeEditor()
			return nil
		case tcell.KeyF1:
			m.help()
			return nil
		case tcell.KeyCtrlC:
			m.closeEditor()
			return nil
		case tcell.KeyCtrlZ:
			if e.HistoryIndex > 0 {
				e.HistoryIndex--
				e.apply(e.Changes[e.HistoryIndex], true)
			}
		case tcell.KeyCtrlY:
			if e.HistoryIndex < len(e.Changes) {
				e.apply(e.Changes[e.HistoryIndex], false)
				e.HistoryIndex++
			}
		default:
			if m.Focus != Key(app.Action{Kind: "editor-input"}) {
				if v.Key() == tcell.KeyTab || v.Key() == tcell.KeyBacktab {
					m.FocusNext(1)
					return nil
				}
				if v.Key() == tcell.KeyEnter && m.Frame != nil {
					for _, h := range m.Frame.Hits {
						if h.Key == m.Focus {
							return m.Activate(h.Action)
						}
					}
				}
				return nil
			}
			switch v.Key() {
			case tcell.KeyRune:
				e.insert(v.Str())
			case tcell.KeyTab:
				e.insert("  ")
			case tcell.KeyBacktab:
				m.FocusNext(1)
				return nil
			case tcell.KeyEnter:
				prefix := e.Lines[e.Row][:e.Col]
				indent := len(prefix) - len(strings.TrimLeft(prefix, " "))
				e.insert("\n" + strings.Repeat(" ", indent))
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if e.Col > 0 {
					e.replace(e.Row, previousBoundary(e.Lines[e.Row], e.Col), e.Row, e.Col, "")
				} else if e.Row > 0 {
					e.replace(e.Row-1, len(e.Lines[e.Row-1]), e.Row, 0, "")
				}
			case tcell.KeyDelete:
				if e.Col < len(e.Lines[e.Row]) {
					e.replace(e.Row, e.Col, e.Row, nextBoundary(e.Lines[e.Row], e.Col), "")
				} else if e.Row < len(e.Lines)-1 {
					e.replace(e.Row, e.Col, e.Row+1, 0, "")
				}
			case tcell.KeyLeft:
				if e.Col > 0 {
					e.Col = previousBoundary(e.Lines[e.Row], e.Col)
				} else if e.Row > 0 {
					e.Row--
					e.Col = len(e.Lines[e.Row])
				}
			case tcell.KeyRight:
				if e.Col < len(e.Lines[e.Row]) {
					e.Col = nextBoundary(e.Lines[e.Row], e.Col)
				} else if e.Row < len(e.Lines)-1 {
					e.Row++
					e.Col = 0
				}
			case tcell.KeyUp:
				e.moveVertical(-1)
			case tcell.KeyDown:
				e.moveVertical(1)
			case tcell.KeyHome:
				if v.Modifiers()&tcell.ModCtrl != 0 {
					e.Row = 0
				}
				e.Col = 0
			case tcell.KeyEnd:
				if v.Modifiers()&tcell.ModCtrl != 0 {
					e.Row = len(e.Lines) - 1
				}
				e.Col = len(e.Lines[e.Row])
			case tcell.KeyPgUp:
				e.moveVertical(-max(1, m.FrameHeight()-8))
			case tcell.KeyPgDn:
				e.moveVertical(max(1, m.FrameHeight()-8))
			}
		}
		m.revealEditor()
	}
	if e.Error != "" {
		m.Message = e.Error
	}
	return nil
}
func (m *Model) revealEditor() {
	e := m.Editor
	if e == nil {
		return
	}
	view := max(1, m.FrameHeight()-8)
	if e.Row < e.Top {
		e.Top = e.Row
	}
	if e.Row >= e.Top+view {
		e.Top = e.Row - view + 1
	}
	column := displaywidth.String(strings.ReplaceAll(e.Lines[e.Row][:e.Col], "\t", "  "))
	width := 70
	if m.Frame != nil {
		width = m.Frame.W - 12
	}
	if column < e.Left {
		e.Left = column
	}
	if column >= e.Left+width {
		e.Left = column - width + 1
	}
}
func (m *Model) renderEditor(f *Frame) {
	e := m.Editor
	r := Rect{2, 1, f.W - 4, f.H - 3}
	f.Hits = nil
	f.Clip = Rect{0, 0, f.W, f.H}
	title := m.tr("Edit File") + " · " + e.Name
	if e.dirty() {
		title += " *"
	}
	f.Box(r, title)
	f.Button(Rect{r.X + r.W - 22, r.Y + 1, 10, 1}, m.tr("Save"), app.Action{Kind: "editor-save"}, false)
	f.Button(Rect{r.X + r.W - 12, r.Y + 1, 10, 1}, m.tr("Close"), app.Action{Kind: "editor-close"}, false)
	f.Clip = Rect{r.X + 1, r.Y + 3, r.W - 2, r.H - 5}
	area := Rect{r.X + 7, r.Y + 3, r.W - 9, r.H - 5}
	f.Add(area, app.Action{Kind: "editor-input"})
	for i := 0; i < area.H && e.Top+i < len(e.Lines); i++ {
		row := e.Top + i
		line := e.Lines[row]
		f.Line(r.X+1, area.Y+i, 5, fmt.Sprintf("%5d", row+1), f.Theme.Card.Foreground(f.Theme.Muted))
		start := columnByte(line, e.Left)
		visible := strings.ReplaceAll(line[start:], "\t", "  ")
		style := f.Theme.Card
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			style = style.Foreground(f.Theme.Muted)
		}
		f.Line(area.X, area.Y+i, area.W, visible, style)
		if row == e.Row && e.Col >= start && m.Focus == Key(app.Action{Kind: "editor-input"}) {
			x := area.X + displaywidth.String(strings.ReplaceAll(line[start:e.Col], "\t", "  "))
			ch := " "
			if e.Col < len(line) {
				ch = line[e.Col:nextBoundary(line, e.Col)]
				if ch == "\t" {
					ch = " "
				}
			}
			f.Put(x, area.Y+i, ch, style.Reverse(true).Bold(true))
		}
	}
	f.Clip = Rect{0, 0, f.W, f.H}
	f.Line(r.X+2, r.Y+r.H-2, r.W-4, fmt.Sprintf("Ln %d, Col %d  ·  YAML  ·  Ctrl+S Save  Ctrl+Z Undo  Ctrl+Y Redo  Esc Close", e.Row+1, displaywidth.String(e.Lines[e.Row][:e.Col])+1), f.Theme.Card.Foreground(f.Theme.Muted))
}
