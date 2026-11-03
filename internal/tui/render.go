package tui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/clipperhouse/displaywidth"
	"verge-tui/internal/app"
	"verge-tui/internal/state"
)

func (m *Model) Render(w, h int) *Frame {
	f := themedFrame(max(1, w), max(1, h), paletteFor(m.View.Preferences.Theme))
	f.Focus = m.Focus
	m.Frame = f
	if w < 60 || h < 18 {
		f.Line(1, 1, w-2, m.tr("Please resize the terminal to at least 60 x 18."), f.Theme.Base)
		return f
	}
	side := 20
	if w < 90 {
		side = 16
	}
	f.Fill(Rect{0, 0, side, h - 1}, f.Theme.Sidebar)
	f.Line(2, 1, side-4, "VERGE TUI", f.Theme.Card.Foreground(f.Theme.Accent).Bold(true))
	f.Line(2, 2, side-4, "v"+m.View.Version, f.Theme.Card.Foreground(f.Theme.Muted))
	navStep, navHeight := 3, 2
	if h < 33 {
		navStep, navHeight = 2, 1
	}
	if h < 26 {
		navStep = 1
	}
	for i, page := range navigation {
		f.NavButton(Rect{1, 5 + i*navStep, side - 2, navHeight}, m.tr(page.Name), app.Action{Kind: "navigate", ID: strconv.Itoa(page.Tab)}, m.Tab == page.Tab)
	}
	stateText := m.tr("Core Not Running")
	if m.View.Online {
		stateText = m.tr("Core Running")
	}
	if h >= 25 {
		f.Line(2, h-7, side-4, stateText, f.Theme.Card.Foreground(f.Theme.Muted))
		f.Line(2, h-6, side-4, m.View.Settings.Core, f.Theme.Card.Foreground(f.Theme.Muted))
	}
	label, kind := m.tr("Start Core"), "start"
	if m.View.Online {
		label, kind = m.tr("Stop Core"), "stop"
	}
	if m.View.Settings.InstallMode == "system" && !m.View.Owned {
		label, kind = m.tr("System Service"), "service-info"
	}
	f.Button(Rect{1, h - 4, side - 2, 2}, label, app.Action{Kind: kind}, false)
	x, width := side+2, w-side-4
	title := m.tr(pageTitle(m.Tab))
	f.Line(x, 1, width-27, title, f.Theme.Base.Bold(true))
	f.Button(Rect{w - 27, 1, 14, 1}, m.tr("Refresh"), m.refreshAction(), false)
	f.Button(Rect{w - 13, 1, 11, 1}, m.tr("Core"), app.Action{Kind: "cores"}, false)
	f.Line(x, 3, width, strings.Repeat("─", width), f.Theme.Base.Foreground(f.Theme.Border))
	f.Clip = Rect{x, 4, width, h - 6}
	bottom := 4
	switch m.Tab {
	case 0:
		bottom = m.home(f, x, 5, width)
	case 1:
		bottom = m.proxies(f, x, 5, width)
	case 2:
		bottom = m.profiles(f, x, 5, width)
	case 3:
		bottom = m.settings(f, x, 5, width)
	case 4:
		bottom = m.connectionsPage(f, x, 5, width)
	case 5:
		bottom = m.rulesPage(f, x, 5, width)
	case 6:
		bottom = m.logsPage(f, x, 5, width)
	}
	m.MaxScroll = max(0, bottom+m.Scroll[m.Tab]-(h-2))
	if m.Scroll[m.Tab] > m.MaxScroll {
		m.Scroll[m.Tab] = m.MaxScroll
		return m.Render(w, h)
	}
	f.Clip = Rect{0, 0, w, h}
	status := m.tr(m.Message)
	if m.DelayActive && m.DelayMessage != "" && m.Message != m.DelayMessage {
		status += " | " + m.tr(m.DelayMessage)
	}
	if m.Busy {
		status = m.tr("Working…  ") + status
	}
	f.Fill(Rect{0, h - 1, w, 1}, f.Theme.Card)
	f.Line(1, h-1, max(0, w-45), status, f.Theme.Card.Foreground(f.Theme.Muted))
	f.Line(max(1, w-43), h-1, 42, m.tr("1–7 Pages  Tab Focus  F1 Help  q Quit"), f.Theme.Card.Foreground(f.Theme.Muted))
	if m.Editor != nil {
		m.renderEditor(f)
	}
	if m.Modal != nil {
		m.modal(f)
	}
	return f
}
func (m *Model) home(f *Frame, x, y, width int) int {
	cols := 1
	if width >= 78 {
		cols = 2
	}
	cw := (width - (cols-1)*2) / cols
	y -= m.Scroll[0]
	renders := []func(*Frame, Rect){m.homeProfile, m.homeNode, m.homeNetwork, m.homeMode, m.homeClash, m.homeSystem}
	heights := []int{12, 12, 10, 10, 11, 11}
	for i, draw := range renders {
		row, col := i/cols, i%cols
		offset := 0
		for r := 0; r < row; r++ {
			offset += heights[r*cols] + 1
		}
		draw(f, Rect{x + col*(cw+2), y + offset, cw, heights[i]})
	}
	total := 0
	for i := 0; i < len(renders); i += cols {
		total += heights[i] + 1
	}
	return y + total - 1
}
func (m *Model) homeProfile(f *Frame, r Rect) {
	p, found := state.Find(m.View.Settings, m.View.Settings.Current)
	title := m.tr("Profiles")
	if found {
		title = p.Name
	}
	f.Box(r, title)
	if !found {
		f.Line(r.X+2, r.Y+4, r.W-4, m.tr("No active subscription"), f.Theme.Card.Foreground(f.Theme.Muted))
		f.Button(Rect{r.X + 2, r.Y + 7, r.W - 4, 1}, m.tr("Go to Profiles"), app.Action{Kind: "navigate", ID: "2"}, false)
		return
	}
	source := "local"
	if u, err := url.Parse(p.URL); err == nil && u.Host != "" {
		source = u.Host
	}
	lines := []string{m.tr("From: ") + source, m.tr("Update Time: ") + p.Updated.Local().Format("2006-01-02 15:04")}
	if p.Usage != nil {
		used := p.Usage.Upload + p.Usage.Download
		lines = append(lines, m.tr("Used / Total: ")+bytesString(used)+m.tr(" / ")+bytesString(p.Usage.Total))
		expiry := "-"
		if p.Usage.Expire > 0 {
			expiry = time.Unix(p.Usage.Expire, 0).Local().Format("2006-01-02")
		}
		lines = append(lines, m.tr("Expire Time: ")+expiry)
		percent := 0
		if p.Usage.Total > 0 {
			percent = int(min(100, float64(used)*100/float64(p.Usage.Total)))
		}
		barWidth := max(1, r.W-12)
		filled := barWidth * percent / 100
		lines = append(lines, strings.Repeat("━", filled)+strings.Repeat("─", barWidth-filled)+fmt.Sprintf(m.tr(" %d%%"), percent))
	} else {
		lines = append(lines, m.tr("Usage / expiry: not provided"))
	}
	for i, line := range lines {
		f.Line(r.X+2, r.Y+3+i, r.W-4, line, f.Theme.Card.Foreground(f.Theme.Muted))
	}
	f.Button(Rect{r.X + 2, r.Y + r.H - 3, 14, 1}, m.tr("Profiles"), app.Action{Kind: "navigate", ID: "2"}, false)
	if p.Type == "remote" {
		f.Button(Rect{r.X + 17, r.Y + r.H - 3, r.W - 19, 1}, m.tr("Update"), app.Action{Kind: "profile-update", ID: p.ID}, false)
	}
}
func (m *Model) homeNode(f *Frame, r Rect) {
	f.Box(r, m.tr("Current Node"))
	mode := m.mode()
	if mode == "direct" {
		f.Line(r.X+2, r.Y+4, r.W-4, "DIRECT", f.Theme.Card.Foreground(f.Theme.Green).Bold(true))
		f.Line(r.X+2, r.Y+6, r.W-4, m.tr("Direct Mode"), f.Theme.Card.Foreground(f.Theme.Muted))
		return
	}
	g := app.HomeGroup(m.View)
	if !m.View.Online || g.Name == "" {
		f.Line(r.X+2, r.Y+4, r.W-4, m.tr("No active proxy node"), f.Theme.Card.Foreground(f.Theme.Muted))
		return
	}
	if mode == "global" {
		f.Line(r.X+2, r.Y+3, r.W-4, m.tr("Global Mode"), f.Theme.Card.Foreground(f.Theme.Muted))
	} else {
		f.Button(Rect{r.X + 2, r.Y + 3, r.W - 4, 1}, m.tr("Group: ")+g.Name, app.Action{Kind: "home-groups"}, false)
	}
	f.Button(Rect{r.X + 2, r.Y + 5, r.W - 4, 1}, m.tr("Proxy: ")+g.Now, app.Action{Kind: "home-nodes"}, true)
	node := m.View.Snapshot.Proxies[g.Now]
	f.Line(r.X+2, r.Y+7, r.W-4, m.delayLabel(node), f.Theme.Card.Foreground(m.delayColor(node)))
	f.Button(Rect{r.X + 2, r.Y + 9, r.W - 4, 1}, m.tr("Delay check"), app.Action{Kind: "delay", ID: g.Name, Node: g.Now}, false)
}
func (m *Model) homeNetwork(f *Frame, r Rect) {
	f.Box(r, m.tr("Network Settings"))
	half := (r.W - 4) / 2
	f.Button(Rect{r.X + 2, r.Y + 3, half, 1}, m.tr("System Proxy"), app.Action{Kind: "network-tab", ID: "system"}, m.NetworkTab == "system")
	f.Button(Rect{r.X + 2 + half, r.Y + 3, half, 1}, m.tr("TUN Mode"), app.Action{Kind: "network-tab", ID: "tun"}, m.NetworkTab == "tun")
	if m.NetworkTab == "system" {
		status := m.tr("System Proxy Disabled")
		if m.View.Desktop.Enabled {
			status = m.tr("System Proxy Enabled")
		}
		if !m.View.Desktop.Available {
			status = m.tr("GNOME proxy unavailable")
		}
		f.Line(r.X+2, r.Y+5, r.W-4, status, f.Theme.Card.Foreground(f.Theme.Muted))
		if m.View.Desktop.Available {
			label := m.tr("System Proxy") + ": " + m.tr("Off")
			if m.View.Desktop.Enabled {
				label = m.tr("System Proxy") + ": " + m.tr("On")
			}
			f.Button(Rect{r.X + 2, r.Y + 7, r.W - 4, 1}, label, app.Action{Kind: "system-proxy"}, m.View.Desktop.Enabled)
		} else {
			f.Button(Rect{r.X + 2, r.Y + 7, r.W - 4, 1}, m.tr("Proxy Environment"), app.Action{Kind: "environment"}, false)
		}
	} else {
		status := m.tr("TUN Mode Disabled")
		enabled := m.View.Settings.Tun
		if m.View.Online {
			enabled = m.View.Snapshot.Config.Tun.Enable
		}
		if enabled {
			status = m.tr("TUN Mode Enabled")
		}
		if m.View.Settings.InstallMode != "system" {
			status = m.tr("TUN Mode Service Required")
		}
		f.Line(r.X+2, r.Y+5, r.W-4, status, f.Theme.Card.Foreground(f.Theme.Muted))
		label := m.tr("TUN Mode") + ": " + m.tr("Off")
		if enabled {
			label = m.tr("TUN Mode") + ": " + m.tr("On")
		}
		f.Button(Rect{r.X + 2, r.Y + 7, r.W - 4, 1}, label, app.Action{Kind: "tun"}, enabled)
	}
}
func (m *Model) mode() string {
	if m.View.Online {
		return m.View.Snapshot.Config.Mode
	}
	return m.View.Settings.Mode
}
func (m *Model) modeButtons(f *Frame, x, y, width int) {
	bw := width / 3
	for i, mode := range []string{"rule", "global", "direct"} {
		label := []string{m.tr("Rule"), m.tr("Global"), m.tr("Direct")}[i]
		f.Button(Rect{x + i*bw, y, bw, 1}, label, app.Action{Kind: "mode", Value: mode}, m.mode() == mode)
	}
}
func (m *Model) homeMode(f *Frame, r Rect) {
	f.Box(r, m.tr("Proxy Mode"))
	m.modeButtons(f, r.X+2, r.Y+3, r.W-4)
	description := m.tr("Automatically choose proxies according to the rule set.")
	if m.mode() == "global" {
		description = m.tr("Forward all network requests through the selected proxy.")
	}
	if m.mode() == "direct" {
		description = m.tr("Bypass the proxy and connect to the internet directly.")
	}
	lines := wrapText(description, r.W-4)

	for i, line := range lines {
		f.Line(r.X+2, r.Y+6+i, r.W-4, line, f.Theme.Card.Foreground(f.Theme.Muted))
	}
}
func (m *Model) homeClash(f *Frame, r Rect) {
	f.Box(r, m.tr("Clash Info"))
	version := m.View.Settings.Core
	if m.View.Online {
		version = m.View.Snapshot.Version
	}
	if version == "" {
		version = m.tr("Not installed")
	}
	address := m.View.Desktop.Address
	if address == "" {
		address = "-"
	}
	uptime := "-"
	if m.View.Online && !m.View.CoreStatus.Started.IsZero() {
		uptime = time.Since(m.View.CoreStatus.Started).Truncate(time.Second).String()
	}
	rules := "-"
	if m.View.Online {
		rules = strconv.Itoa(m.View.Snapshot.Rules)
	}
	lines := []string{m.tr("Core Version: ") + version, m.tr("System Proxy Address: ") + address, m.tr("Mixed Port: ") + strconv.Itoa(m.View.Settings.MixedPort), m.tr("Uptime: ") + uptime, m.tr("Rules Count: ") + rules}
	for i, line := range lines {
		f.Line(r.X+2, r.Y+3+i, r.W-4, line, f.Theme.Card.Foreground(f.Theme.Muted))
	}
	f.Button(Rect{r.X + 2, r.Y + 8, r.W - 4, 1}, m.tr("Core Version"), app.Action{Kind: "cores"}, false)
}
func (m *Model) homeSystem(f *Frame, r Rect) {
	f.Box(r, m.tr("System Info"))
	mode := m.tr("Core Not Running")
	if m.View.Online {
		mode = m.tr("Service Mode")
		if m.View.Owned {
			mode = m.tr("User Mode")
		}
		if m.View.Settings.InstallMode == "system" {
			mode = m.tr("Administrator + Service Mode")
		}
	}
	lastCheck := "-"
	if !m.AppUpdate.LastChecked.IsZero() {
		lastCheck = m.AppUpdate.LastChecked.Local().Format("2006-01-02 15:04")
	}
	lines := []string{m.tr("OS Info: ") + m.View.OS, m.tr("Auto Launch: service controlled"), m.tr("Running Mode: ") + mode, m.tr("Last Check Update") + ": " + lastCheck, m.tr("TUI Version: ") + m.View.Version}
	for i, line := range lines {
		f.Line(r.X+2, r.Y+3+i, r.W-4, line, f.Theme.Card.Foreground(f.Theme.Muted))
	}
	f.Button(Rect{r.X + 2, r.Y + 8, r.W - 4, 1}, m.tr("Auto Launch"), app.Action{Kind: "service-info"}, false)
}
func (m *Model) proxies(f *Frame, x, y, width int) int {
	m.modeButtons(f, x, y, min(width, 42))
	y += 3
	groups := app.Groups(m.View)
	if !m.View.Online || len(groups) == 0 {
		message := m.tr("No proxy groups are available")
		if m.mode() == "direct" {
			message = m.tr("Direct Mode")
		} else if len(m.View.Settings.Profiles) == 0 {
			message = m.tr("No subscription imported")
		} else if m.View.Settings.Current == "" {
			message = m.tr("No active subscription")
		} else if !m.View.Online {
			message = m.tr("Core temporarily unavailable")
		}
		f.Line(x+2, y+2, width-4, message, f.Theme.Base.Foreground(f.Theme.Muted))
		f.Button(Rect{x + 2, y + 5, 23, 1}, m.tr("Go to Profiles"), app.Action{Kind: "navigate", ID: "2"}, false)
		return y + 8
	}
	f.Clip = Rect{x, y, width, max(0, f.H-y-2)}
	y -= m.Scroll[1]
	cols := max(1, min(3, width/30))
	if m.View.Preferences.ProxyColumns > 0 {
		cols = min(m.View.Preferences.ProxyColumns, max(1, width/24))
	}
	cw := (width - (cols-1)*2) / cols
	for _, g := range groups {
		nodeRows := (len(g.All) + cols - 1) / cols
		height := 5 + nodeRows*4
		if m.Collapsed[g.Name] {
			height = 4
		}
		if y+height > f.Clip.Y && y < f.Clip.Y+f.Clip.H {
			f.Box(Rect{x, y, width, height}, "")
			arrow := "▾"
			if m.Collapsed[g.Name] {
				arrow = "▸"
			}
			f.Button(Rect{x + 1, y + 1, max(10, width-31), 1}, arrow+" "+g.Name+m.tr(" · ")+g.Type, app.Action{Kind: "collapse", ID: g.Name}, false)
			f.Button(Rect{x + width - 30, y + 1, 16, 1}, m.tr("Delay check"), app.Action{Kind: "delay", ID: g.Name}, false)
			f.Button(Rect{x + width - 14, y + 1, 13, 1}, m.tr("Test URL"), app.Action{Kind: "test-url", ID: g.Name}, false)
			f.Line(x+2, y+2, width-4, fmt.Sprintf(m.tr("%d nodes  ·  Selected: %s"), len(g.All), g.Now), f.Theme.Card.Foreground(f.Theme.Muted))
			if !m.Collapsed[g.Name] {
				for i, name := range g.All {
					nx, ny := x+(i%cols)*(cw+2)+1, y+4+(i/cols)*4
					if ny+3 <= f.Clip.Y || ny >= f.Clip.Y+f.Clip.H {
						continue
					}
					node := m.View.Snapshot.Proxies[name]
					mark := m.tr("○ ")
					if name == g.Now {
						mark = m.tr("● ")
					}
					f.Button(Rect{nx, ny, cw - 2, 1}, mark+name, app.Action{Kind: "node-select", ID: g.Name, Node: name}, name == g.Now)
					f.Line(nx+1, ny+1, max(1, cw-17), node.Type, f.Theme.Card.Foreground(f.Theme.Muted))
					f.DelayButton(Rect{nx + cw - 16, ny + 1, 14, 1}, m.delayLabel(node), app.Action{Kind: "delay", ID: g.Name, Node: name}, m.delayColor(node))
				}
			}
		}
		y += height + 1
	}
	return y
}
func (m *Model) profiles(f *Frame, x, y, width int) int {
	inputWidth := width - 33
	if width < 58 {
		inputWidth = width - 11
	}
	m.input(f, Rect{x, y, inputWidth, 1}, m.tr("Profile URL"), m.URL, "url")
	label, kind := m.tr("Paste"), "paste"
	if m.URL != "" {
		label, kind = m.tr("Clear"), "clear"
	}
	f.Button(Rect{x + inputWidth + 1, y, 10, 1}, label, app.Action{Kind: kind}, false)
	buttonY := y
	buttonX := x + inputWidth + 12
	if width < 58 {
		buttonY = y + 2
		buttonX = x
	}
	f.Button(Rect{buttonX, buttonY, 10, 1}, m.tr("Import"), app.Action{Kind: "import"}, false)
	f.Button(Rect{buttonX + 11, buttonY, 8, 1}, m.tr("New"), app.Action{Kind: "new"}, false)
	y = buttonY + 3
	if len(m.View.Settings.Profiles) == 0 {
		f.Line(x+2, y+2, width-4, m.tr("Click to import subscription"), f.Theme.Base.Foreground(f.Theme.Muted))
		return y + 5
	}
	f.Clip = Rect{x, y, width, max(0, f.H-y-2)}
	y -= m.Scroll[2]
	cols := 1
	if width >= 76 {
		cols = 2
	}
	cw := (width - (cols-1)*2) / cols
	for i, p := range m.View.Settings.Profiles {
		r := Rect{x + (i%cols)*(cw+2), y + (i/cols)*10, cw, 9}
		if r.Y+r.H <= f.Clip.Y || r.Y >= f.Clip.Y+f.Clip.H {
			continue
		}
		f.Box(r, "")
		f.Add(r, app.Action{Kind: "profile-select", ID: p.ID})
		mark := m.tr("○ ")
		if p.ID == m.View.Settings.Current {
			mark = m.tr("● ")
		}
		f.Line(r.X+2, r.Y+1, r.W-17, mark+p.Name, f.Theme.Card.Foreground(f.Theme.Accent).Bold(true))
		f.Button(Rect{r.X + r.W - 14, r.Y + 1, 12, 1}, m.tr("Options"), app.Action{Kind: "profile-menu", ID: p.ID}, false)
		f.Line(r.X+2, r.Y+3, r.W-4, strings.ToUpper(p.Type), f.Theme.Card.Foreground(f.Theme.Muted))
		source := p.Description
		if source == "" {
			source = m.tr("Local YAML")
			if u, err := url.Parse(p.URL); err == nil && u.Host != "" {
				source = u.Host
			}
		}
		f.Line(r.X+2, r.Y+4, r.W-4, source, f.Theme.Card.Foreground(f.Theme.Muted))
		f.Line(r.X+2, r.Y+5, r.W-4, p.Updated.Local().Format("2006-01-02 15:04"), f.Theme.Card.Foreground(f.Theme.Muted))
		f.Button(Rect{r.X + 2, r.Y + 7, 12, 1}, m.tr("Select"), app.Action{Kind: "profile-select", ID: p.ID}, p.ID == m.View.Settings.Current)
		if p.Type == "remote" {
			f.Button(Rect{r.X + 15, r.Y + 7, r.W - 17, 1}, m.tr("Update"), app.Action{Kind: "profile-update", ID: p.ID}, false)
		}
	}
	return y + ((len(m.View.Settings.Profiles)+cols-1)/cols)*10 - 1
}
func (m *Model) input(f *Frame, r Rect, placeholder, value, id string) {
	s := f.Theme.Card.Foreground(f.Theme.Text)
	focused := f.Focus == Key(app.Action{Kind: "input", ID: id})
	empty := value == ""
	if focused {
		s = f.Theme.Card.Reverse(true)
	}
	f.Fill(r, s)
	if value == "" {
		value = placeholder
		s = s.Foreground(f.Theme.Muted)
	}
	caret, cursorText := -1, " "
	if focused && m.Input() != nil {
		pos := m.cursor(value)
		if *m.Input() == "" {
			pos, value = 0, ""
		}
		start := 0
		for displaywidth.String(value[start:pos]) >= max(1, r.W-3) {
			start = nextBoundary(value, start)
		}
		caret = displaywidth.String(value[start:pos])
		if pos < len(value) {
			cursorText = value[pos:nextBoundary(value, pos)]
		}
		value = value[start:]
	} else if !empty && displaywidth.String(value) > r.W-3 {
		value = tail(value, r.W-3)
	}
	f.Line(r.X+1, r.Y, r.W-2, value, s)
	if caret >= 0 {
		f.Put(r.X+1+caret, r.Y, cursorText, s.Underline(true).Bold(true))
	}
	f.Add(r, app.Action{Kind: "input", ID: id})
}
func tail(value string, width int) string {
	runes := []rune(value)
	for len(runes) > 0 && displaywidth.String(string(runes)) > width {
		runes = runes[1:]
	}
	return string(runes)
}
func (m *Model) modal(f *Frame) {
	modal := m.Modal
	if modal.RevealFocus {
		m.revealFocus()
		modal.RevealFocus = false
	}
	height := 7 + m.modalContentHeight()
	height = min(f.H-4, max(9, height))
	width := min(f.W-6, 86)
	r := Rect{(f.W - width) / 2, (f.H - height) / 2, width, height}
	f.Hits = nil
	f.Clip = Rect{0, 0, f.W, f.H}
	f.Box(r, m.tr(modal.Title))
	f.Clip = Rect{r.X + 1, r.Y + 2, r.W - 2, r.H - 4}
	y := r.Y + 3 - modal.Scroll
	isLocal := false
	for _, field := range modal.Fields {
		if field.Key == "type" && field.Text == "Local" {
			isLocal = true
		}
	}
	for _, field := range modal.Fields {
		if isLocal && (field.Key == "agent" || field.Key == "timeout" || modal.Kind == "profile-info-save" && field.Key == "source") {
			continue
		}
		label := m.tr(field.Label)
		if isLocal && field.Key == "source" {
			label = m.tr("YAML File (optional)")
		}
		f.Line(r.X+2, y, r.W-4, label, f.Theme.Card.Foreground(f.Theme.Muted))
		y++
		if m.fieldDisabled(field.Key) {
			f.Line(r.X+3, y, r.W-6, field.Text, f.Theme.Card.Foreground(f.Theme.Muted))
		} else {
			m.input(f, Rect{r.X + 2, y, r.W - 4, 1}, label, m.optionLabel(field), field.Key)
		}
		y++
	}
	for _, choice := range modal.Header {
		f.Button(Rect{r.X + 2, y, r.W - 4, 1}, choice.Label, choice.Action, false)
		y += 2
	}
	for i, line := range modal.Body {
		if modal.Kind != "viewer" && modal.Title != "Proxy Environment" {
			line = m.tr(line)
		}
		f.Line(r.X+2, y+i, r.W-4, line, f.Theme.Card.Foreground(f.Theme.Muted))
	}
	y += len(modal.Body)
	for i, choice := range modal.Choices {
		if choice.Action.Kind == "copy-field" && m.fieldDisabled(choice.Action.ID) {
			continue
		}
		label, selected := m.tr(choice.Label), m.choiceSelected(choice.Action)
		if modal.List {
			if selected {
				label = "● " + label
			} else {
				label = "○ " + label
			}
		}
		f.Button(Rect{r.X + 2, y + i*m.choiceStep(), r.W - 4, 1}, label, choice.Action, selected)
	}
	f.Clip = Rect{0, 0, f.W, f.H}
	for i, choice := range modal.Footer {
		f.Button(Rect{r.X + r.W - 12*(len(modal.Footer)+1-i), r.Y + r.H - 2, 11, 1}, m.tr(choice.Label), choice.Action, choice.Action.Kind == "update-install")
	}
	if len(modal.Fields) > 0 {
		f.Button(Rect{r.X + r.W - 24, r.Y + r.H - 2, 11, 1}, m.tr("Save"), app.Action{Kind: "submit"}, true)
	}
	if modal.Title == "Proxy Environment" {
		f.Button(Rect{r.X + 2, r.Y + r.H - 2, 13, 1}, m.tr("Copy"), app.Action{Kind: "copy-environment"}, false)
	}
	f.Button(Rect{r.X + r.W - 12, r.Y + r.H - 2, 10, 1}, m.tr("Close"), app.Action{Kind: "close"}, false)
}

func wrapText(value string, width int) []string {
	var lines []string
	for displaywidth.String(value) > width && len(lines) < 2 {
		piece := displaywidth.TruncateString(value, width, "")
		if piece == "" {
			break
		}
		lines = append(lines, piece)
		value = strings.TrimLeft(strings.TrimPrefix(value, piece), " ")
	}
	return append(lines, value)
}
