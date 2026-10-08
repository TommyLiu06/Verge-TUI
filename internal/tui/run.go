package tui

import (
	"context"
	"errors"
	"sync"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
)

type result struct {
	View   app.View
	Action app.Action
	Err    error
	Delay  *app.DelayResult
	Body   []string
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
				if action.Kind == "delay" {
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
				view, loadErr := b.Load(ctx)
				err = errors.Join(err, loadErr)
				select {
				case results <- result{View: view, Action: action, Err: err, Body: body}:
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
	redraw := func() { w, h := screen.Size(); m.Render(w, h).Show(screen) }
	redraw()
	for !m.Quit {
		select {
		case <-ctx.Done():
			return nil
		case done := <-results:
			if done.Delay != nil {
				m.ApplyDelay(*done.Delay)
			} else {
				m.Complete(done.View, done.Action, done.Err)
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
		redraw()
	}
	return nil
}
