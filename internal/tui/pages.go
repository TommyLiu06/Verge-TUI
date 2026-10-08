package tui

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"verge-tui/internal/state"

	"github.com/gdamore/tcell/v3/color"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
)

var navigation = []struct {
	Tab        int
	Name, Kind string
}{
	{0, "Home", "home"}, {1, "Proxies", "proxies"}, {2, "Profiles", "profiles"},
	{4, "Connections", "connections"}, {5, "Rules", "rules"}, {6, "Logs", "logs"}, {3, "Settings", "settings"},
}

func pageKind(tab int) string {
	for _, p := range navigation {
		if p.Tab == tab {
			return p.Kind
		}
	}
	return ""
}
func pageTitle(tab int) string {
	if tab == 1 {
		return "Proxy Groups"
	}
	for _, p := range navigation {
		if p.Tab == tab {
			return p.Name
		}
	}
	return "Home"
}
func (m *Model) refreshAction() app.Action {
	if m.Tab == 6 {
		return app.Action{Kind: "logs-refresh"}
	}
	if m.Tab >= 4 {
		return app.Action{Kind: "page-refresh", ID: pageKind(m.Tab)}
	}
	return app.Action{Kind: "refresh"}
}

func (m *Model) ApplyConnections(next controller.Connections, now time.Time) {
	previous := make(map[string]controller.Connection, len(m.Connections.Connections))
	for _, c := range m.Connections.Connections {
		previous[c.ID] = c
	}
	seconds := now.Sub(m.ConnectionTime).Seconds()
	for i := range next.Connections {
		c := &next.Connections[i]
		if old, found := previous[c.ID]; found && seconds > 0 {
			if c.Upload >= old.Upload {
				c.UploadSpeed = uint64(float64(c.Upload-old.Upload) / seconds)
			}
			if c.Download >= old.Download {
				c.DownloadSpeed = uint64(float64(c.Download-old.Download) / seconds)
			}
		}
		delete(previous, c.ID)
	}
	for _, c := range previous {
		c.UploadSpeed, c.DownloadSpeed = 0, 0
		m.ClosedConnections = append(m.ClosedConnections, c)
	}
	if len(m.ClosedConnections) > 1000 {
		m.ClosedConnections = append([]controller.Connection(nil), m.ClosedConnections[len(m.ClosedConnections)-1000:]...)
	}
	m.Connections, m.ConnectionTime = next, now
}
func (m *Model) AppendLogs(entries []controller.Log) {
	follow := !m.LogsDescending && m.Scroll[6] >= m.MaxScroll-1
	m.Logs = append(m.Logs, entries...)
	if len(m.Logs) > 1000 {
		drop := len(m.Logs) - 1000
		m.Logs = append([]controller.Log(nil), m.Logs[drop:]...)
		if !m.LogsDescending {
			m.Scroll[6] = max(0, m.Scroll[6]-drop*3)
		}
	}
	if follow {
		m.Scroll[6] = 1 << 20
	}
}
func connHost(c controller.Connection) string {
	host := c.Metadata.Host
	if host == "" {
		host = c.Metadata.DestinationIP
	}
	if host == "" {
		host = c.Metadata.RemoteDestination
	}
	return net.JoinHostPort(host, c.Metadata.DestinationPort)
}
func connChains(c controller.Connection) string {
	chains := append([]string(nil), c.Chains...)
	for i, j := 0, len(chains)-1; i < j; i, j = i+1, j-1 {
		chains[i], chains[j] = chains[j], chains[i]
	}
	return strings.Join(chains, " / ")
}
func connRule(c controller.Connection) string {
	if c.RulePayload != "" {
		return c.Rule + "(" + c.RulePayload + ")"
	}
	return c.Rule
}
func matches(text, query string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(strings.TrimSpace(query)))
}
func (m *Model) connectionRows() []controller.Connection {
	data := m.Connections.Connections
	if m.ConnectionClosed {
		data = m.ClosedConnections
	}
	rows := make([]controller.Connection, 0, len(data))
	for _, c := range data {
		if matches(connHost(c)+" "+c.Metadata.DestinationIP+" "+c.Metadata.Process+" "+c.Metadata.ProcessPath, m.Search[4]) {
			rows = append(rows, c)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if m.ConnectionOrder == "Upload Speed" && rows[i].UploadSpeed != rows[j].UploadSpeed {
			return rows[i].UploadSpeed > rows[j].UploadSpeed
		}
		if m.ConnectionOrder == "Download Speed" && rows[i].DownloadSpeed != rows[j].DownloadSpeed {
			return rows[i].DownloadSpeed > rows[j].DownloadSpeed
		}
		if !rows[i].Start.Equal(rows[j].Start) {
			return rows[i].Start.After(rows[j].Start)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}
func (m *Model) pageSearch(f *Frame, x, y, width int) {
	m.input(f, Rect{x, y, width, 1}, m.tr("Search"), m.Search[m.Tab], "search")
}
func (m *Model) pageStatus(f *Frame, x, y, width int, empty bool) {
	status := m.PageError[m.Tab]
	if status == "" && !m.View.Online {
		status = m.tr("Core Not Running")
	}
	if status == "" && empty {
		status = m.tr("No Data")
	}
	f.Line(x+1, y, width-2, status, f.Theme.Base.Foreground(f.Theme.Muted))
}
func (m *Model) connectionsPage(f *Frame, x, y, width int) int {
	f.Line(x, y, width, m.tr("Downloaded")+": "+bytesString(m.Connections.DownloadTotal)+"   "+m.tr("Uploaded")+": "+bytesString(m.Connections.UploadTotal), f.Theme.Base.Foreground(f.Theme.Muted))
	y += 2
	half := width / 2
	f.Button(Rect{x, y, half, 1}, fmt.Sprintf("%s (%d)", m.tr("Active"), len(m.Connections.Connections)), app.Action{Kind: "connection-tab", ID: "active"}, !m.ConnectionClosed)
	f.Button(Rect{x + half, y, width - half, 1}, fmt.Sprintf("%s (%d)", m.tr("Closed"), len(m.ClosedConnections)), app.Action{Kind: "connection-tab", ID: "closed"}, m.ConnectionClosed)
	y += 2
	f.Button(Rect{x, y, width / 2, 1}, m.tr(m.ConnectionOrder), app.Action{Kind: "connection-order"}, false)
	label, kind := "Close All", "connection-close"
	if m.ConnectionClosed {
		label, kind = "Clear", "connections-clear"
	}
	f.Button(Rect{x + width/2, y, width - width/2, 1}, m.tr(label), app.Action{Kind: kind}, false)
	y += 2
	layout := "List Layout"
	if m.ConnectionTable {
		layout = "Table Layout"
	}
	f.Button(Rect{x, y, min(width, 22), 1}, m.tr(layout), app.Action{Kind: "connection-layout"}, false)
	m.pageSearch(f, x+min(width, 22), y, max(1, width-22))
	y += 2
	f.Clip = Rect{x, y, width, max(0, f.H-y-2)}
	rows := m.connectionRows()
	step := 6
	if m.ConnectionTable {
		step = 2
	}
	base := y - m.Scroll[4]
	for i, c := range rows {
		rowY := base + i*step
		if rowY+step <= f.Clip.Y || rowY >= f.Clip.Y+f.Clip.H {
			continue
		}
		r := Rect{x, rowY, width, step - 1}
		style := f.Theme.Card
		a := app.Action{Kind: "connection-detail", ID: c.ID}
		if m.Focus == Key(a) {
			style = style.Reverse(true)
		}
		f.Fill(r, style)
		f.Add(r, a)
		f.Line(x+1, rowY, width-2, connHost(c), style.Bold(true))
		if m.ConnectionTable && width > 62 {
			f.Line(x+width/2, rowY, width/2-1, "↓ "+bytesString(c.DownloadSpeed)+"/s   ↑ "+bytesString(c.UploadSpeed)+"/s", style.Foreground(f.Theme.Muted))
		}
		if !m.ConnectionTable {
			f.Line(x+1, rowY+1, width-2, connChains(c), style.Foreground(f.Theme.Accent))
			f.Line(x+1, rowY+2, width-2, connRule(c)+"  ·  "+c.Metadata.Type+"("+c.Metadata.Network+")", style.Foreground(f.Theme.Muted))
			f.Line(x+1, rowY+3, width-2, "↓ "+bytesString(c.Download)+" ("+bytesString(c.DownloadSpeed)+"/s)   ↑ "+bytesString(c.Upload)+" ("+bytesString(c.UploadSpeed)+"/s)", style.Foreground(f.Theme.Muted))
			f.Line(x+1, rowY+4, width-2, c.Metadata.Process+"  "+c.Start.Local().Format("15:04:05"), style.Foreground(f.Theme.Muted))
		}
	}
	m.pageStatus(f, x, y, width, len(rows) == 0)
	return base + len(rows)*step
}
func (m *Model) rulesPage(f *Frame, x, y, width int) int {
	f.Button(Rect{x, y, min(width, 23), 1}, m.tr("Rule Provider"), app.Action{Kind: "rule-providers"}, false)
	m.pageSearch(f, x+min(width, 23), y, max(1, width-23))
	y += 2
	f.Clip = Rect{x, y, width, max(0, f.H-y-2)}
	base, count := y-m.Scroll[5], 0
	for i, r := range m.Rules.Rules {
		if !matches(r.Payload, m.Search[5]) {
			continue
		}
		rowY := base + count*3
		count++
		if rowY+3 <= f.Clip.Y || rowY >= f.Clip.Y+f.Clip.H {
			continue
		}
		payload := r.Payload
		if payload == "" {
			payload = "-"
		}
		f.Line(x, rowY, 6, fmt.Sprint(i+1), f.Theme.Base.Foreground(f.Theme.Muted))
		f.Line(x+6, rowY, width-6, payload, f.Theme.Base)
		f.Line(x+6, rowY+1, width/2-6, r.Type, f.Theme.Base.Foreground(f.Theme.Muted))
		fg := f.Theme.Accent
		if r.Proxy == "DIRECT" {
			fg = f.Theme.Text
		}
		if r.Proxy == "REJECT" || r.Proxy == "REJECT-DROP" {
			fg = color.PaletteColor(1)
		}
		f.Line(x+width/2, rowY+1, width/2, r.Proxy, f.Theme.Base.Foreground(fg))
	}
	m.pageStatus(f, x, y, width, count == 0)
	return base + count*3
}
func logType(s string) string {
	switch strings.ToLower(s) {
	case "warning", "warn":
		return "Warn"
	case "error", "err":
		return "Error"
	case "info", "inf":
		return "Info"
	default:
		return "Debug"
	}
}
func (m *Model) logsPage(f *Frame, x, y, width int) int {
	label := "Pause"
	if m.LogsPaused {
		label = "Resume"
	}
	order := "Oldest first"
	if m.LogsDescending {
		order = "Newest first"
	}
	f.Button(Rect{x, y, width / 3, 1}, m.tr(label), app.Action{Kind: "logs-pause"}, m.LogsPaused)
	f.Button(Rect{x + width/3, y, width / 3, 1}, m.tr(order), app.Action{Kind: "logs-order"}, false)
	f.Button(Rect{x + 2*(width/3), y, width - 2*(width/3), 1}, m.tr("Clear"), app.Action{Kind: "logs-clear"}, false)
	y += 2
	f.Button(Rect{x, y, 12, 1}, m.tr(m.LogLevel), app.Action{Kind: "logs-level"}, false)
	m.pageSearch(f, x+12, y, width-12)
	y += 2
	f.Clip = Rect{x, y, width, max(0, f.H-y-2)}
	base, count := y-m.Scroll[6], 0
	for n := range m.Logs {
		i := n
		if m.LogsDescending {
			i = len(m.Logs) - 1 - n
		}
		entry := m.Logs[i]
		if m.LogLevel != "All" && logType(entry.Type) != m.LogLevel {
			continue
		}
		if !matches(entry.Time+" "+entry.Type+" "+entry.Payload, m.Search[6]) {
			continue
		}
		rowY := base + count*3
		count++
		if rowY+3 <= f.Clip.Y || rowY >= f.Clip.Y+f.Clip.H {
			continue
		}
		fg := f.Theme.Accent
		switch logType(entry.Type) {
		case "Warn":
			fg = color.PaletteColor(3)
		case "Error":
			fg = color.PaletteColor(1)
		case "Debug":
			fg = f.Theme.Muted
		}
		f.Line(x+1, rowY, 10, entry.Time, f.Theme.Base.Foreground(f.Theme.Muted))
		f.Line(x+12, rowY, width-12, strings.ToUpper(entry.Type), f.Theme.Base.Foreground(fg).Bold(true))
		f.Line(x+1, rowY+1, width-2, entry.Payload, f.Theme.Base)
		f.Add(Rect{x, rowY, width, 2}, app.Action{Kind: "log-detail", ID: fmt.Sprint(i)})
	}
	m.pageStatus(f, x, y, width, count == 0)
	return base + count*3
}

func (m *Model) pageAction(a app.Action) (bool, *app.Action) {
	switch a.Kind {
	case "editor-save":
		return true, m.saveEditor()
	case "editor-close":
		m.closeEditor()
	case "editor-discard":
		m.Editor = nil
		m.Modal = nil
		m.Focus = ""
	case "connection-tab":
		m.ConnectionClosed = a.ID == "closed"
		m.Scroll[4] = 0
	case "connections-clear":
		m.ClosedConnections = nil
		m.Scroll[4] = 0
	case "connection-layout":
		m.ConnectionTable = !m.ConnectionTable
		m.Scroll[4] = 0
	case "connection-order", "logs-level":
		options := []string{"Default", "Upload Speed", "Download Speed"}
		title := "Sort"
		if a.Kind == "logs-level" {
			title = "Log Level"
			options = []string{"All", "Debug", "Info", "Warn", "Error"}
		}
		m.Modal = &Modal{Title: title, List: true}
		for _, option := range options {
			m.Modal.Choices = append(m.Modal.Choices, Choice{option, app.Action{Kind: "page-option", ID: a.Kind, Value: option}})
		}
		m.Focus = ""
	case "page-option":
		if a.ID == "connection-order" {
			m.ConnectionOrder = a.Value
			m.Scroll[4] = 0
		} else {
			m.LogLevel = a.Value
			m.Scroll[6] = 0
		}
		m.closeModal()
	case "logs-pause":
		m.LogsPaused = !m.LogsPaused
	case "logs-order":
		m.LogsDescending = !m.LogsDescending
		m.Scroll[6] = 0
	case "logs-refresh":
		m.WatchRevision++
		m.PageError[6] = ""
	case "logs-clear":
		m.WatchRevision++
		m.Logs = nil
		m.Scroll[6] = 0
	case "connection-detail":
		for _, c := range m.connectionRows() {
			if c.ID != a.ID {
				continue
			}
			fields := []struct{ label, value string }{
				{"Host", connHost(c)}, {"Downloaded", bytesString(c.Download)}, {"Uploaded", bytesString(c.Upload)},
				{"DL Speed", bytesString(c.DownloadSpeed) + "/s"}, {"UL Speed", bytesString(c.UploadSpeed) + "/s"},
				{"Chains", connChains(c)}, {"Rule", connRule(c)}, {"Process", c.Metadata.Process + " (" + c.Metadata.ProcessPath + ")"},
				{"Time", c.Start.Local().Format("2006-01-02 15:04:05")}, {"Source", net.JoinHostPort(c.Metadata.SourceIP, c.Metadata.SourcePort)},
				{"Destination", c.Metadata.DestinationIP}, {"Destination Port", c.Metadata.DestinationPort}, {"Type", c.Metadata.Type + "(" + c.Metadata.Network + ")"},
			}
			m.Modal = &Modal{Title: "Connections", Kind: "viewer"}
			for _, field := range fields {
				m.Modal.Body = append(m.Modal.Body, m.tr(field.label)+": "+field.value)
			}
			if !m.ConnectionClosed {
				m.Modal.Choices = []Choice{{"Close Connection", app.Action{Kind: "connection-close", ID: c.ID}}}
			}
			m.Focus = ""
			break
		}
	case "rule-providers":
		m.Modal = &Modal{Title: "Rule Providers", List: true}
		names := make([]string, 0, len(m.Rules.Providers))
		for name := range m.Rules.Providers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			m.Modal.Choices = append(m.Modal.Choices, Choice{name, app.Action{Kind: "rule-provider", ID: name}})
		}
		if len(names) > 0 {
			m.Modal.Choices = append(m.Modal.Choices, Choice{"Update All", app.Action{Kind: "providers-update"}})
		} else {
			m.Modal.Body = []string{m.tr("No Data")}
		}
		m.Focus = ""
	case "rule-provider":
		p := m.Rules.Providers[a.ID]
		m.Modal = &Modal{Title: a.ID, Kind: "viewer", Parent: m.Modal, Choices: []Choice{{"Update", app.Action{Kind: "provider-update", ID: a.ID}}}, Body: []string{p.Behavior + " · " + p.VehicleType, fmt.Sprintf("%s: %d", m.tr("Rules"), p.RuleCount), m.tr("Update Time") + ": " + p.UpdatedAt.Local().Format("2006-01-02 15:04:05")}}
		m.Focus = ""
	case "log-detail":
		var i int
		if _, err := fmt.Sscan(a.ID, &i); err == nil && i >= 0 && i < len(m.Logs) {
			entry := m.Logs[i]
			m.Modal = &Modal{Title: "Logs", Kind: "viewer", Body: append([]string{entry.Time + "  " + strings.ToUpper(entry.Type)}, strings.Split(entry.Payload, "\n")...)}
			m.Focus = ""
		}
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) showProfileDocument(kind string, doc *app.ProfileDocument) {
	if doc == nil {
		return
	}
	if kind == "profile-file" {
		m.Modal = nil
		m.Editor = newEditor(doc)
		m.Focus = Key(app.Action{Kind: "editor-input"})
		return
	}
	p := doc.Profile
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30
	}
	m.Modal = &Modal{Title: "Edit Info", Kind: "profile-info-save", ProfileID: p.ID, Section: doc.Revision, Fields: []Field{
		{Key: "type", Label: "Type", Text: map[string]string{"remote": "Remote", "local": "Local"}[p.Type], Options: []string{"Remote", "Local"}},
		{Key: "name", Label: "Name", Text: p.Name}, {Key: "description", Label: "Descriptions", Text: p.Description},
		{Key: "source", Label: "Subscription URL", Text: p.URL}, {Key: "agent", Label: "User Agent", Text: p.UserAgent},
		{Key: "timeout", Label: "HTTP Request Timeout", Text: strconv.Itoa(timeout)},
	}}
	m.Focus = Key(app.Action{Kind: "input", ID: "type"})
}
func (m *Model) submitProfileInfo(values map[string]string) *app.Action {
	p := state.Profile{Type: strings.ToLower(values["type"]), Name: values["name"], Description: values["description"], URL: values["source"], UserAgent: values["agent"]}
	timeout, err := strconv.Atoi(values["timeout"])
	if p.Type == "remote" && (err != nil || timeout < 1 || timeout > 300) {
		m.Message = "HTTP Request Timeout must be between 1 and 300 seconds"
		return nil
	}
	p.Timeout = timeout
	m.PendingProfile = m.Modal
	action := &app.Action{Kind: "profile-info-save", ID: m.Modal.ProfileID, Value: m.Modal.Section, Profile: p}
	m.Modal = nil
	return action
}
