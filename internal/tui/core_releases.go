package tui

import (
	"strconv"

	"verge-tui/internal/app"
	"verge-tui/internal/core"
)

func (m *Model) showCoreReleases(a app.Action, page *core.ReleasePage, err error) {
	if m.Modal == nil || m.Modal.Kind != "core-releases" || a.Node != strconv.Itoa(m.CoreReleaseEpoch) {
		return
	}
	previousCount := len(m.CoreReleases)
	previousScroll := m.Modal.Scroll
	if err == nil && page != nil {
		seen := make(map[string]bool, len(m.CoreReleases))
		for _, release := range m.CoreReleases {
			seen[release.ID] = true
		}
		for _, release := range page.Releases {
			if !seen[release.ID] {
				m.CoreReleases = append(m.CoreReleases, release)
				seen[release.ID] = true
			}
		}
		m.CoreReleaseNext = page.NextPage
	}
	modal := &Modal{Title: "Install Core", Kind: "core-releases", List: true, Body: []string{"Official MetaCubeX/mihomo releases"}, Scroll: previousScroll}
	for _, release := range m.CoreReleases {
		label := release.Tag
		if release.Prerelease {
			label += " (" + release.ID + ")"
		}
		action := app.Action{Kind: "core-install", Value: release.Tag}
		for _, installed := range m.View.Cores {
			if installed.ID == release.ID {
				label += " (Installed)"
				action = app.Action{Kind: "core-use", ID: release.ID}
				break
			}
		}
		modal.Choices = append(modal.Choices, Choice{label, action})
	}
	if err != nil {
		modal.Body = append(modal.Body, err.Error())
		modal.Choices = append(modal.Choices, Choice{"Retry", a})
		m.Message = "Error: " + err.Error()
	} else {
		m.Message = "Ready"
		if len(m.CoreReleases) == 0 {
			modal.Body = append(modal.Body, "No compatible verified release assets found.")
		}
		if m.CoreReleaseNext != 0 {
			modal.Choices = append(modal.Choices, Choice{"Load More", app.Action{Kind: "core-releases", ID: strconv.Itoa(m.CoreReleaseNext), Node: a.Node}})
		}
	}
	modal.Choices = append(modal.Choices, Choice{"Refresh", app.Action{Kind: "core-form"}})
	m.Modal, m.Focus = modal, ""
	if len(modal.Choices) > 0 {
		index := 0
		if a.ID != "1" && err == nil {
			index = min(previousCount, len(modal.Choices)-1)
		}
		m.Focus = Key(modal.Choices[index].Action)
		modal.RevealFocus = true
	}
}
