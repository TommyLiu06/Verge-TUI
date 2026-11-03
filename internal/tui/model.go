package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/state"
	"verge-tui/internal/updater"
)

const mouseWheelMask = tcell.WheelDown | tcell.WheelUp | tcell.WheelLeft | tcell.WheelRight

type Field struct {
	Key, Label, Text string
	Options          []string
}
type Choice struct {
	Label  string
	Action app.Action
}
type Modal struct {
	Title, Kind        string
	Section, ProfileID string
	Fields             []Field
	Choices            []Choice
	Header             []Choice
	Footer             []Choice
	Body               []string
	Scroll             int
	RevealFocus        bool
	List               bool
	Parent             *Modal
	ParentFocus        string
}
type Model struct {
	AppUpdate         updater.Status
	UpdateRequest     *updater.Request
	UpdateInstalling  bool
	UpdateNeedsAdmin  bool
	CoreReleases      []core.ReleaseOption
	CoreReleaseNext   int
	CoreReleaseEpoch  int
	PreferencesEpoch  uint64
	Tasks             map[string]app.Action
	DelayActive       bool
	DelayMessage      string
	Latencies         map[string]app.DelayResult
	SearchError       [7]string
	HiddenColumns     map[string]bool
	TableSort         string
	TableDescending   bool
	TableLeft         int
	RuleSearchCache   ruleSearchCache
	SearchOptions     [7]SearchOptions
	SearchCache       [7]searchCache
	View              app.View
	Tab               int
	Scroll            [7]int
	MaxScroll         int
	Focus             string
	URL               string
	Modal             *Modal
	Frame             *Frame
	Collapsed         map[string]bool
	TestURL           map[string]string
	NetworkTab        string
	Busy              bool
	Message           string
	Quit              bool
	MouseDown         bool
	Pasting           bool
	Cursors           map[string]int
	HelpBack          *Modal
	HelpFocus         string
	Delays            map[string]app.DelayResult
	Dark              bool
	Loaded            bool
	Search            [7]string
	Connections       controller.Connections
	ClosedConnections []controller.Connection
	ConnectionTime    time.Time
	ConnectionClosed  bool
	ConnectionOrder   string
	ConnectionTable   bool
	Rules             controller.Rules
	Logs              []controller.Log
	LogsPaused        bool
	LogsDescending    bool
	LogLevel          string
	PageError         [7]string
	Editor            *Editor
	PendingProfile    *Modal
	PendingEditor     *Editor
	WatchRevision     int
}

func New(v app.View) *Model {
	m := &Model{View: v, Collapsed: map[string]bool{}, TestURL: map[string]string{}, Cursors: map[string]int{}, NetworkTab: "system", Message: "Ready", ConnectionOrder: "Default", LogLevel: "All", Dark: darkTerminal()}
	m.applyPreferences()
	m.AppUpdate = v.Update
	if m.AppUpdate.CurrentVersion == "" {
		m.AppUpdate.CurrentVersion = v.Version
	}
	return m
}
func (m *Model) Input() *string {
	if m.Modal == nil && m.Tab >= 4 && m.Focus == Key(app.Action{Kind: "input", ID: "search"}) {
		return &m.Search[m.Tab]
	}
	if m.Focus == Key(app.Action{Kind: "input", ID: "url"}) && m.Modal == nil {
		return &m.URL
	}
	if m.Modal != nil {
		for i := range m.Modal.Fields {
			field := &m.Modal.Fields[i]
			if !m.fieldDisabled(field.Key) && m.Focus == Key(app.Action{Kind: "input", ID: field.Key}) && len(field.Options) == 0 {
				return &field.Text
			}
		}
	}
	return nil
}
func (m *Model) appendText(text string) {
	text = Clean(text)
	if input := m.Input(); input != nil && len(*input)+len(text) <= 8192 {
		pos := m.cursor(*input)
		*input = (*input)[:pos] + text + (*input)[pos:]
		m.Cursors[m.Focus] = pos + len(text)
		if m.Modal == nil && m.Tab >= 4 && m.Focus == Key(app.Action{Kind: "input", ID: "search"}) {
			m.Scroll[m.Tab] = 0
		}
	}
}
func (m *Model) FocusNext(delta int) {
	if m.Frame == nil {
		return
	}
	var keys []string
	seen := map[string]bool{}
	for _, h := range m.focusTargets() {
		if !seen[h.Key] {
			keys = append(keys, h.Key)
			seen[h.Key] = true
		}
	}
	if len(keys) == 0 {
		return
	}
	i := -1
	for n, key := range keys {
		if key == m.Focus {
			i = n
			break
		}
	}
	if i == -1 {
		if delta > 0 {
			i = 0
		} else {
			i = len(keys) - 1
		}
	} else {
		i = (i + delta + len(keys)) % len(keys)
	}
	m.Focus = keys[i]
	m.revealFocus()
}
func (m *Model) Handle(ev tcell.Event) *app.Action {
	if m.Editor != nil && m.Modal == nil {
		return m.handleEditor(ev)
	}
	switch e := ev.(type) {
	case *tcell.EventPaste:
		m.Pasting = e.Start()
	case *tcell.EventClipboard:
		m.appendText(string(e.Data()))
	case *tcell.EventMouse:
		x, y := e.Position()
		buttons := e.Buttons()
		if buttons&mouseWheelMask != 0 {
			m.Focus = ""
			m.HelpFocus = ""
			m.MouseDown = buttons&tcell.Button1 != 0
			switch {
			case buttons&tcell.WheelDown != 0:
				m.scroll(3)
			case buttons&tcell.WheelUp != 0:
				m.scroll(-3)
			}
			// Wheel packets can include held buttons. Never interpret them as
			// clicks, even for horizontal wheels or at a scroll boundary.
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
			m.closeModal()
			return nil
		}
		if e.Key() == tcell.KeyF1 {
			m.help()
			return nil
		}
		if e.Key() == tcell.KeyCtrlS && m.Modal != nil {
			return m.submit()
		}
		if e.Key() == tcell.KeyPgUp || e.Key() == tcell.KeyPgDn {
			step := max(1, m.FrameHeight()-8)
			if e.Key() == tcell.KeyPgUp {
				step = -step
			}
			m.scroll(step)
			return nil
		}
		if e.Key() == tcell.KeyEnter {
			if input := m.Input(); input != nil {
				if m.Modal == nil {
					if m.Tab >= 4 {
						m.Focus = ""
						return nil
					}
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
			m.editInput(e, input)
			return nil
		}
		if e.Key() == tcell.KeyUp {
			m.moveFocus(0, -1)
			return nil
		}
		if e.Key() == tcell.KeyDown {
			m.moveFocus(0, 1)
			return nil
		}
		if e.Key() == tcell.KeyLeft || e.Key() == tcell.KeyRight {
			if m.Modal != nil {
				for _, field := range m.Modal.Fields {
					if len(field.Options) > 0 && m.Focus == Key(app.Action{Kind: "input", ID: field.Key}) {
						m.optionList(field.Key)
						return nil
					}
				}
			}
			delta := 1
			if e.Key() == tcell.KeyLeft {
				delta = -1
			}
			m.moveFocus(delta, 0)
			return nil
		}
		if e.Key() == tcell.KeyHome || e.Key() == tcell.KeyEnd {
			if e.Key() == tcell.KeyHome {
				m.scroll(-1 << 20)
			} else {
				m.scroll(1 << 20)
			}
			return nil
		}
		if e.Key() == tcell.KeyRune {
			if e.Str() == "?" {
				m.help()
				return nil
			}
			if m.Modal != nil && e.Str() != " " {
				return nil
			}
			switch e.Str() {
			case "q":
				m.Quit = true
			case "1", "2", "3", "4", "5", "6", "7":
				m.Tab = navigation[int(e.Str()[0]-'1')].Tab
				m.Focus = ""
			case "r":
				if m.Tab >= 4 {
					a := m.refreshAction()
					return m.Activate(a)
				}
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
	if handled, action := m.updateAction(a); handled {
		return action
	}
	if handled, action := m.pageAction(a); handled {
		return action
	}
	if handled, action := m.settingsAction(a); handled {
		return action
	}
	switch a.Kind {
	case "navigate":
		if tab, err := strconv.Atoi(a.ID); err == nil && tab >= 0 && tab < 7 {
			m.Tab = tab
		}
		m.Focus = ""
		m.Modal = nil
	case "input":
		m.Focus = Key(a)
		m.optionList(a.ID)
	case "select-option":
		if m.Modal == nil || m.Modal.Parent == nil {
			return nil
		}
		for i := range m.Modal.Parent.Fields {
			field := &m.Modal.Parent.Fields[i]
			if field.Key == a.ID {
				for _, option := range field.Options {
					if option == a.Value {
						field.Text = option
						m.closeModal()
						return nil
					}
				}
			}
		}
	case "new":
		m.newProfile()
	case "profile-menu":
		p, ok := state.Find(m.View.Settings, a.ID)
		if !ok {
			return nil
		}
		m.Modal = &Modal{Title: "Profile Options", List: true, Body: []string{p.Name}, Choices: []Choice{{"Select", app.Action{Kind: "profile-select", ID: p.ID}}}}
		if p.Type == "remote" {
			m.Modal.Choices = append(m.Modal.Choices, Choice{"Update", app.Action{Kind: "profile-update", ID: p.ID}})
		}
		m.Modal.Choices = append(m.Modal.Choices, Choice{"Edit Info", app.Action{Kind: "profile-info", ID: p.ID}}, Choice{"Edit File", app.Action{Kind: "profile-file", ID: p.ID}})
		m.Modal.Choices = append(m.Modal.Choices, Choice{"Delete", app.Action{Kind: "profile-delete-confirm", ID: p.ID}})
		m.Focus = ""
	case "profile-delete-confirm":
		p, ok := state.Find(m.View.Settings, a.ID)
		if !ok {
			return nil
		}
		body := []string{"Delete this profile?", p.Name}
		if p.ID == m.View.Settings.Current {
			body = append(body, "The core will switch to the built-in DIRECT configuration.")
		}
		m.Modal = &Modal{Title: "Delete Profile", Body: body, Choices: []Choice{{"Cancel", app.Action{Kind: "close"}}, {"Delete", app.Action{Kind: "profile-delete", ID: p.ID}}}}
		m.Focus = Key(app.Action{Kind: "close"})
	case "close":
		m.closeModal()
	case "clear":
		m.URL = ""
	case "paste":
		m.Focus = Key(app.Action{Kind: "input", ID: "url"})
	case "network-tab":
		m.NetworkTab = a.ID
	case "collapse":
		m.Collapsed[a.ID] = !m.Collapsed[a.ID]
	case "import":
		if strings.TrimSpace(m.URL) != "" && m.canRun(app.Action{Kind: "profile-import"}) {
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
		modal.Choices = append(modal.Choices, Choice{"Import Core File", app.Action{Kind: "core-import-form"}})
		m.Modal = modal
		m.Focus = ""
	case "core-form":
		request := app.Action{Kind: "core-releases", ID: "1", Node: strconv.Itoa(m.CoreReleaseEpoch + 1)}
		if !m.canRun(request) {
			return nil
		}
		m.CoreReleaseEpoch++
		m.CoreReleases, m.CoreReleaseNext = nil, 0
		m.Modal = &Modal{Title: "Install Core", Kind: "core-releases", List: true, Body: []string{"Loading official mihomo releases..."}}
		m.Focus = ""
		return &request
	case "core-releases":
		if m.Modal != nil && m.Modal.Kind == "core-releases" && a.Node == strconv.Itoa(m.CoreReleaseEpoch) && m.canRun(a) {
			return &a
		}
	case "test-url":
		m.Modal = &Modal{Title: "Delay check URL", Kind: "test-url", Fields: []Field{{Key: "group", Label: "Group", Text: a.ID}, {Key: "testurl", Label: "Delay check URL", Text: m.TestURL[a.ID]}}}
		m.Focus = Key(app.Action{Kind: "input", ID: "testurl"})
	case "service-info":
		m.Modal = &Modal{Title: "Auto Launch", Body: []string{"Use the systemd service to keep the core running.", "Generate a unit with: verge-tui service --mode user", "Enable it with: systemctl --user enable --now verge-tui", "Boot startup requires a permitted user session or lingering.", "System mode uses: verge-tui service --mode system"}}
		if m.View.Settings.InstallMode == "system" {
			m.Modal = &Modal{Title: "System Service", Body: []string{"Service management requires root.", "Start: sudo systemctl start verge-tui.service", "Stop: sudo systemctl stop verge-tui.service", "Status: systemctl status verge-tui.service", "Profiles, nodes, TUN and managed cores are shared."}}
		}
	case "environment":
		m.Modal = &Modal{Title: "Proxy Environment", Body: strings.Split(app.ProxyEnvironment(m.View.Settings), "\n")}
	case "copy-field":
		if !m.fieldDisabled(a.ID) {
			m.Message = "Copied"
		}
	case "copy-environment":
		m.Message = "Proxy environment copied to clipboard"
	default:
		if m.canRun(a) {
			m.Modal = nil
			m.Focus = ""
			if a.Kind == "delay" {
				a.Value = m.TestURL[a.ID]
				if a.Value == "" {
					a.Value = m.View.Preferences.DelayURL
				}
				a.DelayTimeout = m.delayTimeout()
			}
			return &a
		}
	}
	return nil
}
func (m *Model) scroll(delta int) {
	if m.Modal != nil {
		limit := max(0, m.modalContentHeight()-m.modalViewport())
		m.Modal.Scroll = max(0, min(limit, m.Modal.Scroll+delta))
		return
	}
	m.Scroll[m.Tab] = max(0, min(m.MaxScroll, m.Scroll[m.Tab]+delta))
}
func (m *Model) newProfile() {
	m.Cursors = map[string]int{}
	m.Modal = &Modal{Title: "Create Profile", Kind: "profile-new", Fields: []Field{{Key: "type", Label: "Type", Text: "Remote", Options: []string{"Remote", "Local"}}, {Key: "name", Label: "Name"}, {Key: "description", Label: "Descriptions"}, {Key: "source", Label: "Subscription URL"}, {Key: "agent", Label: "User Agent"}, {Key: "timeout", Label: "HTTP Request Timeout", Text: "30"}}}
	m.Focus = Key(app.Action{Kind: "input", ID: "type"})
}
func (m *Model) submit() *app.Action {
	if m.Modal == nil {
		return nil
	}
	if m.Modal.List {
		for _, choice := range m.Modal.Choices {
			if Key(choice.Action) == m.Focus {
				return m.Activate(choice.Action)
			}
		}
		return nil
	}
	values := map[string]string{}
	for _, f := range m.Modal.Fields {
		values[f.Key] = strings.TrimSpace(f.Text)
	}
	target := app.Action{Kind: m.Modal.Kind, ID: m.Modal.Section, Node: m.Modal.ProfileID}
	if target.Kind == "profile-info-save" {
		target.ID = m.Modal.ProfileID
	}
	if target.Kind == "core-import" {
		target.ID = values["id"]
		target.Node = ""
	}
	if target.Kind != "test-url" && !m.canRun(target) {
		return nil
	}
	switch m.Modal.Kind {
	case "profile-info-save":
		return m.submitProfileInfo(values)
	case "update-options":
		return m.applyUpdateOptions(values)
	case "settings-save", "preferences-save":
		section, profileID, kind := m.Modal.Section, m.Modal.ProfileID, m.Modal.Kind
		m.Modal = nil
		return &app.Action{Kind: kind, ID: section, Node: profileID, Fields: values}
	case "core-import":
		if !state.ValidID(values["id"]) || values["file"] == "" {
			m.Message = "Enter a valid version ID and core file path"
			return nil
		}
		m.Modal = nil
		return &app.Action{Kind: "core-import", ID: values["id"], File: values["file"]}
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
	case "test-url":
		m.TestURL[values["group"]] = values["testurl"]
		m.Modal = nil
		m.Message = "Delay check URL saved"
	}
	return nil
}
func (m *Model) Complete(v app.View, a app.Action, err error) {
	m.finishTask(a)
	if a.Kind == "delay" {
		m.DelayActive = false
		m.DelayMessage = ""
	} else {
		if v.Settings.Current != m.View.Settings.Current || v.Settings.Core != m.View.Settings.Core || err == nil && invalidatesDelay(a, m.View) {
			m.cancelDelay()
			m.Latencies = nil
		}
		m.View = v
		if v.Update.CurrentVersion != "" {
			m.AppUpdate = v.Update
		}
	}
	for _, update := range m.Latencies {
		m.setDelay(update)
	}
	m.applyPreferences()
	if !m.Loaded && a.Kind == "refresh" && err == nil {
		for _, page := range navigation {
			if page.Kind == v.Preferences.StartPage {
				m.Tab = page.Tab
			}
		}
		m.Loaded = true
	}
	if a.Kind == "delay" {
		m.Delays = nil
	}
	if err != nil {
		m.Message = "Error: " + err.Error()
		if a.Kind == "profile-file-save" {
			m.Editor = m.PendingEditor
			m.PendingEditor = nil
		}
		if a.Kind == "profile-info-save" {
			m.Modal = m.PendingProfile
			m.PendingProfile = nil
		}
		if a.Kind != "delay" && a.Kind != "profile-file-save" && a.Kind != "profile-info-save" {
			m.Modal = &Modal{Title: "Action Failed", Body: strings.Split(err.Error(), "\n")}
		}
	} else {
		m.Message = "Ready"
		if a.Kind == "profile-info-save" || a.Kind == "profile-file-save" {
			m.PendingProfile = nil
			m.PendingEditor = nil
			m.Message = "Save successfully"
		}
		if a.Kind == "profile-import" {
			m.URL = ""
			m.Message = "Profile imported"
		}
		if a.Kind == "profile-select" {
			m.Message = "Profile Switched"
		}
		if a.Kind == "profile-delete" {
			m.Message = "Profile deleted"
		}
		if a.Kind == "delay" {
			m.Message = "Delay check completed"
		}
		if a.Kind == "settings-save" || a.Kind == "preferences-save" {
			m.Message = "Settings saved"
		}
		if a.Kind == "geo-update" {
			m.Message = "GeoData updated"
		}
	}
}

func (m *Model) setDelay(update app.DelayResult) {
	p, ok := m.View.Snapshot.Proxies[update.Name]
	if !ok {
		return
	}
	p.History = []controller.History{{Delay: update.Delay}}
	m.View.Snapshot.Proxies[update.Name] = p
}

func (m *Model) ApplyDelay(update app.DelayResult) {
	if m.Delays == nil {
		m.Delays = make(map[string]app.DelayResult)
	}
	m.Delays[update.Name] = update
	if m.Latencies == nil {
		m.Latencies = make(map[string]app.DelayResult)
	}
	m.Latencies[update.Name] = update
	m.setDelay(update)
	m.DelayMessage = fmt.Sprintf("Delay check: %d/%d", update.Done, update.Total)
	if len(m.Tasks) <= 1 {
		m.Message = m.DelayMessage
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

func (m *Model) fieldDisabled(key string) bool {
	if m.Modal != nil && m.Modal.Section == "system-proxy" && key == "proxy-bypass" {
		for _, f := range m.Modal.Fields {
			if f.Key == "use-default-bypass" {
				return f.Text == "true"
			}
		}
	}
	if m.Modal == nil || m.Modal.Section != "controller" || key == "enabled" {
		return false
	}
	for _, f := range m.Modal.Fields {
		if f.Key == "enabled" {
			return f.Text != "true"
		}
	}
	return false
}
func (m *Model) clipboardText(a app.Action) string {
	if a.Kind == "copy-environment" {
		return app.ProxyEnvironment(m.View.Settings)
	}
	if m.Modal != nil && !m.fieldDisabled(a.ID) {
		for _, f := range m.Modal.Fields {
			if f.Key == a.ID {
				return f.Text
			}
		}
	}
	return ""
}
