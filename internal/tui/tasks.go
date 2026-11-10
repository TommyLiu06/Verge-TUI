package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"verge-tui/internal/app"
	"verge-tui/internal/state"
)

const maxPendingTasks = 32

type taskBackend interface {
	Execute(context.Context, app.Action) error
	TestDelay(context.Context, app.Action, func(app.DelayResult)) error
	ReadPage(context.Context, string, string) (app.PageData, error)
	Inspect(context.Context, string) ([]string, error)
	Load(context.Context) (app.View, error)
	ReadPreferences() (state.Preferences, error)
}
type task struct {
	ID      uint64
	Action  app.Action
	Context context.Context
}

// Configuration changes remain serial. A separate, single batch worker keeps
// delay testing bounded to four requests without blocking normal commands.
// Private preferences have their own queue so downloads cannot hold up the UI.
type taskRunner struct {
	ctx               context.Context
	normal, delay     chan task
	preferences       chan task
	appUpdates        chan task
	updateStatus      chan task
	preferencesEpoch  atomic.Uint64
	results           chan result
	workers           sync.WaitGroup
	sequence, DelayID uint64
	delayCancel       context.CancelFunc
}

func newTaskRunner(ctx context.Context, b taskBackend) *taskRunner {
	r := &taskRunner{ctx: ctx, normal: make(chan task, maxPendingTasks), delay: make(chan task, 1), results: make(chan result, app.DelayConcurrency)}
	r.preferences = make(chan task, maxPendingTasks)
	r.appUpdates = make(chan task, maxPendingTasks)
	r.updateStatus = make(chan task, maxPendingTasks)
	r.workers.Go(func() { r.work(b, r.normal) })
	r.workers.Go(func() { r.work(b, r.delay) })
	r.workers.Go(func() { r.work(b, r.preferences) })
	r.workers.Go(func() { r.work(b, r.appUpdates) })
	r.workers.Go(func() { r.work(b, r.updateStatus) })
	return r
}
func (r *taskRunner) submit(a app.Action) {
	r.sequence++
	job := task{ID: r.sequence, Action: a, Context: r.ctx}
	if a.Kind == "delay" {
		r.cancelDelay()
		select {
		case <-r.delay:
		default:
		}
		job.Context, r.delayCancel = context.WithCancel(r.ctx)
		r.DelayID = job.ID
		r.delay <- job
	} else if a.Kind == "app-update" && a.ID == "status" || a.Kind == "update-options" {
		r.updateStatus <- job
	} else if a.Kind == "app-update" {
		r.appUpdates <- job
	} else if privatePreference(a) {
		r.preferences <- job
	} else {
		r.normal <- job
	}
}
func (r *taskRunner) cancelDelay() {
	if r.delayCancel != nil {
		r.delayCancel()
		r.delayCancel = nil
	}
	r.DelayID = 0
}
func (r *taskRunner) work(b taskBackend, jobs <-chan task) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case job := <-jobs:
			if job.Context.Err() != nil {
				continue
			}
			done := result{TaskID: job.ID, Action: job.Action}
			a, ctx := job.Action, job.Context
			switch a.Kind {
			case "app-update":
				data, err := b.ReadPage(ctx, "app-update", a.ID)
				done.Page, done.Err = &data, err
			case "update-options":
				done.Err = b.Execute(ctx, a)
				data, err := b.ReadPage(ctx, "app-update", "status")
				done.Page = &data
				done.Err = errors.Join(done.Err, err)
			case "core-releases":
				data, err := b.ReadPage(ctx, "core-releases", a.ID)
				done.Page, done.Err = &data, err
			case "profile-info", "profile-file", "page-refresh":
				kind, id := a.Kind, a.ID
				if a.Kind == "page-refresh" {
					kind, id = a.ID, ""
				}
				data, err := b.ReadPage(ctx, kind, id)
				done.Page, done.Err = &data, err
			case "delay":
				done.Err = b.TestDelay(ctx, a, func(update app.DelayResult) {
					r.send(ctx, result{TaskID: job.ID, Delay: &update})
				})
			case "preferences-save":
				done.Err = b.Execute(ctx, a)
				if privatePreference(a) {
					if done.Err == nil {
						r.preferencesEpoch.Add(1)
					}
					p, err := b.ReadPreferences()
					done.Preferences = &p
					done.Err = errors.Join(done.Err, err)
					done.PreferencesEpoch = r.preferencesEpoch.Load()
				}
			case "settings-view":
				done.Body, done.Err = b.Inspect(ctx, a.ID)
			default:
				done.Err = b.Execute(ctx, a)
			}
			// A delay completion updates only latency data. Loading and replacing
			// the full view here could overwrite a concurrent configuration change.
			if done.Page == nil && done.Preferences == nil && a.Kind != "delay" {
				done.PreferencesEpoch = r.preferencesEpoch.Load()
				view, err := b.Load(ctx)
				done.View = view
				done.Err = errors.Join(done.Err, err)
				if a.Kind == "provider-update" || a.Kind == "providers-update" {
					data, err := b.ReadPage(ctx, "rules", "")
					done.Page = &data
					done.Err = errors.Join(done.Err, err)
				}
			}
			if !r.send(ctx, done) && r.ctx.Err() != nil {
				return
			}
		}
	}
}

func privatePreference(a app.Action) bool {
	return a.Kind == "preferences-save" && a.ID != "system-proxy"
}
func (m *Model) completeTask(done result) {
	if done.Preferences != nil {
		done.View = m.View
		if done.Err == nil && done.PreferencesEpoch >= m.PreferencesEpoch {
			// Desktop proxy changes belong to the normal queue. A private
			// preference result must not restore an older desktop setting.
			systemProxy := done.View.Preferences.SystemProxy
			done.View.Preferences = *done.Preferences
			done.View.Preferences.SystemProxy = systemProxy
			m.PreferencesEpoch = done.PreferencesEpoch
		}
	} else if done.Action.Kind != "delay" {
		if done.PreferencesEpoch < m.PreferencesEpoch {
			systemProxy := done.View.Preferences.SystemProxy
			done.View.Preferences = m.View.Preferences
			done.View.Preferences.SystemProxy = systemProxy
		} else {
			m.PreferencesEpoch = done.PreferencesEpoch
		}
	}
	m.Complete(done.View, done.Action, done.Err)
}
func (r *taskRunner) send(ctx context.Context, done result) bool {
	select {
	case r.results <- done:
		return true
	case <-ctx.Done():
		return false
	}
}
func taskKey(a app.Action) string {
	if a.Kind == "delay" || a.Kind == "profile-file-save" || a.Kind == "profile-info-save" {
		return a.Kind
	}
	return a.Kind + "|" + a.ID + "|" + a.Node
}
func (m *Model) canRun(a app.Action) bool {
	if a.Kind == "delay" {
		for _, pending := range m.Tasks {
			if invalidatesDelay(pending, m.View) {
				m.Message = "Wait for the configuration change to finish before testing"
				return false
			}
		}
	}
	if m.PendingEditor != nil && a.Kind == "profile-file" || m.PendingProfile != nil && a.Kind == "profile-info" {
		m.Message = "An editor save is already in progress"
		return false
	}
	if _, pending := m.Tasks[taskKey(a)]; pending {
		m.Message = "This action is already in progress"
		return false
	}
	if len(m.Tasks) >= maxPendingTasks {
		m.Message = "Task queue is full; wait for a task to finish"
		return false
	}
	return true
}
func (m *Model) beginTask(a app.Action) bool {
	if !m.canRun(a) {
		return false
	}
	if m.Tasks == nil {
		m.Tasks = make(map[string]app.Action)
	}
	m.Tasks[taskKey(a)] = a
	m.refreshBusy()
	if message := taskMessage(a); message != "" {
		m.Message = message
	}
	if a.Kind == "delay" {
		m.DelayActive = true
		m.DelayMessage = ""
		m.Delays = nil
	}
	return true
}
func taskMessage(a app.Action) string {
	switch a.Kind {
	case "app-update":
		switch a.ID {
		case "status":
			return ""
		case "check":
			return "Checking for Updates..."
		case "download":
			return "Downloading..."
		}
	case "update-options":
		return "Custom Update Policy"
	case "core-releases":
		return "Loading official mihomo releases..."
	}
	return a.Kind
}
func (m *Model) refreshBusy() {
	m.Busy = false
	for _, pending := range m.Tasks {
		if taskMessage(pending) != "" {
			m.Busy = true
			return
		}
	}
}
func (m *Model) finishTask(a app.Action) {
	delete(m.Tasks, taskKey(a))
	m.refreshBusy()
}
func (m *Model) cancelDelay() {
	m.finishTask(app.Action{Kind: "delay"})
	m.DelayActive = false
	m.DelayMessage = ""
	m.Delays = nil
}
func invalidatesDelay(a app.Action, v app.View) bool {
	switch a.Kind {
	case "profile-select", "core-use", "stop", "start":
		return true
	case "profile-update", "profile-delete", "profile-file-save":
		return a.ID == v.Settings.Current
	case "settings-save":
		return a.ID == "controller"
	}
	return false
}
