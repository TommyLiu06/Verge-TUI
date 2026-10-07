package tui

import (
	"strings"
	"unicode"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
	"mihomo-tui/internal/app"
)

var (
	background = color.NewHexColor(0x10131c)
	panel      = color.NewHexColor(0x191e2c)
	border     = color.NewHexColor(0x31394c)
	accent     = color.NewHexColor(0xa997ff)
	text       = color.NewHexColor(0xe3e7f1)
	muted      = color.NewHexColor(0x8c96aa)
	green      = color.NewHexColor(0x70d6a3)
	baseStyle  = tcell.StyleDefault.Foreground(text).Background(background)
	cardStyle  = tcell.StyleDefault.Foreground(text).Background(panel)
)

type Rect struct{ X, Y, W, H int }

func (r Rect) Contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }
func intersection(a, b Rect) Rect {
	x, y := max(a.X, b.X), max(a.Y, b.Y)
	return Rect{x, y, max(0, min(a.X+a.W, b.X+b.W)-x), max(0, min(a.Y+a.H, b.Y+b.H)-y)}
}

type Cell struct {
	Text  string
	Style tcell.Style
	Width int
}
type Hit struct {
	Rect   Rect
	Action app.Action
	Key    string
}
type Frame struct {
	W, H  int
	Cells []Cell
	Hits  []Hit
	Clip  Rect
	Focus string
}

func NewFrame(w, h int) *Frame {
	f := &Frame{W: w, H: h, Cells: make([]Cell, w*h), Clip: Rect{0, 0, w, h}}
	for i := range f.Cells {
		f.Cells[i] = Cell{" ", baseStyle, 1}
	}
	return f
}
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x202e || r == 0x202d || r >= 0x2066 && r <= 0x2069 {
			return ' '
		}
		return r
	}, s)
}
func (f *Frame) Put(x, y int, s string, style tcell.Style) {
	it := graphemes.FromString(Clean(s))
	for it.Next() {
		cluster := it.Value()
		w := displaywidth.String(cluster)
		if w == 0 {
			continue
		}
		if f.Clip.Contains(x, y) && f.Clip.Contains(x+w-1, y) && x >= 0 && x+w <= f.W && y >= 0 && y < f.H {
			f.Cells[y*f.W+x] = Cell{cluster, style, w}
			for i := 1; i < w; i++ {
				f.Cells[y*f.W+x+i] = Cell{"", style, 0}
			}
		}
		x += w
	}
}
func (f *Frame) Line(x, y, width int, s string, style tcell.Style) {
	f.Put(x, y, displaywidth.TruncateString(Clean(s), max(0, width), "…"), style)
}
func (f *Frame) Fill(r Rect, style tcell.Style) {
	r = intersection(intersection(r, f.Clip), Rect{0, 0, f.W, f.H})
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			f.Cells[y*f.W+x] = Cell{" ", style, 1}
		}
	}
}
func (f *Frame) Box(r Rect, title string) {
	f.Fill(r, cardStyle)
	s := cardStyle.Foreground(border)
	f.Put(r.X, r.Y, "╭"+strings.Repeat("─", max(0, r.W-2))+"╮", s)
	f.Put(r.X, r.Y+r.H-1, "╰"+strings.Repeat("─", max(0, r.W-2))+"╯", s)
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		f.Put(r.X, y, "│", s)
		f.Put(r.X+r.W-1, y, "│", s)
	}
	f.Line(r.X+2, r.Y+1, r.W-4, title, cardStyle.Bold(true))
}
func Key(a app.Action) string { return a.Kind + "|" + a.ID + "|" + a.Node + "|" + a.Value }
func (f *Frame) Add(r Rect, a app.Action) {
	r = intersection(r, f.Clip)
	if r.W > 0 && r.H > 0 {
		f.Hits = append(f.Hits, Hit{r, a, Key(a)})
	}
}
func (f *Frame) Button(r Rect, label string, a app.Action, selected bool) {
	s := cardStyle.Foreground(muted)
	if selected {
		s = s.Foreground(accent).Bold(true)
	}
	if f.Focus == Key(a) {
		s = s.Background(border).Foreground(text)
	}
	f.Fill(r, s)
	f.Line(r.X+1, r.Y+(r.H-1)/2, r.W-2, "["+label+"]", s)
	f.Add(r, a)
}
func (f *Frame) Plain() string {
	var out strings.Builder
	for y := 0; y < f.H; y++ {
		var line strings.Builder
		for x := 0; x < f.W; x++ {
			line.WriteString(f.Cells[y*f.W+x].Text)
		}
		out.WriteString(strings.TrimRight(line.String(), " "))
		out.WriteByte('\n')
	}
	return out.String()
}
func (f *Frame) Show(s tcell.Screen) {
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			cell := f.Cells[y*f.W+x]
			if cell.Width > 0 {
				s.Put(x, y, cell.Text, cell.Style)
			}
		}
	}
	s.Show()
}
