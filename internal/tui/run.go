package tui

import (
	"context"
	"errors"
	"sync"
	"time"
	"verge-tui/internal/controller"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
)

type result struct {
	View   app.View
	Action app.Action
	Err    error
	Delay  *app.DelayResult
	Body   []string
	Page   *app.PageData
}

func Run(ctx context.Context, b *app.Backend) (runErr error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	restoreWheel, err := prepareWheel(screen)
	defer func() { runErr = errors.Join(runErr, restoreWheel()) }()
	if err != nil {
		return err
	}
	screen.SetStyle(tcell.StyleDefault)
	screen.HideCursor()
	screen.EnableMouse(tcell.MouseButtonEvents)
	screen.EnablePaste()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan app.Action, 1)
	results := make(chan result, app.DelayConcurrency)
	var worker sync.WaitGroup
	var shutdownErr error
	worker.Go(func() {
		defer func() { shutdownErr = b.Close(context.Background()) }()
		for {
			select {
			case <-ctx.Done():
				return
			case action := <-jobs:
				var err error
				var body []string
				var page *app.PageData
				if action.Kind == "profile-info" || action.Kind == "profile-file" || action.Kind == "page-refresh" {
					kind := action.Kind
					id := action.ID
					if action.Kind == "page-refresh" {
						kind = action.ID
						id = ""
					}
					data, e := b.ReadPage(ctx, kind, id)
					err = e
					page = &data
				} else if action.Kind == "delay" {
					err = b.TestDelay(ctx, action, func(update app.DelayResult) {
						select {
						case results <- result{Delay: &update}:
						case <-ctx.Done():
						}
					})
				} else if action.Kind == "settings-view" {
					body, err = b.Inspect(ctx, action.ID)
				} else {
					err = b.Execute(ctx, action)
				}
				var view app.View
				if page == nil {
					var loadErr error
					view, loadErr = b.Load(ctx)
					err = errors.Join(err, loadErr)
					if action.Kind == "provider-update" || action.Kind == "providers-update" {
						data, e := b.ReadPage(ctx, "rules", "")
						page = &data
						err = errors.Join(err, e)
					}
				}
				select {
				case results <- result{View: view, Action: action, Err: err, Body: body, Page: page}:
				case <-ctx.Done():
					return
				}
			}
		}
	})
	defer func() { cancel(); worker.Wait(); runErr = errors.Join(runErr, shutdownErr) }()
	m := New(app.View{Version: b.Version})
	m.Busy = true
	m.Message = "Loading"
	jobs <- app.Action{Kind: "refresh"}
	updates := make(chan pageUpdate, 128)
	var watchers sync.WaitGroup
	var stopPage context.CancelFunc
	generation, watchedTab, watchedRevision := 0, -1, -1
	watchedLevel := ""
	var pendingLogs []controller.Log
	flush := time.NewTicker(200 * time.Millisecond)
	flush.Stop()
	var flushChannel <-chan time.Time
	defer flush.Stop()
	defer func() {
		if stopPage != nil {
			stopPage()
		}
		watchers.Wait()
	}()
	syncPage := func() {
		desired := -1
		if m.Tab >= 4 && m.View.Online && !(m.Tab == 6 && m.LogsPaused) {
			desired = m.Tab
		}
		if desired == watchedTab && watchedRevision == m.WatchRevision && (desired != 6 || watchedLevel == m.View.CoreOptions.LogLevel) {
			return
		}
		if stopPage != nil {
			stopPage()
		}
		generation++
		watchedTab = desired
		watchedRevision = m.WatchRevision
		watchedLevel = m.View.CoreOptions.LogLevel
		pendingLogs = nil
		flush.Stop()
		flushChannel = nil
		if desired == 6 {
			flush.Reset(200 * time.Millisecond)
			flushChannel = flush.C
		}
		if desired < 0 {
			return
		}
		pageCtx, stop := context.WithCancel(ctx)
		stopPage = stop
		currentGeneration := generation
		watchers.Go(func() { watchPage(pageCtx, b, desired, currentGeneration, updates) })
	}
	redraw := func() { w, h := screen.Size(); m.Render(w, h).Show(screen) }
	redraw()
	for !m.Quit {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updates:
			if update.Generation != generation {
				continue
			}
			if update.Log != nil {
				pendingLogs = append(pendingLogs, *update.Log)
				if len(pendingLogs) > 1000 {
					pendingLogs = pendingLogs[len(pendingLogs)-1000:]
				}
				m.PageError[6] = ""
				continue
			}
			m.applyPage(update.Tab, update.Data, update.Err)
		case <-flushChannel:
			if len(pendingLogs) == 0 {
				continue
			}
			m.AppendLogs(pendingLogs)
			pendingLogs = nil
		case done := <-results:
			if done.Delay != nil {
				m.ApplyDelay(*done.Delay)
			} else {
				if done.Page != nil && (done.Action.Kind == "profile-info" || done.Action.Kind == "profile-file" || done.Action.Kind == "page-refresh") {
					m.Busy = false
					if done.Action.Kind == "page-refresh" {
						for _, p := range navigation {
							if p.Kind == done.Action.ID && p.Tab >= 4 {
								m.applyPage(p.Tab, *done.Page, done.Err)
							}
						}
					} else if done.Err == nil {
						m.showProfileDocument(done.Action.Kind, done.Page.Document)
					} else {
						m.Message = "Error: " + done.Err.Error()
						m.Modal = &Modal{Title: "Action Failed", Body: []string{done.Err.Error()}}
					}
				} else {
					m.Complete(done.View, done.Action, done.Err)
					if done.Page != nil {
						m.applyPage(5, *done.Page, done.Err)
					}
				}
				if done.Action.Kind == "settings-view" && done.Err == nil {
					m.showSettingsView(done.Action.ID, done.Body)
				}
			}
		case ev := <-screen.EventQ():
			if ev == nil {
				return nil
			}
			if e, ok := ev.(*tcell.EventError); ok {
				return e
			}
			if mouse, ok := ev.(*tcell.EventMouse); ok && mouse.Buttons()&mouseWheelMask == 0 && mouse.Buttons()&tcell.Button1 != 0 && !m.MouseDown && m.Frame != nil {
				x, y := mouse.Position()
				for i := len(m.Frame.Hits) - 1; i >= 0; i-- {
					hit := m.Frame.Hits[i]
					if hit.Rect.Contains(x, y) {
						if hit.Action.Kind == "paste" {
							screen.GetClipboard()
						}
						if hit.Action.Kind == "copy-environment" || hit.Action.Kind == "copy-field" {
							screen.SetClipboard([]byte(m.clipboardText(hit.Action)))
						}
						break
					}
				}
			}
			if key, ok := ev.(*tcell.EventKey); ok && (key.Key() == tcell.KeyEnter || key.Key() == tcell.KeyRune && key.Str() == " ") && m.Input() == nil && m.Frame != nil {
				for _, hit := range m.Frame.Hits {
					if hit.Key == m.Focus {
						if hit.Action.Kind == "paste" {
							screen.GetClipboard()
						}
						if hit.Action.Kind == "copy-environment" || hit.Action.Kind == "copy-field" {
							screen.SetClipboard([]byte(m.clipboardText(hit.Action)))
						}
						break
					}
				}
			}
			if action := m.Handle(ev); action != nil && !m.Busy {
				m.Busy = true
				m.Message = action.Kind
				jobs <- *action
			}
		}
		syncPage()
		redraw()
	}
	return nil
}
