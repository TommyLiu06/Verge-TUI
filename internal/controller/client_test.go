package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEncodedNodeNamesAndAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private" {
			t.Error("missing API secret")
		}
		if r.Method == "PUT" {
			if r.URL.EscapedPath() != "/proxies/Group%2F%E8%8A%82%E7%82%B9%3F" {
				t.Errorf("unsafe path %s", r.URL.EscapedPath())
			}
			w.WriteHeader(204)
			return
		}
		if r.URL.Query().Get("url") != "https://example.com/check?a=1&b=2" {
			t.Error("test URL changed")
		}
		w.Write([]byte(`{"delay":123}`))
	}))
	defer server.Close()
	c := &Client{Base: server.URL, Secret: "private", HTTP: server.Client()}
	if err := c.Select(context.Background(), "Group/节点?", "Node A"); err != nil {
		t.Fatal(err)
	}
	delay, err := c.Delay(context.Background(), "Node A", "https://example.com/check?a=1&b=2")
	if err != nil || delay != 123 {
		t.Fatalf("delay %d: %v", delay, err)
	}
}
