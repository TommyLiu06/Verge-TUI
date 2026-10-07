package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
	"mihomo-tui/internal/app"
	"mihomo-tui/internal/state"
)

type Field struct {
	Key, Label, Text string
	Options          []string
}
type Choice struct {
	Label  string
	Action app.Action
}
type Modal struct {
	Title, Kind string
	Fields      []Field
	Choices     []Choice
	Body        []string
	Scroll      int
}
type Model struct {
	View       app.View
	Tab        int
	Scroll     [3]int
	MaxScroll  int
	Focus      string
	URL        string
	Modal      *Modal
	Frame      *Frame
	Collapsed  map[string]bool
	TestURL    map[string]string
	NetworkTab string
	Busy       bool
	Message    string
	Quit       bool
	MouseDown  bool
	Pasting    bool
}

func New(v app.View) *Model {
	return &Model{View: v, Collapsed: map[string]bool{}, TestURL: map[string]string{}, NetworkTab: "system", Message: "Ready"}
}
func (m *Model) Input() *string {
	if m.Focus == Key(app.Action{Kind: "input", ID: "url"}) && m.Modal == nil {
		return &m.URL
	}
	if m.Modal != nil {
		for i := range m.Modal.Fields {
			field := &m.Modal.Fields[i]
			if m.Focus == Key(app.Action{Kind: "input", ID: field.Key}) && len(field.Options) == 0 {
				return &field.Text
			}
		}
	}
	return nil
}
func (m *Model) appendText(text string) {
	if input := m.Input(); input != nil && len(*input)+len(text) <= 8192 {
		*input += Clean(text)
	}
}
func (m *Model) FocusNext(delta int) {
	if m.Frame == nil || len(m.Frame.Hits) == 0 {
		return
	}
	var keys []string
	seen := map[string]bool{}
	for _, h := range m.Frame.Hits {
		if !seen[h.Key] {
			keys = append(keys, h.Key)
			seen[h.Key] = true
		}
	}
	i := -1
	for n, key := range keys {
		if key == m.Focus {
			i = n
			break
		}
	}
	i = (i + delta + len(keys)) % len(keys)
	m.Focus = keys[i]
}
func (m *Model) Handle(ev tcell.Event) *app.Action {
	switch e := ev.(type) {
	case *tcell.EventPaste:
		m.Pasting = e.Start()
	case *tcell.EventClipboard:
		m.appendText(string(e.Data()))
	case *tcell.EventMouse:
		x, y := e.Position()
		buttons := e.Buttons()
		if buttons&tcell.WheelDown != 0 {
			m.scroll(3)
			return nil
		}
		if buttons&tcell.WheelUp != 0 {
			m.scroll(-3)
			return nil
		}
		pressed := buttons&tcell.Button1 != 0
		if pressed && !m.MouseDown && m.Frame != nil {
			m.MouseDown = true
			for i := len(m.Frame.Hits) - 1; i >= 0; i-- {
				hit := m.Frame.Hits[i]
				if hit.Rect.Contains(x, y) {
					m.Focus = hit.Key
					return m.Activate(hit.Action)
				}
			}
		}
		m.MouseDown = pressed
	case *tcell.EventKey:
		if e.Key() == tcell.KeyCtrlC {
			m.Quit = true
			return nil
		}
		if m.Pasting {
			if e.Key() == tcell.KeyRune {
				m.appendText(e.Str())
			}
			return nil
		}
		if e.Key() == tcell.KeyTab {
			delta := 1
			if e.Modifiers()&tcell.ModShift != 0 {
				delta = -1
			}
			m.FocusNext(delta)
			return nil
		}
		if e.Key() == tcell.KeyBacktab {
			m.FocusNext(-1)
			return nil
		}
		if e.Key() == tcell.KeyEscape {
			m.Modal = nil
			m.Focus = ""
			return nil
		}
		if e.Key() == tcell.KeyEnter {
			if input := m.Input(); input != nil {
				if m.Modal == nil {
					return m.Activate(app.Action{Kind: "import"})
				}
				return m.Activate(app.Action{Kind: "submit"})
			}
			if m.Frame != nil {
				for _, h := range m.Frame.Hits {
					if h.Key == m.Focus {
						return m.Activate(h.Action)
					}
				}
			}
			return nil
		}
		if input := m.Input(); input != nil {
			switch e.Key() {
			case tcell.KeyBackspace:
				if len(*input) > 0 {
					_, size := utf8.DecodeLastRuneInString(*input)
					*input = (*input)[:len(*input)-size]
				}
			case tcell.KeyCtrlU:
				*input = ""
			case tcell.KeyRune:
				m.appendText(e.Str())
			}
			return nil
		}
		if e.Key() == tcell.KeyUp || e.Key() == tcell.KeyPgUp {
			step := 1
			if e.Key() == tcell.KeyPgUp {
				step = 10
			}
			m.scroll(-step)
			return nil
		}
		if e.Key() == tcell.KeyDown || e.Key() == tcell.KeyPgDn {
			step := 1
			if e.Key() == tcell.KeyPgDn {
				step = 10
			}
			m.scroll(step)
			return nil
		}
		if e.Key() == tcell.KeyLeft || e.Key() == tcell.KeyRight {
			if m.Modal != nil {
				for _, field := range m.Modal.Fields {
					if len(field.Options) > 0 && m.Focus == Key(app.Action{Kind: "input", ID: field.Key}) {
						return m.Activate(app.Action{Kind: "input", ID: field.Key})
					}
				}
			}
			return nil
		}
		if e.Key() == tcell.KeyRune {
			switch e.Str() {
			case "q":
				m.Quit = true
			case "1", "2", "3":
				m.Tab = int(e.Str()[0] - '1')
				m.Focus = ""
			case "r":
				return &app.Action{Kind: "refresh"}
			case "n":
				if m.Tab == 2 {
					m.newProfile()
				}
			case "i":
				if m.Tab == 2 {
					m.Focus = Key(app.Action{Kind: "input", ID: "url"})
				}
			case " ":
				if m.Frame != nil {
					for _, h := range m.Frame.Hits {
						if h.Key == m.Focus {
							return m.Activate(h.Action)
						}
					}
				}
			}
		}
	}
	return nil
}
func (m *Model) Activate(a app.Action) *app.Action {
	switch a.Kind {
	case "navigate":
		m.Tab, _ = strconv.Atoi(a.ID)
		m.Focus = ""
		m.Modal = nil
	case "input":
		m.Focus = Key(a)
		if m.Modal != nil {
			for i := range m.Modal.Fields {
				f := &m.Modal.Fields[i]
				if f.Key == a.ID && len(f.Options) > 0 {
					n := 0
					for j, s := range f.Options {
						if s == f.Text {
							n = j + 1
							break
						}
					}
					f.Text = f.Options[n%len(f.Options)]
					break
				}
			}
		}
	case "new":
		m.newProfile()
	case "close":
		m.Modal = nil
		m.Focus = ""
	case "clear":
		m.URL = ""
	case "paste":
		m.Focus = Key(app.Action{Kind: "input", ID: "url"})
	case "network-tab":
		m.NetworkTab = a.ID
	case "collapse":
		m.Collapsed[a.ID] = !m.Collapsed[a.ID]
	case "import":
		if !m.Busy && strings.TrimSpace(m.URL) != "" {
			return &app.Action{Kind: "profile-import", Profile: state.Profile{URL: strings.TrimSpace(m.URL)}}
		}
	case "submit":
		return m.submit()
	case "home-groups":
		modal := &Modal{Title: "Group"}
		for _, g := range app.Groups(m.View) {
			if g.Type == "Selector" || g.Type == "URLTest" {
				modal.Choices = append(modal.Choices, Choice{g.Name, app.Action{Kind: "home-group", ID: g.Name}})
			}
		}
		m.Modal = modal
		m.Focus = ""
	case "home-nodes":
		g := app.HomeGroup(m.View)
		modal := &Modal{Title: "Proxy"}
		for _, name := range g.All {
			modal.Choices = append(modal.Choices, Choice{name, app.Action{Kind: "node-select", ID: g.Name, Node: name}})
		}
		m.Modal = modal
		m.Focus = ""
	case "cores":
		modal := &Modal{Title: "Core Version"}
		for _, c := range m.View.Cores {
			modal.Choices = append(modal.Choices, Choice{c.ID, app.Action{Kind: "core-use", ID: c.ID}})
		}
		modal.Choices = append(modal.Choices, Choice{"Install Core", app.Action{Kind: "core-form"}})
		m.Modal = modal
		m.Focus = ""
	case "core-form":
		m.Modal = &Modal{Title: "Install Core", Kind: "core-install", Fields: []Field{{Key: "tag", Label: "Release Tag", Text: "latest"}}}
		m.Focus = Key(app.Action{Kind: "input", ID: "tag"})
	case "test-url":
		m.Modal = &Modal{Title: "Delay check URL", Kind: "test-url", Fields: []Field{{Key: "group", Label: "Group", Text: a.ID}, {Key: "testurl", Label: "Delay check URL", Text: m.TestURL[a.ID]}}}
		m.Focus = Key(app.Action{Kind: "input", ID: "testurl"})
	case "service-info":
		m.Modal = &Modal{Title: "Auto Launch", Body: []string{"Use the systemd service to keep the core running.", "Generate a unit with: mihomo-tui service --mode user", "Enable it with: systemctl --user enable --now mihomo-tui", "Boot startup requires a permitted user session or lingering.", "System mode uses: mihomo-tui service --mode system"}}
	case "environment":
		m.Modal = &Modal{Title: "Proxy Environment", Body: strings.Split(app.ProxyEnvironment(m.View.Settings), "\n")}
	case "copy-environment":
		m.Message = "Proxy environment copied to clipboard"
	default:
		if !m.Busy {
			m.Modal = nil
			m.Focus = ""
			if a.Kind == "delay" {
				a.Value = m.TestURL[a.ID]
			}
			return &a
		}
	}
	return nil
}
func (m *Model) scroll(delta int) {
	if m.Modal != nil {
		height := 0
		if m.Frame != nil {
			height = m.Frame.H
		}
		limit := max(0, len(m.Modal.Choices)*2-max(1, height-12))
		m.Modal.Scroll = max(0, min(limit, m.Modal.Scroll+delta))
		return
	}
	m.Scroll[m.Tab] = max(0, min(m.MaxScroll, m.Scroll[m.Tab]+delta))
}
func (m *Model) newProfile() {
	m.Modal = &Modal{Title: "Create Profile", Kind: "profile-new", Fields: []Field{{Key: "type", Label: "Type", Text: "Remote", Options: []string{"Remote", "Local"}}, {Key: "name", Label: "Name"}, {Key: "description", Label: "Descriptions"}, {Key: "source", Label: "Subscription URL"}, {Key: "agent", Label: "User Agent"}, {Key: "timeout", Label: "HTTP Request Timeout", Text: "30"}}}
	m.Focus = Key(app.Action{Kind: "input", ID: "type"})
}
func (m *Model) submit() *app.Action {
	if m.Modal == nil || m.Busy {
		return nil
	}
	values := map[string]string{}
	for _, f := range m.Modal.Fields {
		values[f.Key] = strings.TrimSpace(f.Text)
	}
	switch m.Modal.Kind {
	case "profile-new":
		timeout, err := strconv.Atoi(values["timeout"])
		if err != nil || timeout < 1 || timeout > 300 {
			m.Message = "HTTP Request Timeout must be between 1 and 300 seconds"
			return nil
		}
		p := state.Profile{Name: values["name"], Type: strings.ToLower(values["type"]), Description: values["description"], UserAgent: values["agent"], Timeout: timeout}
		if p.Type == "remote" {
			p.URL = values["source"]
			if p.URL == "" {
				m.Message = "Enter a subscription URL"
				return nil
			}
		}
		file := ""
		if p.Type == "local" {
			file = values["source"]
		}
		m.Modal = nil
		return &app.Action{Kind: "profile-new", Profile: p, File: file}
	case "core-install":
		m.Modal = nil
		return &app.Action{Kind: "core-install", Value: values["tag"]}
	case "test-url":
		m.TestURL[values["group"]] = values["testurl"]
		m.Modal = nil
		m.Message = "Delay check URL saved"
	}
	return nil
}
func (m *Model) Complete(v app.View, a app.Action, err error) {
	m.Busy = false
	m.View = v
	if err != nil {
		m.Message = "Error: " + err.Error()
		m.Modal = &Modal{Title: "Action Failed", Body: strings.Split(err.Error(), "\n")}
	} else {
		m.Message = "Ready"
		if a.Kind == "profile-import" {
			m.URL = ""
			m.Message = "Profile imported"
		}
		if a.Kind == "profile-select" {
			m.Message = "Profile Switched"
		}
		if a.Kind == "delay" {
			m.Message = "Delay check completed"
		}
	}
}
func bytesString(n uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(n)
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}
