package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"verge-tui/internal/controller"
)

type idleWindow struct {
	since, sample time.Time
	upload, down  uint64
}

func (w *idleWindow) observe(now time.Time, connections controller.Connections, err error) time.Time {
	traffic := !w.sample.IsZero() && (w.upload != connections.UploadTotal || w.down != connections.DownloadTotal)
	gap := !w.sample.IsZero() && (now.Sub(w.sample) > 15*time.Second || now.Before(w.sample))
	w.upload, w.down, w.sample = connections.UploadTotal, connections.DownloadTotal, now
	if err != nil || len(connections.Connections) > 0 || traffic || gap {
		w.since = time.Time{}
	} else if w.since.IsZero() {
		w.since = now
	}
	return w.since
}

func checkDue(s Status, now time.Time) bool {
	if s.Options.CheckMinutes == 0 || now.Before(s.RetryAfter) {
		return false
	}
	return s.LastAttempt.IsZero() || !now.Before(s.LastAttempt.Add(time.Duration(s.Options.CheckMinutes)*time.Minute))
}

func (u Updater) connections(ctx context.Context) (controller.Connections, error) {
	c, err := u.Store.Read()
	if err != nil {
		return controller.Connections{}, err
	}
	client := controller.New(c)
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return client.Connections(ctx)
}

func (u Updater) AllowAutomatic(ctx context.Context, s Status) error {
	options, err := u.Options()
	if err != nil {
		return err
	}
	if !options.UpdatesEnabled() || options != s.Options || s.IdleSince.IsZero() || time.Since(s.IdleSince) < time.Duration(options.IdleMinutes)*time.Minute {
		return errors.New("automatic update was cancelled or its idle period has not elapsed")
	}
	connections, err := u.connections(ctx)
	if err != nil {
		return err
	}
	if len(connections.Connections) > 0 || connections.UploadTotal != s.IdleUpload || connections.DownloadTotal != s.IdleDownload {
		return errors.New("automatic update postponed because there are active connections")
	}
	return nil
}

// One leader per installation, shared by the supervisor and any open TUIs.
// Manual policies make no network requests. Connection polling starts only
// after an automatic update has been downloaded and verified.
func (u Updater) Run(ctx context.Context, apply func(context.Context, *Request) error) error {
	if filepath.Base(u.Store.Root) != "data" {
		return nil
	}
	marker, err := os.Lstat(filepath.Join(filepath.Dir(u.Store.Root), ".verge-tui-install"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !marker.Mode().IsRegular() {
		return errors.New("invalid installation marker for automatic updates")
	}
	var leader interface{ Close() error }
	for leader == nil {
		lock, err := u.lock("scheduler")
		if err == nil {
			leader = lock
			break
		}
		if !errors.Is(err, ErrBusy) {
			return err
		}
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	defer leader.Close()
	var window idleWindow
	var previous Options
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		s, err := u.Status()
		if err != nil {
			return err
		}
		// A fresh installer can start the proxy service just before it records
		// the public installation summary. Keep the scheduler alive for it.
		if !s.Supported {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}
		if s.Options != previous {
			window = idleWindow{}
			previous = s.Options
		}
		if checkDue(s, time.Now()) {
			s, err = u.checkWhileIdle(ctx, s, &window)
		}
		if err == nil && s.Available() && s.Options.DownloadsEnabled() && !s.Ready && !time.Now().Before(s.RetryAfter) {
			s, err = u.prepareAutomatic(ctx)
			window = idleWindow{}
		}
		if err == nil && s.Ready && s.Available() && s.Options.UpdatesEnabled() && !time.Now().Before(s.RetryAfter) {
			var due bool
			s, due, err = u.observeIdle(ctx, s, &window)
			if err != nil {
				return err
			}
			if due {
				// The cached archive may have been superseded while waiting for
				// idle connections. Recheck before authorizing installation.
				if time.Since(s.LastChecked) >= time.Minute {
					s, err = u.checkWhileIdle(ctx, s, &window)
					if err == nil && !s.Ready && s.Available() && s.Options.DownloadsEnabled() {
						s, err = u.prepareAutomatic(ctx)
						window = idleWindow{}
					}
				}
				if err == nil && s.Ready {
					s, due, err = u.observeIdle(ctx, s, &window)
					if err != nil {
						return err
					}
					if due {
						if err := apply(ctx, &Request{Version: s.Release.Version, Automatic: true}); err != nil {
							if !errors.Is(err, ErrDeferred) {
								if err := u.automaticFailure(err); err != nil {
									return err
								}
								window = idleWindow{}
							}
						} else {
							if err := u.awaitApply(ctx); err != nil {
								return err
							}
							window = idleWindow{}
						}
					}
				}
			}
		} else {
			window = idleWindow{}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	return nil
}

// Only the updater's metadata request is excluded from byte-counter changes.
// Other active connections, failed probes and long gaps still reset the window.
func (w *idleWindow) afterCheck(now time.Time, connections controller.Connections, err error) {
	if err != nil || len(connections.Connections) > 0 || w.sample.IsZero() || now.Sub(w.sample) > 15*time.Second || now.Before(w.sample) {
		w.since = time.Time{}
	}
	w.upload, w.down, w.sample = connections.UploadTotal, connections.DownloadTotal, now
}

func (u Updater) checkWhileIdle(ctx context.Context, s Status, window *idleWindow) (Status, error) {
	watch := s.Ready && s.Options.UpdatesEnabled()
	if watch {
		connections, err := u.connections(ctx)
		window.observe(time.Now(), connections, err)
	}
	checked, err := u.Check(ctx)
	if err != nil || !watch || !checked.Ready || checked.Release == nil || s.Release == nil || checked.Release.SHA256 != s.Release.SHA256 {
		*window = idleWindow{}
		return checked, err
	}
	connections, probeErr := u.connections(ctx)
	window.afterCheck(time.Now(), connections, probeErr)
	return checked, nil
}

func (u Updater) observeIdle(ctx context.Context, s Status, window *idleWindow) (Status, bool, error) {
	connections, probeErr := u.connections(ctx)
	since := window.observe(time.Now(), connections, probeErr)
	lock, err := u.lock("operation")
	if errors.Is(err, ErrBusy) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	defer lock.Close()
	latest, err := u.Status()
	if err != nil {
		return s, false, err
	}
	if !latest.Ready || !latest.Options.UpdatesEnabled() || latest.Release == nil || s.Release == nil || latest.Release.SHA256 != s.Release.SHA256 || latest.Options != s.Options {
		*window = idleWindow{}
		return latest, false, nil
	}
	if !latest.IdleSince.Equal(since) || latest.IdleUpload != connections.UploadTotal || latest.IdleDownload != connections.DownloadTotal {
		latest.IdleSince = since
		latest.IdleUpload, latest.IdleDownload = connections.UploadTotal, connections.DownloadTotal
		if err := u.save(latest); err != nil {
			return latest, false, err
		}
	}
	due := !since.IsZero() && time.Since(since) >= time.Duration(latest.Options.IdleMinutes)*time.Minute
	return latest, due, nil
}

func (u Updater) automaticFailure(cause error) error {
	lock, err := u.lock("operation")
	if errors.Is(err, ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	s, err := u.Status()
	if err != nil {
		return err
	}
	s.Phase, s.Error, s.RetryAfter = "error", cause.Error(), time.Now().Add(15*time.Minute)
	return u.save(s)
}

func (u Updater) awaitApply(ctx context.Context) error {
	started := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s, err := u.Status()
			if err != nil || s.Phase == "error" {
				return err
			}
			if time.Since(started) < 30*time.Second {
				continue
			}
			lock, err := u.lock("operation")
			if errors.Is(err, ErrBusy) {
				continue
			}
			if err != nil {
				return err
			}
			s, err = u.Status()
			if err == nil {
				s.Phase, s.Error, s.RetryAfter = "error", "The independent updater exited before completing the update.", time.Now().Add(15*time.Minute)
				err = u.save(s)
			}
			lock.Close()
			return err
		}
	}
}
