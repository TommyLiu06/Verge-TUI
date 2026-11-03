package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/clipperhouse/displaywidth"
	"verge-tui/internal/app"
	"verge-tui/internal/updater"
)

func (m *Model) updateInterval(minutes int, idle bool) string {
	if minutes == 0 {
		return m.tr("Manual")
	}
	if idle {
		return fmt.Sprintf(m.tr("After %d minutes without active connections"), minutes)
	}
	return fmt.Sprintf(m.tr("Every %d minutes"), minutes)
}

func (m *Model) updateModal() {
	s := m.AppUpdate
	modal := &Modal{Title: "Check for Updates", Kind: "app-update"}
	modal.Footer = []Choice{{"Check", app.Action{Kind: "app-update", ID: "check"}}}
	modal.Header = []Choice{
		{m.tr("Auto Check Updates") + ": " + m.updateInterval(s.Options.CheckMinutes, false), app.Action{Kind: "update-policy-list", ID: "check"}},
		{m.tr("Auto Update") + ": " + m.updateInterval(s.Options.IdleMinutes, true), app.Action{Kind: "update-policy-list", ID: "idle"}},
	}
	current := s.CurrentVersion
	if current == "" {
		current = m.View.Version
	}
	modal.Body = []string{m.tr("Current Version") + ": v" + current}
	if updater.Newer(current, m.View.Version) {
		modal.Body = append(modal.Body, m.tr("TUI Version")+": v"+m.View.Version)
		modal.Footer = append(modal.Footer, Choice{"Restart", app.Action{Kind: "update-restart"}})
	}
	if !s.LastChecked.IsZero() {
		modal.Body = append(modal.Body, m.tr("Last Check Update")+": "+s.LastChecked.Local().Format("2006-01-02 15:04"))
	}
	if s.Release != nil {
		modal.Body = append(modal.Body, m.tr("Latest Version")+": v"+s.Release.Version)
	}
	phase := map[string]string{"checking": "Checking for Updates...", "available": "A new version is available", "downloading": "Downloading...", "verifying": "Verifying update...", "ready": "Ready to Update", "applying": "Installing update...", "latest": "Currently on the Latest Version"}[s.Phase]
	if phase != "" {
		modal.Body = append(modal.Body, m.tr(phase))
	}
	if s.Phase == "downloading" && s.Release != nil && s.Release.Size > 0 {
		percent := min(100, int(s.Downloaded*100/s.Release.Size))
		modal.Body = append(modal.Body, fmt.Sprintf("[%s%s] %d%%  %s / %s", strings.Repeat("=", percent/5), strings.Repeat(" ", 20-percent/5), percent, bytesString(uint64(s.Downloaded)), bytesString(uint64(s.Release.Size))))
	}
	if !s.IdleSince.IsZero() && s.Options.IdleMinutes > 0 && s.Ready {
		modal.Body = append(modal.Body, m.tr("Waiting for idle connections")+": "+s.IdleSince.Local().Format("15:04:05"))
	}
	if !s.Supported {
		modal.Body = append(modal.Body, m.tr("Application updates require an installed offline package."))
	}
	if s.Error != "" {
		modal.Body = append(modal.Body, m.tr("Error")+": "+Clean(s.Error))
	}
	if m.View.Settings.InstallMode == "system" {
		modal.Body = append(modal.Body, m.tr("System installation updates require administrator authorization."))
	}
	if s.Available() && s.Release != nil {
		modal.Title = strings.ReplaceAll(m.tr("New Version v{{version}}"), "{{version}}", s.Release.Version)
		modal.Body = append(modal.Body, "", m.tr("Release Notes"))
		width := 76
		if m.Frame != nil {
			width = max(16, min(m.Frame.W-10, 76))
		}
		for _, line := range strings.Split(s.Release.Notes, "\n") {
			line = Clean(line)
			for displaywidth.String(line) > width {
				part := displaywidth.TruncateString(line, width, "")
				modal.Body = append(modal.Body, part)
				line = strings.TrimPrefix(line, part)
			}
			modal.Body = append(modal.Body, line)
		}
		if s.Supported && s.Phase != "checking" && s.Phase != "downloading" && s.Phase != "verifying" && s.Phase != "applying" {
			modal.Footer = append(modal.Footer, Choice{"Update", app.Action{Kind: "update-install"}})
		}
	}
	if m.Modal != nil && m.Modal.Kind == "app-update" {
		modal.Scroll = m.Modal.Scroll
	}
	m.Modal = modal
	modal.Scroll = min(modal.Scroll, max(0, m.modalContentHeight()-m.modalViewport()))
}

func (m *Model) updatePolicyList(section string) {
	if !m.AppUpdate.Supported {
		m.Message = "Application updates require an installed offline package."
		return
	}
	parent, focus := m.Modal, m.Focus
	title, key, current := "Auto Check Updates", "check-minutes", m.AppUpdate.Options.CheckMinutes
	options := []string{"0", "15", "30", "60", "360", "720", "1440"}
	if section == "idle" {
		title, key, current = "Auto Update", "idle-minutes", m.AppUpdate.Options.IdleMinutes
		options = []string{"0", "5", "10", "15", "30", "60"}
	}
	value := strconv.Itoa(current)
	found := false
	for _, option := range options {
		found = found || option == value
	}
	if !found {
		options = append(options, value)
	}
	m.selectionList(title, "update-options", section, key, value, options)
	for i := range m.Modal.Choices {
		choice := &m.Modal.Choices[i]
		n, _ := strconv.Atoi(choice.Action.Value)
		choice.Label = m.updateInterval(n, section == "idle")
	}
	m.Modal.Choices = append(m.Modal.Choices, Choice{"Custom...", app.Action{Kind: "update-custom", ID: section}})
	m.Modal.Parent, m.Modal.ParentFocus = parent, focus
}

func (m *Model) applyUpdateOptions(fields map[string]string) *app.Action {
	options := m.AppUpdate.Options
	for key, value := range fields {
		n, err := strconv.Atoi(value)
		if err != nil {
			m.Message = "Update intervals must be whole minutes"
			return nil
		}
		if key == "check-minutes" {
			options.CheckMinutes = n
		} else if key == "idle-minutes" {
			options.IdleMinutes = n
		} else {
			return nil
		}
	}
	if err := options.Validate(); err != nil {
		m.Message = err.Error()
		return nil
	}
	if m.UpdateNeedsAdmin {
		m.UpdateRequest, m.Quit = &updater.Request{Configure: &options, ConfigureFields: fields}, true
		return nil
	}
	a := app.Action{Kind: "update-options", Fields: fields}
	if m.canRun(a) {
		return &a
	}
	return nil
}

func (m *Model) updateAction(a app.Action) (bool, *app.Action) {
	switch a.Kind {
	case "update-open":
		m.UpdateInstalling = false
		m.updateModal()
		m.Focus = Key(app.Action{Kind: "app-update", ID: "check"})
		return true, &app.Action{Kind: "app-update", ID: "status"}
	case "update-policy-list":
		m.updatePolicyList(a.ID)
	case "update-custom":
		key, value, title := "check-minutes", m.AppUpdate.Options.CheckMinutes, "Check Interval (minutes, 0 = Manual)"
		if a.ID == "idle" {
			key, value, title = "idle-minutes", m.AppUpdate.Options.IdleMinutes, "Idle Time (minutes, 0 = Manual)"
		}
		m.openSettingsForm("Custom Update Policy", "update-options", a.ID, []Field{{Key: key, Label: title, Text: strconv.Itoa(value)}})
	case "update-options":
		return true, m.applyUpdateOptions(a.Fields)
	case "submit":
		if m.Modal == nil || m.Modal.Kind != "update-options" {
			return false, nil
		}
		fields := map[string]string{}
		for _, field := range m.Modal.Fields {
			fields[field.Key] = field.Text
		}
		return true, m.applyUpdateOptions(fields)
	case "update-install":
		if !m.AppUpdate.Supported || !m.AppUpdate.Available() {
			return true, nil
		}
		if m.AppUpdate.Ready {
			m.UpdateRequest, m.Quit = &updater.Request{Version: m.AppUpdate.Release.Version}, true
			return true, nil
		}
		a = app.Action{Kind: "app-update", ID: "download"}
		if m.canRun(a) {
			m.UpdateInstalling = true
			return true, &a
		}
	case "update-restart":
		if updater.Newer(m.AppUpdate.CurrentVersion, m.View.Version) {
			m.UpdateRequest, m.Quit = &updater.Request{RestartOnly: true}, true
		}
	case "app-update":
		if m.canRun(a) {
			return true, &a
		}
	case "close", "navigate":
		if m.Modal != nil && m.Modal.Kind == "app-update" {
			m.UpdateInstalling = false
		}
		return false, nil
	default:
		return false, nil
	}
	return true, nil
}

func (m *Model) completeUpdate(done result) {
	m.finishTask(done.Action)
	if done.Page != nil && done.Page.Update != nil && done.Page.Update.CurrentVersion != "" {
		m.AppUpdate = *done.Page.Update
	}
	if done.Err != nil {
		m.AppUpdate.Error = done.Err.Error()
		m.UpdateInstalling = false
	}
	if m.Modal == nil || m.Modal.Kind != "app-update" && !(done.Action.Kind == "update-options" && (m.Modal.Kind == "update-options" || m.Modal.Kind == "selection-list" && (m.Modal.Section == "check" || m.Modal.Section == "idle"))) {
		return
	}
	if done.Action.Kind == "update-options" {
		m.Focus = ""
	}
	m.updateModal()
	if m.UpdateInstalling && done.Err == nil && m.AppUpdate.Ready && m.AppUpdate.Available() {
		m.UpdateRequest, m.Quit = &updater.Request{Version: m.AppUpdate.Release.Version}, true
	}
}
