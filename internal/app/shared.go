package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/desktop"
	"verge-tui/internal/management"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

func NewRemote(s *state.Store, version string) *Backend {
	b := New(s, version)
	b.Remote = management.NewClient(s)
	b.Desktop.Store = desktop.UserStore(s)
	b.Desktop.PreferencesRoot = b.preferencesRoot()
	return b
}

func sharedAction(a Action) error {
	switch a.Kind {
	case "connection-close", "provider-update", "providers-update", "profile-info-save", "profile-file-save", "refresh", "mode", "tun", "core-install", "core-use", "profile-import", "profile-new", "profile-select", "profile-update", "profile-delete", "home-group", "node-select", "delay", "settings-save", "geo-update":
		if a.File != "" {
			return errors.New("local profile files must be uploaded by the calling user")
		}
		return nil
	default:
		return errors.New("service management requires root; use systemctl as an administrator")
	}
}

func (b *Backend) executeRemote(ctx context.Context, a Action) error {
	if a.Kind == "core-import" {
		return b.Remote.ImportCore(ctx, a.ID, a.File)
	}
	if a.Kind == "settings-save" && a.ID == "ports" {
		v, err := b.Load(ctx)
		if err != nil {
			return err
		}
		if v.Desktop.Owned {
			return errors.New("disable System Proxy before changing ports")
		}
	}
	if a.Kind == "system-proxy" {
		v, err := b.Load(ctx)
		if err != nil {
			return err
		}
		observed := b.Desktop.Read(ctx, v.Settings)
		if observed.Owned || !observed.Enabled && fileExists(filepath.Join(b.Desktop.Store.Root, "gnome-proxy-backup.ini")) {
			return b.Desktop.Disable(ctx, v.Settings)
		}
		if !observed.Available {
			return errors.New(observed.Detail)
		}
		if !v.Online {
			return errors.New("start the system service before enabling System Proxy")
		}
		if err := desktop.PrepareUser(b.Desktop.Store); err != nil {
			return err
		}
		if err := desktop.SaveSession(b.Desktop.Store); err != nil {
			return err
		}
		return b.Desktop.Enable(ctx, v.Settings)
	}
	if a.Kind == "profile-new" && a.File != "" {
		f, err := os.Open(a.File)
		if err != nil {
			return err
		}
		defer f.Close()
		a.Data, err = io.ReadAll(io.LimitReader(f, profile.MaxSize+1))
		if err != nil {
			return err
		}
		if _, err := profile.Parse(a.Data); err != nil {
			return err
		}
		a.File = ""
	}
	if err := sharedAction(a); err != nil {
		return err
	}
	if a.Kind == "delay" {
		return b.TestDelay(ctx, a, nil)
	}
	return b.Remote.Call(ctx, "POST", "/action", a, nil)
}

// PublicView deliberately omits the controller token, private proxy credentials
// and subscription URL tokens. Mutable slices/maps are copied before editing.
func PublicView(v View) View {
	v.Owned = false
	v.Preferences = state.DefaultPreferences()
	v.Settings.Secret, v.Settings.ProxyUser, v.Settings.ProxyPassword = "", "", ""
	v.Settings.Profiles = append([]state.Profile(nil), v.Settings.Profiles...)
	for i := range v.Settings.Profiles {
		p := &v.Settings.Profiles[i]
		if u, err := url.Parse(p.URL); err == nil {
			p.URL = (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
		} else {
			p.URL = ""
		}
		p.Home, p.UserAgent = "", ""
	}
	v.Cores = append([]core.Installed(nil), v.Cores...)
	for i := range v.Cores {
		v.Cores[i].Source = ""
	}
	return v
}

// The supervisor shares this lock when inspecting the current core process.
// Core replacement can change Done; it must not be mistaken for a crash.
func (b *Backend) SharedHandlers(mu *sync.RWMutex) management.Handlers {
	decode := func(data json.RawMessage) (Action, error) {
		var a Action
		if err := json.Unmarshal(data, &a); err != nil {
			return a, err
		}
		return a, sharedAction(a)
	}
	return management.Handlers{
		ReadData: func(ctx context.Context, kind, id string) (any, error) {
			if kind == "core-releases" || kind == "app-update" {
				return b.ReadPage(ctx, kind, id)
			}
			mu.RLock()
			defer mu.RUnlock()
			return b.ReadPage(ctx, kind, id)
		},
		Logs: func(ctx context.Context, emit func(any) error) error {
			return b.Logs(ctx, func(entry controller.Log) error { return emit(entry) })
		},
		Inspect: func(ctx context.Context, kind string) (any, error) {
			mu.RLock()
			defer mu.RUnlock()
			return b.Inspect(ctx, kind)
		},
		CoreImport: func(ctx context.Context, id, path string) error {
			mu.Lock()
			defer mu.Unlock()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if _, err := core.Import(ctx, b.Store, id, path); err != nil {
				return err
			}
			c, err := b.Store.Read()
			if err != nil {
				return err
			}
			if c.Core == "" {
				return b.Manager.Use(ctx, id)
			}
			return nil
		},
		View: func(ctx context.Context) (any, error) {
			mu.RLock()
			defer mu.RUnlock()
			v, err := b.Load(ctx)
			return PublicView(v), err
		},
		Action: func(ctx context.Context, data json.RawMessage) error {
			a, err := decode(data)
			if err != nil {
				return err
			}
			if a.Kind == "delay" {
				return errors.New("use the streaming delay endpoint")
			}
			mu.Lock()
			defer mu.Unlock()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return b.Execute(ctx, a)
		},
		Delay: func(ctx context.Context, data json.RawMessage, progress func(any)) error {
			a, err := decode(data)
			if err != nil {
				return err
			}
			if a.Kind != "delay" {
				return errors.New("expected delay action")
			}
			// TestDelay uses a settings snapshot and its own HTTP client; holding
			// the supervisor lock for network probes would block all mutations.
			return b.TestDelay(ctx, a, func(update DelayResult) { progress(update) })
		},
	}
}
