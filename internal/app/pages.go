package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

type ProfileDocument struct {
	Profile  state.Profile
	Data     []byte
	Revision string
}
type PageData struct {
	Connections controller.Connections
	Rules       controller.Rules
	Document    *ProfileDocument
	Releases    *core.ReleasePage
}

func revision(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func (b *Backend) ReadPage(ctx context.Context, kind, id string) (PageData, error) {
	var out PageData
	if b.Remote != nil {
		err := b.Remote.Call(ctx, "GET", "/data/"+url.PathEscape(kind)+"?id="+url.QueryEscape(id), nil, &out)
		return out, err
	}
	if kind == "core-releases" {
		page, err := strconv.Atoi(id)
		if err != nil {
			return out, errors.New("invalid release page")
		}
		releases, err := core.Available(ctx, b.Store, page)
		out.Releases = &releases
		return out, err
	}
	c, err := b.Store.Read()
	if err != nil {
		return out, err
	}
	if kind == "profile-info" || kind == "profile-file" {
		p, ok := state.Find(c, id)
		if !ok {
			return out, errors.New("profile does not exist")
		}
		doc := &ProfileDocument{Profile: p}
		metadata, _ := json.Marshal(p)
		doc.Revision = revision(metadata)
		if kind == "profile-file" {
			path, err := b.Store.ProfilePath(id)
			if err != nil {
				return out, err
			}
			doc.Data, err = readProfileFile(path)
			if err != nil {
				return out, err
			}
			doc.Revision = revision(doc.Data)
		}
		out.Document = doc
		return out, nil
	}
	client := controller.New(c)
	defer client.Close()
	switch kind {
	case "connections":
		out.Connections, err = client.Connections(ctx)
	case "rules":
		out.Rules, err = client.Rules(ctx)
	default:
		err = errors.New("unknown page")
	}
	return out, err
}
func readProfileFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > profile.MaxSize {
		return nil, errors.New("profile must be a regular YAML file no larger than 16 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, profile.MaxSize+1))
	if len(data) > profile.MaxSize {
		return nil, errors.New("profile exceeds 16 MiB")
	}
	return data, err
}
func (b *Backend) Logs(ctx context.Context, emit func(controller.Log) error) error {
	if b.Remote != nil {
		return b.Remote.Logs(ctx, func(data json.RawMessage) error {
			var entry controller.Log
			if err := json.Unmarshal(data, &entry); err != nil {
				return err
			}
			return emit(entry)
		})
	}
	c, err := b.Store.Read()
	if err != nil {
		return err
	}
	client := controller.New(c)
	defer client.Close()
	return client.Logs(ctx, emit)
}
func (b *Backend) pageAction(ctx context.Context, a Action) error {
	c, err := b.Store.Read()
	if err != nil {
		return err
	}
	client := controller.New(c)
	defer client.Close()
	if a.Kind == "connection-close" {
		return client.CloseConnection(ctx, a.ID)
	}
	client.HTTP.Timeout = 5 * time.Minute
	if a.Kind == "provider-update" {
		return client.UpdateProvider(ctx, a.ID)
	}
	rules, err := client.Rules(ctx)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(rules.Providers))
	for name := range rules.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	var failures []error
	for _, name := range names {
		if err := client.UpdateProvider(ctx, name); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(failures...)
}
func (b *Backend) editProfileInfo(a Action) error {
	return b.Store.Update(func(c *state.Settings) error {
		for i, p := range c.Profiles {
			if p.ID != a.ID {
				continue
			}
			data, _ := json.Marshal(p)
			if a.Value != revision(data) {
				return errors.New("profile information changed; reopen Edit Info before saving")
			}
			if a.Profile.Type != "local" && a.Profile.Type != "remote" {
				return errors.New("invalid profile type")
			}
			p.Type = a.Profile.Type
			p.Name, p.Description = strings.TrimSpace(a.Profile.Name), a.Profile.Description
			if p.Name == "" {
				p.Name = p.Type + " file"
			}
			if len(p.Name) > 1024 || len(p.Description) > 8192 {
				return errors.New("profile information is too long")
			}
			if p.Type == "remote" {
				u, err := url.Parse(a.Profile.URL)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
					return errors.New("subscription URL must use HTTP or HTTPS without embedded credentials")
				}
				if len(a.Profile.URL) > 8192 || len(a.Profile.UserAgent) > 8192 || strings.ContainsAny(a.Profile.UserAgent, "\r\n") {
					return errors.New("invalid subscription URL or User Agent")
				}
				if a.Profile.Timeout < 1 || a.Profile.Timeout > 300 {
					return errors.New("HTTP Request Timeout must be between 1 and 300 seconds")
				}
				p.URL, p.UserAgent, p.Timeout = a.Profile.URL, a.Profile.UserAgent, a.Profile.Timeout
			}
			c.Profiles[i] = p
			return nil
		}
		return errors.New("profile does not exist")
	})
}

// Validate before touching the original, serialize with profile selection, and
// restore both the file and live configuration if persistence fails.
func (b *Backend) editProfileFile(ctx context.Context, a Action) error {
	if _, err := profile.Parse(a.Data); err != nil {
		return err
	}
	return b.Store.WithSettingsLock(func(c state.Settings) error {
		if _, ok := state.Find(c, a.ID); !ok {
			return errors.New("profile does not exist")
		}
		path, err := b.Store.ProfilePath(a.ID)
		if err != nil {
			return err
		}
		before, err := readProfileFile(path)
		if err != nil {
			return err
		}
		if revision(before) != a.Value {
			return errors.New("profile file changed; reopen Edit File before saving")
		}
		payload, err := profile.RuntimeData(c, a.Data)
		if err != nil {
			return err
		}
		if c.Core != "" {
			if err := b.Manager.Validate(ctx, c, payload); err != nil {
				return err
			}
		}
		active := c.Current == a.ID && c.Core != ""
		var oldPayload []byte
		var running bool
		client := controller.New(c)
		defer client.Close()
		if active {
			oldPayload, err = profile.RuntimeData(c, before)
			if err != nil {
				return err
			}
			running, err = core.Running(b.Store)
			if err != nil {
				return err
			}
			if running {
				if err := client.Reload(ctx, payload); err != nil {
					return err
				}
			}
		}
		rollback := func(cause error) error {
			var liveErr error
			if running {
				liveErr = client.Reload(context.WithoutCancel(ctx), oldPayload)
			}
			return errors.Join(cause, state.Write(path, before, 0600), liveErr)
		}
		if err := state.Write(path, a.Data, 0600); err != nil {
			return rollback(err)
		}
		if active {
			if err := state.Write(filepath.Join(b.Store.Root, "runtime", "config.yaml"), payload, 0600); err != nil {
				return rollback(err)
			}
		}
		return nil
	})
}
