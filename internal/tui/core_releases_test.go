package tui

import (
	"context"
	"errors"
	"github.com/gdamore/tcell/v3"
	"testing"

	"verge-tui/internal/app"
	"verge-tui/internal/core"
)

func TestCoreInstallUsesOfficialListAndIgnoresClosedOrStaleRequests(t *testing.T) {
	m := New(fixture())
	request := m.Activate(app.Action{Kind: "core-form"})
	if request == nil || request.Kind != "core-releases" || m.Modal == nil || !m.Modal.List || len(m.Modal.Fields) != 0 {
		t.Fatal("install did not request an official version list")
	}
	page := &core.ReleasePage{Releases: []core.ReleaseOption{{Tag: "v2", ID: "v2"}}, NextPage: 2}
	m.showCoreReleases(*request, page, nil)
	if len(m.Modal.Choices) != 3 || m.Modal.Choices[0].Action.Value != "v2" || m.Modal.Choices[1].Action.ID != "2" {
		t.Fatal("official versions or pagination were not displayed", m.Modal)
	}
	loadMore := m.Modal.Choices[1].Action
	m.showCoreReleases(loadMore, &core.ReleasePage{Releases: []core.ReleaseOption{{Tag: "v2", ID: "v2"}, {Tag: "v1", ID: "v1"}}}, nil)
	if len(m.CoreReleases) != 2 {
		t.Fatal("pagination duplicated versions")
	}
	if m.Focus != Key(app.Action{Kind: "core-install", Value: "v1"}) {
		t.Fatal("pagination did not focus the newly loaded versions")
	}
	m.Modal = nil
	m.showCoreReleases(*request, page, nil)
	if m.Modal != nil {
		t.Fatal("a closed version list was reopened")
	}
	newRequest := m.Activate(app.Action{Kind: "core-form"})
	m.showCoreReleases(*request, page, nil)
	if len(m.CoreReleases) != 0 {
		t.Fatal("an old request replaced a newly opened version list")
	}
	m.showCoreReleases(*newRequest, nil, errors.New("release request failed"))
	if m.Modal.Choices[0].Label != "Retry" || len(m.CoreReleases) != 0 {
		t.Fatal("a failed request did not offer retry or invented a fallback version")
	}
}

func TestOfficialVersionCanBeChosenWithKeyboard(t *testing.T) {
	m := New(fixture())
	request := m.Activate(app.Action{Kind: "core-form"})
	m.showCoreReleases(*request, &core.ReleasePage{Releases: []core.ReleaseOption{{Tag: "v2", ID: "v2"}}}, nil)
	m.Render(100, 24)
	action := m.Handle(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if action == nil || action.Kind != "core-install" || action.Value != "v2" || m.Modal != nil {
		t.Fatal("keyboard choice did not install the selected official version", action)
	}
}

func TestCoreCatalogTaskReadsDataWithoutMutatingOrReloadingView(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &taskFixture{view: fixture(), executed: make(chan app.Action, 1)}
	runner := newTaskRunner(ctx, b)
	defer func() { cancel(); runner.workers.Wait() }()
	runner.submit(app.Action{Kind: "core-releases", ID: "1", Node: "1"})
	done := awaitTask(t, runner.results)
	if done.Err != nil || done.Page == nil || done.View.Settings.Core != "" {
		t.Fatal("catalog task performed an unrelated full view load", done.Err)
	}
	select {
	case <-b.executed:
		t.Fatal("catalog task was submitted as a mutation")
	default:
	}
}
