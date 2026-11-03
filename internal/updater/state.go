// Package updater checks GitHub releases and prepares verified offline bundles.
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	"verge-tui/internal/state"
)

type Options struct {
	CheckMinutes int `json:"check_minutes"`
	IdleMinutes  int `json:"idle_minutes"`
}

func (o Options) Validate() error {
	if o.CheckMinutes < 0 || o.CheckMinutes > 10080 {
		return errors.New("check interval must be Manual or between 1 and 10080 minutes")
	}
	if o.IdleMinutes < 0 || o.IdleMinutes > 1440 {
		return errors.New("automatic update must be Manual or between 1 and 1440 idle minutes")
	}
	return nil
}

type Status struct {
	Options        Options   `json:"options"`
	CurrentVersion string    `json:"current_version"`
	Release        *Release  `json:"release,omitempty"`
	LastChecked    time.Time `json:"last_checked,omitempty"`
	LastAttempt    time.Time `json:"last_attempt,omitempty"`
	RetryAfter     time.Time `json:"retry_after,omitempty"`
	Phase          string    `json:"phase"`
	Downloaded     int64     `json:"downloaded"`
	Ready          bool      `json:"ready"`
	IdleSince      time.Time `json:"idle_since,omitempty"`
	IdleUpload     uint64    `json:"idle_upload,omitempty"`
	IdleDownload   uint64    `json:"idle_download,omitempty"`
	Error          string    `json:"error,omitempty"`
	Supported      bool      `json:"supported"`
}

func (s Status) Available() bool {
	return s.Release != nil && Newer(s.Release.Version, s.CurrentVersion)
}

type Request struct {
	Version   string
	Automatic bool
	Configure *Options
}

func (r *Request) Error() string { return "application update requested" }

type Updater struct {
	Store   *state.Store
	Version string
}

var ErrBusy = errors.New("another update operation is in progress")

func (u Updater) directory() string { return filepath.Join(u.Store.Root, "updater") }

func readJSON(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("invalid update state: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return state.Write(path, append(data, '\n'), 0600)
}

func (u Updater) Options() (Options, error) {
	var options Options
	err := readJSON(filepath.Join(u.directory(), "options.json"), &options)
	if os.IsNotExist(err) {
		return options, nil
	}
	if err != nil {
		return options, err
	}
	return options, options.Validate()
}

func (u Updater) Configure(options Options) error {
	if err := options.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(u.directory(), 0700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(u.directory(), "options.json"), options)
}

func (u Updater) Status() (Status, error) {
	var s Status
	err := readJSON(filepath.Join(u.directory(), "status.json"), &s)
	if err != nil && !os.IsNotExist(err) {
		return s, err
	}
	s.CurrentVersion = u.Version
	s.Options, err = u.Options()
	if err != nil {
		return s, err
	}
	s.Supported = installed(u.Store.Root)
	if !s.Available() {
		s.Ready, s.IdleSince = false, time.Time{}
		if s.Phase == "applying" || s.Phase == "ready" {
			s.Phase = "latest"
		}
	}
	return s, nil
}

func (u Updater) save(s Status) error {
	return writeJSON(filepath.Join(u.directory(), "status.json"), s)
}

func (u Updater) lock(name string) (*os.File, error) {
	if err := os.MkdirAll(u.directory(), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(u.directory(), name+".lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return f, nil
}
