package tui

import (
	"context"
	"time"

	"verge-tui/internal/app"
	"verge-tui/internal/controller"
)

type pageUpdate struct {
	Generation int
	Tab        int
	Data       app.PageData
	Err        error
	Log        *controller.Log
}

func watchPage(ctx context.Context, b *app.Backend, tab, generation int, updates chan<- pageUpdate) {
	send := func(update pageUpdate) bool {
		update.Generation, update.Tab = generation, tab
		select {
		case updates <- update:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if tab == 6 {
		for ctx.Err() == nil {
			dropped := 0
			err := b.Logs(ctx, func(entry controller.Log) error {
				if dropped > 0 {
					warning := controller.Log{Time: time.Now().Format("15:04:05"), Type: "warning", Payload: "Some log entries were dropped to keep the interface responsive."}
					select {
					case updates <- pageUpdate{Generation: generation, Tab: tab, Log: &warning}:
						dropped = 0
					default:
					}
				}
				select {
				case updates <- pageUpdate{Generation: generation, Tab: tab, Log: &entry}:
				default:
					dropped++
				}
				return ctx.Err()
			})
			if ctx.Err() != nil {
				return
			}
			if !send(pageUpdate{Err: err}) {
				return
			}
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		data, err := b.ReadPage(ctx, pageKind(tab), "")
		if !send(pageUpdate{Data: data, Err: err}) || tab == 5 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (m *Model) applyPage(tab int, data app.PageData, err error) {
	if err != nil {
		m.PageError[tab] = err.Error()
		return
	}
	m.PageError[tab] = ""
	if tab == 4 {
		m.ApplyConnections(data.Connections, time.Now())
	}
	if tab == 5 {
		m.Rules = data.Rules
	}
}
