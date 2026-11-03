package updater

import (
	"context"
	"errors"
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
	if options.IdleMinutes == 0 || options != s.Options || s.IdleSince.IsZero() || time.Since(s.IdleSince) < time.Duration(options.IdleMinutes)*time.Minute {
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
	if !installed(u.Store.Root) {
		return nil
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
		if s.Options != previous {
			window = idleWindow{}
			previous = s.Options
		}
		if checkDue(s, time.Now()) {
			s, err = u.Check(ctx)
			window = idleWindow{}
		}
		if err == nil && s.Available() && s.Options.IdleMinutes > 0 && !time.Now().Before(s.RetryAfter) {
			if !s.Ready {
				s, err = u.Prepare(ctx)
				window = idleWindow{}
			}
			if err == nil && s.Ready {
				connections, probeErr := u.connections(ctx)
				since := window.observe(time.Now(), connections, probeErr)
				// Operation owners also write status; never overwrite progress or
				// a newly checked release with an older idle observation.
				lock, lockErr := u.lock("operation")
				if lockErr == nil {
					latest, readErr := u.Status()
					eligible := false
					if readErr == nil && latest.Ready && latest.Release != nil && latest.Release.SHA256 == s.Release.SHA256 && latest.Options == s.Options {
						if latest.IdleSince != since {
							latest.IdleSince = since
							latest.IdleUpload, latest.IdleDownload = connections.UploadTotal, connections.DownloadTotal
							readErr = u.save(latest)
						}
						s = latest
						eligible = true
					} else {
						window = idleWindow{}
					}
					lock.Close()
					if readErr != nil {
						return readErr
					}
					if eligible && !since.IsZero() && time.Since(since) >= time.Duration(s.Options.IdleMinutes)*time.Minute && s.Options.IdleMinutes > 0 {
						if err := apply(ctx, &Request{Version: s.Release.Version, Automatic: true}); err != nil {
							s.Phase, s.Error, s.RetryAfter = "error", err.Error(), time.Now().Add(15*time.Minute)
							if err := u.save(s); err != nil {
								return err
							}
							window = idleWindow{}
						} else {
							<-ctx.Done()
							return nil
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
