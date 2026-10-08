package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/management"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestProfileEditorsPreserveIdentityAndRejectStaleOrInvalidChanges(t *testing.T) {
	s, err := state.Open(testutil.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Add(s, state.Profile{Type: "remote", Name: "Original", URL: "https://example.com/private?token=123", Timeout: 30}, []byte(profile.DefaultConfig))
	if err != nil {
		t.Fatal(err)
	}
	b := New(s, "test")
	ctx := context.Background()
	d, err := b.ReadPage(ctx, "profile-info", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Name, p.Description, p.Timeout = "Edited", "Description", 45
	if err := b.Execute(ctx, Action{Kind: "profile-info-save", ID: p.ID, Value: d.Document.Revision, Profile: p}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "profile-info-save", ID: p.ID, Value: d.Document.Revision, Profile: p}); err == nil {
		t.Fatal("stale metadata accepted")
	}
	c, _ := s.Read()
	got, _ := state.Find(c, p.ID)
	if got.URL != p.URL || got.Name != "Edited" || got.Type != "remote" || got.Timeout != 45 {
		t.Fatal(got)
	}
	d, err = b.ReadPage(ctx, "profile-file", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := Action{Kind: "profile-file-save", ID: p.ID, Value: d.Document.Revision, Data: []byte("proxies: []\nrules: [MATCH,DIRECT]\n")}
	if err := b.Execute(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, a); err == nil {
		t.Fatal("stale file accepted")
	}
	current, _ := b.ReadPage(ctx, "profile-file", p.ID)
	a.Value, a.Data = current.Document.Revision, []byte("not: [valid")
	if err := b.Execute(ctx, a); err == nil {
		t.Fatal("invalid YAML saved")
	}
	path, _ := s.ProfilePath(p.ID)
	data, _ := os.ReadFile(path)
	if string(data) != string(current.Document.Data) {
		t.Fatal("invalid edit overwrote original")
	}
	if _, err := b.ReadPage(ctx, "profile-file", "../settings"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}

func TestSharedPagesEditorsAndLogs(t *testing.T) {
	s, err := state.Open(testutil.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	shortenStore(t, s)
	p, err := profile.Local(s, "Shared", "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.CoreServer(t, s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connections":
			json.NewEncoder(w).Encode(controller.Connections{UploadTotal: 100, Connections: []controller.Connection{{ID: "active", Start: time.Now(), Metadata: controller.Metadata{Host: "example.com", DestinationPort: "443"}}}})
		case "/rules":
			json.NewEncoder(w).Encode(controller.Rules{Rules: []controller.Rule{{Type: "Match", Proxy: "DIRECT"}}})
		case "/providers/rules":
			json.NewEncoder(w).Encode(controller.Rules{Providers: map[string]controller.RuleProvider{"test": {RuleCount: 42}}})
		case "/logs":
			json.NewEncoder(w).Encode(controller.Log{Type: "info", Payload: "core log"})
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			w.WriteHeader(204)
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := New(s, "test")
	var mu sync.RWMutex
	server, err := management.Serve(ctx, s, b.SharedHandlers(&mu))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); server.Close() }()
	remote := NewRemote(s, "test")
	defer remote.Remote.Close()
	for _, kind := range []string{"connections", "rules", "profile-info", "profile-file"} {
		page, err := remote.ReadPage(ctx, kind, p.ID)
		if err != nil {
			t.Fatal(kind, err)
		}
		if kind == "connections" && len(page.Connections.Connections) != 1 {
			t.Fatal(page)
		}
		if kind == "rules" && page.Rules.Providers["test"].RuleCount != 42 {
			t.Fatal(page)
		}
		if kind == "profile-file" {
			if err := remote.Execute(ctx, Action{Kind: "profile-file-save", ID: p.ID, Value: page.Document.Revision, Data: []byte("proxies: []\nrules: []\n")}); err != nil {
				t.Fatal(err)
			}
		}
	}
	streamCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	err = remote.Logs(streamCtx, func(entry controller.Log) error {
		if entry.Payload != "core log" || entry.Time == "" {
			t.Error(entry)
		}
		// A streaming client must not hold the service action lock.
		if err := remote.Execute(ctx, Action{Kind: "connection-close", ID: "active"}); err != nil {
			t.Error(err)
		}
		return errors.New("received")
	})
	if err == nil || err.Error() != "received" {
		t.Fatal(err)
	}
}
