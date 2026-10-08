// Package testutil keeps socket fixtures inside the project's configured TMPDIR.
package testutil

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"verge-tui/internal/state"
)

func Dir(t *testing.T) string {
	t.Helper()
	path, err := os.MkdirTemp(os.Getenv("TMPDIR"), "vt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(path) })
	return path
}

func CoreServer(t *testing.T, s *state.Store, handler http.Handler) *httptest.Server {
	t.Helper()
	c, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	address := server.Listener.Addr().String()
	server.Listener.Close()
	listener, err := net.Listen("unix", c.ControllerSocket)
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	server.URL = "http://" + address
	t.Cleanup(server.Close)
	return server
}
