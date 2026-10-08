package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/core"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestRealCorePagesLogsAndProfileEditing(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY")
	}
	s, err := state.Open(testutil.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	b := New(s, "test")
	ctx := context.Background()
	if _, err := core.Import(ctx, s, "test", binary); err != nil {
		t.Fatal(err)
	}
	p, err := profile.Add(s, state.Profile{Name: "Edited", Type: "local"}, []byte("proxies: []\nrules:\n  - DOMAIN,example.com,DIRECT\n  - MATCH,DIRECT\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *state.Settings) error { c.Current = p.ID; c.Engine.LogLevel = "info"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Use(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if err := b.Manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	doc, err := b.ReadPage(ctx, "profile-file", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated := []byte("proxies: []\nrules:\n  - DOMAIN,edited.example,DIRECT\n  - MATCH,DIRECT\n")
	if err := b.Execute(ctx, Action{Kind: "profile-file-save", ID: p.ID, Value: doc.Document.Revision, Data: updated}); err != nil {
		t.Fatal(err)
	}
	rules, err := b.ReadPage(ctx, "rules", "")
	if err != nil || len(rules.Rules.Rules) != 2 || rules.Rules.Rules[0].Payload != "edited.example" {
		t.Fatal("edited configuration was not reloaded", rules, err)
	}
	doc, err = b.ReadPage(ctx, "profile-file", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "profile-file-save", ID: p.ID, Value: doc.Document.Revision, Data: []byte("proxies: []\nrules: [INVALID-RULE]\n")}); err == nil {
		t.Fatal("core-invalid rules accepted")
	}
	doc, err = b.ReadPage(ctx, "profile-file", p.ID)
	if err != nil || string(doc.Document.Data) != string(updated) {
		t.Fatal("rejected edit replaced original file", err)
	}
	rules, err = b.ReadPage(ctx, "rules", "")
	if err != nil || rules.Rules.Rules[0].Payload != "edited.example" {
		t.Fatal("rejected edit changed live rules", err)
	}
	c, _ := s.Read()
	logCtx, stopLog := context.WithTimeout(ctx, 5*time.Second)
	defer stopLog()
	logsDone := make(chan error, 1)
	go func() {
		logsDone <- b.Logs(logCtx, func(entry controller.Log) error {
			if strings.Contains(entry.Payload, "127.0.0.1") {
				return errors.New("received real log")
			}
			return nil
		})
	}()
	// mihomo starts sending headers with its first entry. Generate a real proxy
	// connection repeatedly until the subscriber is registered, without Internet.
	entered := make(chan struct{}, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer origin.Close()
	proxy, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(c.MixedPort))
	proxy.User = url.UserPassword(c.ProxyUser, c.ProxyPassword)
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	requestCtx, stopRequest := context.WithCancel(ctx)
	defer stopRequest()
	request, _ := http.NewRequestWithContext(requestCtx, "GET", origin.URL, nil)
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("proxy request did not arrive")
	}
	connections, err := b.ReadPage(ctx, "connections", "")
	if err != nil || len(connections.Connections.Connections) == 0 {
		t.Fatal("live connections missing", err)
	}
	id := connections.Connections.Connections[0].ID
	if err := b.Execute(ctx, Action{Kind: "connection-close", ID: id}); err != nil {
		t.Fatal(err)
	}
	connections, err = b.ReadPage(ctx, "connections", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range connections.Connections.Connections {
		if conn.ID == id {
			t.Fatal("connection was not closed")
		}
	}
	stopRequest()
	// Local requests also emit logs when the subscription was registered later.
	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.MixedPort), time.Second)
		if err == nil {
			fmt.Fprint(conn, "GET http://127.0.0.1:1/ HTTP/1.1\r\nHost: 127.0.0.1\r\nProxy-Authorization: Basic invalid\r\n\r\n")
			conn.Close()
		}
		select {
		case err := <-logsDone:
			if err == nil || err.Error() != "received real log" {
				t.Fatal(err)
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	select {
	case err := <-logsDone:
		if err == nil || err.Error() != "received real log" {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no real logs received")
	}
}
