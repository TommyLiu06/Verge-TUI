package tui

import (
	"context"
	"errors"
	"sync"
	"time"
	"verge-tui/internal/controller"
	"verge-tui/internal/state"
	"verge-tui/internal/updater"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
)

type result struct {
	Preferences      *state.Preferences
	PreferencesEpoch uint64
	TaskID           uint64
	View             app.View
	Action           app.Action
	Err              error
	Delay            *app.DelayResult
	Body             []string
	Page             *app.PageData
}

type RunOptions struct{ OpenUpdates bool }
type updateNotice struct {
	Request *updater.Request
	Reply   chan error
}

func Run(ctx context.Context, b *app.Backend, options ...RunOptions) (runErr error) {
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
	runner := newTaskRunner(ctx, b)
	defer func() {
		cancel()
		runner.cancelDelay()
		runner.workers.Wait()
		runErr = errors.Join(runErr, b.Close(context.Background()))
	}()
	m := New(app.View{Version: b.Version})
	m.UpdateNeedsAdmin = b.Remote != nil
	showUpdates := len(options) > 0 && options[0].OpenUpdates
	initial := app.Action{Kind: "refresh"}
	m.beginTask(initial)
	m.Message = "Loading"
	runner.submit(initial)
	updates := make(chan pageUpdate, 128)
	var watchers sync.WaitGroup
	updateRequests := make(chan updateNotice)
	updateErrors := make(chan error, 1)
	if b.Remote == nil {
		watchers.Go(func() {
			u := updater.Updater{Store: b.Store, Version: b.Version}
			err := u.Run(ctx, func(ctx context.Context, request *updater.Request) error {
				status, err := u.Status()
				if err != nil {
					return err
				}
				if err := u.AllowAutomatic(ctx, status); err != nil {
					return err
				}
				notice := updateNotice{Request: request, Reply: make(chan error, 1)}
				select {
				case updateRequests <- notice:
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case err := <-notice.Reply:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if err != nil && ctx.Err() == nil {
				updateErrors <- err
			}
		})
	}
	var stopPage context.CancelFunc
	generation, watchedTab, watchedRevision := 0, -1, -1
	watchedLevel := ""
	var pendingLogs []controller.Log
	flush := time.NewTicker(200 * time.Millisecond)
	flush.Stop()
	var flushChannel <-chan time.Time
	defer flush.Stop()
	defer func() {
		cancel()
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
	updatePoll := time.NewTicker(time.Second)
	updatePoll.Stop()
	defer updatePoll.Stop()
	var updatePollChannel <-chan time.Time
	var updatePollInterval time.Duration
	redraw()
	for !m.Quit {
		select {
		case <-ctx.Done():
			return nil
		case err := <-updateErrors:
			m.AppUpdate.Error = err.Error()
			m.Message = "Automatic updates: " + err.Error()
			if m.Modal != nil && m.Modal.Kind == "app-update" {
				m.updateModal()
			}
		case notice := <-updateRequests:
			if m.Editor != nil || len(m.Tasks) > 0 || m.Modal != nil && len(m.Modal.Fields) > 0 {
				notice.Reply <- updater.ErrDeferred
				continue
			}
			// The foreground core is closed with the TUI. Its idle policy has
			// already been checked while the controller was still reachable.
			if b.Manager.Owned() {
				notice.Request.Automatic = false
			}
			m.UpdateRequest, m.Quit = notice.Request, true
			notice.Reply <- nil
		case <-updatePollChannel:
			if m.updatePollingInterval() == 0 {
				continue
			}
			a := app.Action{Kind: "app-update", ID: "status"}
			if _, pending := m.Tasks[taskKey(a)]; !pending && len(m.Tasks) < maxPendingTasks {
				m.beginTask(a)
				runner.submit(a)
			}
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
		case done := <-runner.results:
			if (done.Delay != nil || done.Action.Kind == "delay") && done.TaskID != runner.DelayID {
				continue
			}
			if done.Delay != nil {
				m.ApplyDelay(*done.Delay)
			} else {
				if done.Action.Kind == "app-update" || done.Action.Kind == "update-options" {
					m.completeUpdate(done)
				} else if done.Action.Kind == "core-releases" {
					m.finishTask(done.Action)
					if done.Page != nil {
						m.showCoreReleases(done.Action, done.Page.Releases, done.Err)
					}
				} else if done.Page != nil && (done.Action.Kind == "profile-info" || done.Action.Kind == "profile-file" || done.Action.Kind == "page-refresh") {
					m.finishTask(done.Action)
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
					m.completeTask(done)
					if done.Action.Kind == "delay" {
						runner.cancelDelay()
					}
					if !m.DelayActive && runner.DelayID != 0 {
						runner.cancelDelay()
					}
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
			if action := m.Handle(ev); action != nil {
				if invalidatesDelay(*action, m.View) {
					runner.cancelDelay()
					m.cancelDelay()
				}
				if m.beginTask(*action) {
					runner.submit(*action)
				}
			}
		}
		if showUpdates && m.Loaded {
			showUpdates = false
			if action := m.Activate(app.Action{Kind: "update-open"}); action != nil && m.beginTask(*action) {
				runner.submit(*action)
			}
		}
		syncPage()
		if interval := m.updatePollingInterval(); interval != updatePollInterval {
			updatePollInterval = interval
			if interval > 0 {
				updatePoll.Reset(interval)
				updatePollChannel = updatePoll.C
			} else {
				updatePoll.Stop()
				updatePollChannel = nil
			}
		}
		redraw()
	}
	if m.UpdateRequest != nil {
		m.UpdateRequest.ResumeCore = b.Manager.Owned()
		return m.UpdateRequest
	}
	return nil
}
