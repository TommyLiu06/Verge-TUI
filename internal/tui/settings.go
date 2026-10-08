package tui

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/clipperhouse/displaywidth"

	"verge-tui/internal/app"
	"verge-tui/internal/i18n"
	"verge-tui/internal/state"
)

type settingRow struct {
	label, value string
	action       app.Action
}
type settingSection struct {
	title string
	rows  []settingRow
}

func toggle(value bool) string {
	if value {
		return "On"
	}
	return "Off"
}
func optionField(key, label, value string, options ...string) Field {
	return Field{Key: key, Label: label, Text: value, Options: options}
}
func booleanField(key, label string, value bool) Field {
	return optionField(key, label, strconv.FormatBool(value), "false", "true")
}

func (m *Model) applyPreferences() {
	m.Dark = darkTerminal()
}

func (m *Model) delayTimeout() int {
	if m.View.Preferences.DelayTimeout == 0 {
		return 5000
	}
	return m.View.Preferences.DelayTimeout
}

func (m *Model) settings(f *Frame, x, y, width int) int {
	c := m.View.Settings
	p := m.View.Preferences
	if p.Theme == "" {
		p = state.DefaultPreferences()
	}
	dns := c.Engine.DNS[c.Current]
	edit := func(id string) app.Action { return app.Action{Kind: "settings-form", ID: id} }
	flip := func(id string, value bool) app.Action {
		return app.Action{Kind: "settings-save", ID: id, Fields: map[string]string{"enabled": strconv.FormatBool(!value)}}
	}
	tunState, tunAction := toggle(c.Tun), app.Action{Kind: "tun"}
	if m.View.Online {
		tunState = toggle(m.View.Snapshot.Config.Tun.Enable)
	}
	if c.InstallMode != "system" {
		tunState, tunAction = "Off", app.Action{Kind: "settings-info", ID: "tun"}
	}
	proxyState, proxyAction := toggle(m.View.Desktop.Enabled), app.Action{Kind: "system-proxy"}
	if !m.View.Desktop.Available {
		proxyAction = app.Action{Kind: "settings-info", ID: "system-proxy"}
	}
	columns := "Auto"
	if p.ProxyColumns > 0 {
		columns = strconv.Itoa(p.ProxyColumns)
	}
	sections := []settingSection{
		{"System Setting", []settingRow{
			{"TUN Mode", tunState, tunAction},
			{"System Proxy", proxyState, proxyAction}, {"Auto Launch", "systemd", app.Action{Kind: "service-info"}},
		}},
		{"Clash Setting", []settingRow{
			{"Allow LAN", toggle(c.Engine.AllowLAN), flip("allow-lan", c.Engine.AllowLAN)},
			{"Network Interface", "View", app.Action{Kind: "settings-view", ID: "interfaces"}},
			{"DNS Overwrite", toggle(dns.Enabled), app.Action{Kind: "settings-save", ID: "dns-toggle", Node: c.Current}},
			{"DNS Settings", "Configure", edit("dns")},
			{"IPv6", toggle(m.View.CoreOptions.IPv6), flip("ipv6", m.View.CoreOptions.IPv6)},
			{"Unified Delay", toggle(m.View.CoreOptions.UnifiedDelay), flip("unified-delay", m.View.CoreOptions.UnifiedDelay)},
			{"Log Level", m.View.CoreOptions.LogLevel, edit("log-level")},
			{"Port Config", strconv.Itoa(c.MixedPort), edit("ports")},
			{"External Controller", toggle(c.ControllerEnabled), app.Action{Kind: "settings-view", ID: "controller"}},
			{"Clash Core", c.Core, app.Action{Kind: "cores"}},
			{"Update GeoData", "Update", app.Action{Kind: "geo-update"}},
		}},
		{"Verge Basic Setting", []settingRow{
			{"Language", i18n.Names[p.Language], edit("language")},
			{"Theme", themeLabel(p.Theme), edit("theme")},
			{"Proxy Environment", "View / Copy", app.Action{Kind: "environment"}},
			{"Start Page", p.StartPage, edit("start-page")},
			{"Proxy Layout Columns", columns, edit("misc")},
			{"Miscellaneous", "Configure", edit("misc")},
			{"Keyboard Shortcuts", "View", app.Action{Kind: "settings-help"}},
		}},
		{"Verge Advanced Setting", []settingRow{
			{"Runtime Config", "View", app.Action{Kind: "settings-view", ID: "runtime"}},
			{"Configuration Directory", "View", app.Action{Kind: "settings-info", ID: "config-dir"}},
			{"Core Directory", "View", app.Action{Kind: "settings-info", ID: "core-dir"}},
			{"Logs Directory", "View", app.Action{Kind: "settings-info", ID: "logs-dir"}},
			{"Diagnostic Information", "View", app.Action{Kind: "settings-info", ID: "diagnostics"}},
			{"Exit", "Quit", app.Action{Kind: "settings-quit"}},
			{"Verge TUI Version", "v" + m.View.Version, app.Action{}},
		}},
	}
	y -= m.Scroll[3]
	draw := func(section settingSection, sx, sy, sw int) int {
		height := 4 + len(section.rows)*2
		f.Box(Rect{sx, sy, sw, height}, m.tr(section.title))
		for i, row := range section.rows {
			r := Rect{sx + 2, sy + 3 + i*2, sw - 4, 1}
			if row.label == "TUN Mode" || row.label == "System Proxy" {
				section := "tun"
				if row.label == "System Proxy" {
					section = "system-proxy"
				}
				m.proxySettingRow(f, r, row, edit(section))
				continue
			}
			style := f.Theme.Card
			if row.action.Kind != "" && f.Focus == Key(row.action) {
				style = style.Reverse(true).Bold(true)
			}
			f.Fill(r, style)
			valueWidth := min(16, max(10, r.W/3))
			f.Line(r.X, r.Y, r.W-valueWidth-1, m.tr(row.label), style)
			f.Line(r.X+r.W-valueWidth, r.Y, valueWidth, m.tr(row.value), style.Foreground(f.Theme.Accent))
			if row.action.Kind != "" {
				f.Add(r, row.action)
			}
		}
		return sy + height + 1
	}
	if width >= 78 {
		cw := (width - 2) / 2
		left := draw(sections[0], x, y, cw)
		left = draw(sections[1], x, left, cw)
		right := draw(sections[2], x+cw+2, y, cw)
		right = draw(sections[3], x+cw+2, right, cw)
		return max(left, right)
	}
	for _, section := range sections {
		y = draw(section, x, y, width)
	}
	return y
}

// Match Verge's single row with separate configuration and toggle controls.
func (m *Model) proxySettingRow(f *Frame, r Rect, row settingRow, configure app.Action) {
	label, config, status := m.tr(row.label), m.tr("Configure"), m.tr(row.value)
	configWidth, statusWidth := displaywidth.String(config), displaywidth.String(status)
	valueWidth := min(r.W-8, configWidth+3+statusWidth)
	configWidth = min(configWidth, valueWidth-3-min(statusWidth, (valueWidth-3)/2))
	statusWidth = valueWidth - 3 - configWidth
	f.Fill(r, f.Theme.Card)
	f.Line(r.X, r.Y, r.W-valueWidth-1, label, f.Theme.Card)
	cx := r.X + r.W - valueWidth
	control := func(rect Rect, text string, action app.Action) {
		style := f.Theme.Card.Foreground(f.Theme.Accent)
		if f.Focus == Key(action) {
			style = f.Theme.Card.Reverse(true).Bold(true)
		}
		f.Fill(rect, style)
		f.Line(rect.X, rect.Y, rect.W, text, style)
		f.Add(rect, action)
	}
	control(Rect{cx, r.Y, configWidth, 1}, config, configure)
	f.Put(cx+configWidth, r.Y, " | ", f.Theme.Card.Foreground(f.Theme.Muted))
	control(Rect{cx + configWidth + 3, r.Y, statusWidth, 1}, status, row.action)
}

func (m *Model) settingsAction(a app.Action) (bool, *app.Action) {
	switch a.Kind {
	case "settings-form":
		m.settingsForm(a.ID)
	case "core-import-form":
		m.openSettingsForm("Import Core File", "core-import", "", []Field{{Key: "id", Label: "Version ID"}, {Key: "file", Label: "Mihomo Executable File"}})
	case "settings-help":
		m.help()
	case "settings-quit":
		m.Quit = true
	case "settings-info":
		m.settingsInfo(a.ID)
	case "settings-save":
		if a.ID == "dns-toggle" && m.View.Settings.Current == "" {
			m.Message = "Select a profile before configuring DNS"
			return true, nil
		}
		if m.canRun(a) {
			m.Modal, m.Focus = nil, ""
			return true, &a
		}
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) openSettingsForm(title, kind, section string, fields []Field) {
	m.Cursors = map[string]int{}
	m.Modal = &Modal{Title: title, Kind: kind, Section: section, ProfileID: m.View.Settings.Current, Fields: fields}
	m.Focus = ""
	if len(fields) > 0 {
		m.Focus = Key(app.Action{Kind: "input", ID: fields[0].Key})
	}
}

func (m *Model) settingsForm(section string) {
	c, p := m.View.Settings, m.View.Preferences
	if p.Theme == "" {
		p = state.DefaultPreferences()
	}
	kind, title := "settings-save", ""
	var fields []Field
	switch section {
	case "log-level":
		current := m.View.CoreOptions.LogLevel
		if current == "" {
			current = "info"
		}
		if current == "warn" {
			current = "warning"
		}
		m.selectionList("Log Level", "settings-save", section, "level", current, []string{"debug", "info", "warning", "error", "silent"})
		return
	case "system-proxy":
		kind, title = "preferences-save", "System Proxy"
		bypass := p.SystemProxy.Bypass
		if bypass == "" {
			bypass = strings.Join(state.DefaultProxyBypass(), ", ")
		}
		fields = []Field{
			{Key: "proxy-host", Label: "Proxy Host", Text: p.SystemProxy.Host},
			booleanField("use-default-bypass", "Always use Default Bypass", p.SystemProxy.UseDefaultBypass),
			{Key: "proxy-bypass", Label: "Proxy Bypass Settings: ", Text: bypass},
		}
	case "ports":
		title = "Port Config"
		fields = []Field{{Key: "mixed-port", Label: "Mixed Port (HTTP + SOCKS)", Text: strconv.Itoa(c.MixedPort)}, {Key: "http-port", Label: "HTTP Port (0 = disabled)", Text: strconv.Itoa(c.HTTPPort)}, {Key: "socks-port", Label: "SOCKS Port (0 = disabled)", Text: strconv.Itoa(c.SOCKSPort)}}
	case "tun":
		if c.InstallMode != "system" {
			m.settingsInfo("tun")
			return
		}
		title = "TUN Settings"
		t := state.DefaultTun()
		if c.Engine.Tun != nil {
			t = *c.Engine.Tun
		}
		fields = []Field{optionField("stack", "Stack", t.Stack, "system", "gvisor", "mixed"), {Key: "device", Label: "Device (empty = core default)", Text: t.Device}, booleanField("auto-route", "Auto Route", t.AutoRoute), booleanField("auto-detect", "Auto Detect Interface", t.AutoDetect), booleanField("strict-route", "Strict Route", t.StrictRoute), {Key: "mtu", Label: "MTU", Text: strconv.Itoa(t.MTU)}, {Key: "dns-hijack", Label: "DNS Hijack (comma separated)", Text: strings.Join(t.DNSHijack, ", ")}, {Key: "route-exclude", Label: "Route Exclude Addresses (IP CIDRs, comma separated)", Text: strings.Join(t.RouteExclude, ", ")}}
	case "dns":
		if c.Current == "" {
			m.Message = "Select a profile before configuring DNS"
			return
		}
		title = "DNS Settings"
		dns, ok := c.Engine.DNS[c.Current]
		if !ok {
			dns = state.DefaultDNS()
		}
		fields = []Field{optionField("mode", "Enhanced Mode", dns.Mode, "fake-ip", "redir-host"), booleanField("ipv6", "DNS IPv6", dns.IPv6), {Key: "nameservers", Label: "Nameservers (comma separated)", Text: strings.Join(dns.Nameservers, ", ")}, {Key: "default-nameservers", Label: "Default Nameservers (comma separated)", Text: strings.Join(dns.DefaultNameservers, ", ")}, {Key: "fallback", Label: "Fallback Nameservers (comma separated)", Text: strings.Join(dns.Fallback, ", ")}, {Key: "fake-ip-range", Label: "Fake IP Range", Text: dns.FakeIPRange}}
	case "language":
		m.preferenceList("language", p.Language)
		return
	case "theme":
		m.preferenceList("theme", p.Theme)
		return
	case "start-page":
		m.preferenceList(section, p.StartPage)
		return
	case "misc":
		kind, title = "preferences-save", "Miscellaneous"
		fields = []Field{{Key: "delay-url", Label: "Default Latency Test URL (empty = group default)", Text: p.DelayURL}, {Key: "delay-timeout", Label: "Default Latency Test Timeout (ms)", Text: strconv.Itoa(p.DelayTimeout)}, optionField("proxy-columns", "Proxy Layout Columns (0 = Auto)", strconv.Itoa(p.ProxyColumns), "0", "1", "2", "3", "4", "5")}
	default:
		return
	}
	m.openSettingsForm(title, kind, section, fields)
	if section == "dns" {
		m.Modal.Body = []string{"Applies to the active profile. Saving enables DNS Overwrite."}
	}
	if section == "system-proxy" {
		address := m.View.Desktop.Address
		if address == "" {
			address = net.JoinHostPort(p.SystemProxy.Host, strconv.Itoa(c.MixedPort))
		}
		m.Modal.Body = []string{
			"Enable Status: " + m.tr(toggle(m.View.Desktop.Enabled)),
			"Server Addr: " + address,
			"Bypass: " + strings.Join(state.DefaultProxyBypass(), ", "),
		}
	}
}

func (m *Model) settingsInfo(kind string) {
	title := "Settings"
	var body []string
	root := m.View.DataRoot
	switch kind {
	case "tun":
		title, body = "TUN Mode", []string{"TUN requires a system installation and its root service."}
	case "system-proxy":
		title, body = "System Proxy", []string{m.View.Desktop.Detail, "Use Proxy Environment on a headless server."}
	case "config-dir":
		title, body = "Configuration Directory", []string{root, filepath.Join(root, "profiles")}
	case "core-dir":
		title, body = "Core Directory", []string{filepath.Join(root, "cores")}
	case "logs-dir":
		title, body = "Logs Directory", []string{filepath.Join(root, "runtime"), filepath.Join(root, "runtime", "core.log")}
	case "diagnostics":
		title, body = "Diagnostic Information", []string{"Verge TUI: " + m.View.Version, "Core: " + m.View.Settings.Core, "OS: " + m.View.OS, "Install Mode: " + m.View.Settings.InstallMode, "Proxy Mode: " + m.mode(), "Core Online: " + strconv.FormatBool(m.View.Online), "System Proxy: " + toggle(m.View.Desktop.Enabled), "TUN: " + toggle(m.View.Settings.Tun), "Mixed Port: " + strconv.Itoa(m.View.Settings.MixedPort), "Data Directory: " + root}
	}
	m.Modal, m.Focus = &Modal{Title: title, Body: body}, ""
}

func (m *Model) showSettingsView(kind string, body []string) {
	if kind == "controller" && len(body) == 3 {
		m.openSettingsForm("External Controller", "settings-save", "controller", []Field{
			booleanField("enabled", "Enable External Controller", body[0] == "true"),
			{Key: "address", Label: "External Controller", Text: body[1]},
			{Key: "secret", Label: "Secret", Text: body[2]},
		})
		m.Modal.Choices = []Choice{{"Copy Address", app.Action{Kind: "copy-field", ID: "address"}}, {"Copy Secret", app.Action{Kind: "copy-field", ID: "secret"}}}
		return
	}
	title := "Network Interface"
	if kind == "runtime" {
		title = "Runtime Config"
		body = append([]string{"Managed configuration; credentials and provider URLs are redacted."}, body...)
	}
	m.Modal, m.Focus = &Modal{Title: title, Kind: "viewer", Body: body}, ""
}

func (m *Model) optionLabel(field Field) string {
	if len(field.Options) > 0 {
		return m.tr(selectionLabel(field.Key, field.Text))
	}
	return field.Text
}

func selectionLabel(key, value string) string {
	switch key {
	case "theme":
		return themeLabel(value)
	case "language":
		return i18n.Names[value]
	case "start-page":
		return map[string]string{"home": "Home", "proxies": "Proxies", "profiles": "Profiles", "settings": "Settings", "connections": "Connections", "rules": "Rules", "logs": "Logs"}[value]
	case "level":
		return map[string]string{"debug": "Debug", "info": "Info", "warning": "Warn", "error": "Error", "silent": "Silent"}[value]
	}
	if value == "true" {
		return "On"
	}
	if value == "false" {
		return "Off"
	}
	return value
}

func (m *Model) tr(english string) string { return i18n.Text(m.View.Preferences.Language, english) }

func (m *Model) selectionList(title, kind, section, key, current string, options []string) {
	modal := &Modal{Title: title, Kind: "selection-list", Section: section, List: true, RevealFocus: true}
	m.Focus = ""
	for _, id := range options {
		action := app.Action{Kind: kind, ID: section, Value: id, Fields: map[string]string{key: id}}
		modal.Choices = append(modal.Choices, Choice{Label: selectionLabel(key, id), Action: action})
		if id == current {
			m.Focus = Key(action)
		}
	}
	if m.Focus == "" && len(modal.Choices) > 0 {
		m.Focus = Key(modal.Choices[0].Action)
	}
	m.Modal = modal
}

func (m *Model) preferenceList(section, current string) {
	title, options := "Theme", state.Themes
	switch section {
	case "language":
		title, options = "Language", i18n.Languages
	case "start-page":
		title, options = "Start Page", []string{"home", "proxies", "profiles", "connections", "rules", "logs", "settings"}
	}
	m.selectionList(title, "preferences-save", section, section, current, options)
}

// Enum choices inside a form only edit that form; its Save button persists them.
func (m *Model) optionList(key string) {
	if m.Modal == nil {
		return
	}
	parent, focus := m.Modal, m.Focus
	for _, field := range parent.Fields {
		if field.Key == key && len(field.Options) > 0 && !m.fieldDisabled(key) {
			m.selectionList(field.Label, "select-option", key, key, field.Text, field.Options)
			m.Modal.Parent, m.Modal.ParentFocus = parent, focus
			return
		}
	}
}

func (m *Model) choiceSelected(action app.Action) bool {
	var current string
	switch action.Kind {
	case "preferences-save":
		switch action.ID {
		case "theme":
			current = m.View.Preferences.Theme
			if current == "" {
				current = state.DefaultPreferences().Theme
			}
		case "language":
			current = m.View.Preferences.Language
			if current == "" {
				current = state.DefaultPreferences().Language
			}
		case "start-page":
			current = m.View.Preferences.StartPage
			if current == "" {
				current = state.DefaultPreferences().StartPage
			}
		default:
			return false
		}
	case "settings-save":
		if action.ID != "log-level" {
			return false
		}
		current = m.View.CoreOptions.LogLevel
		if current == "" {
			current = "info"
		}
		if current == "warn" {
			current = "warning"
		}
	case "select-option":
		if m.Modal == nil || m.Modal.Parent == nil {
			return false
		}
		for _, field := range m.Modal.Parent.Fields {
			if field.Key == action.ID {
				current = field.Text
				break
			}
		}
	default:
		return false
	}
	return action.Value == current
}
