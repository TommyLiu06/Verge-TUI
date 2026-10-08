// Package management carries application operations over a local Unix socket.
// The root service owns the core and private data; clients never need its token.
package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"mihomo-tui/internal/state"
)

const MaxRequest = 24 << 20

func SocketPath(s *state.Store) string { return filepath.Join(state.SharedPath(s), "api.sock") }

type Client struct{ HTTP *http.Client }

func NewClient(s *state.Store) *Client {
	path := SocketPath(s)
	transport := &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", path)
	}}
	return &Client{HTTP: &http.Client{Transport: transport}}
}

func (c *Client) Close() { c.HTTP.CloseIdleConnections() }
func (c *Client) request(ctx context.Context, method, path string, input any) (*http.Response, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("system service is unavailable; ask an administrator to start mihomo-tui.service: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("system service: %s", bytes.TrimSpace(data))
	}
	return resp, nil
}

func (c *Client) Call(ctx context.Context, method, path string, input, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	resp, err := c.request(ctx, method, path, input)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if output == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output)
}

type streamMessage struct {
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
	Complete bool            `json:"complete,omitempty"`
}

func (c *Client) Delay(ctx context.Context, input any, progress func(json.RawMessage) error) error {
	resp, err := c.request(ctx, http.MethodPost, "/delay", input)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	for {
		var message streamMessage
		if err := decoder.Decode(&message); err != nil {
			return err
		}
		if message.Complete {
			if message.Error != "" {
				return errors.New(message.Error)
			}
			return nil
		}
		if progress != nil {
			if err := progress(message.Result); err != nil {
				return err
			}
		}
	}
}

type Handlers struct {
	View       func(context.Context) (any, error)
	Action     func(context.Context, json.RawMessage) error
	Delay      func(context.Context, json.RawMessage, func(any)) error
	CoreImport func(context.Context, string, string) error
}

type Server struct {
	HTTP *http.Server
	Done <-chan error
}

func Serve(ctx context.Context, s *state.Store, handlers Handlers) (*Server, error) {
	if err := state.PrepareShared(s); err != nil {
		return nil, err
	}
	path := SocketPath(s)
	if len(path) > 107 {
		return nil, errors.New("installation path is too long for the system management socket (maximum 107 bytes)")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("management socket path is not a socket")
		}
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil, errors.New("another system management server is running")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0666); err != nil {
		listener.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /view", func(w http.ResponseWriter, r *http.Request) {
		view, err := handlers.View(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
	})
	readAction := func(w http.ResponseWriter, r *http.Request) (json.RawMessage, error) {
		var action json.RawMessage
		err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequest)).Decode(&action)
		return action, err
	}
	mux.HandleFunc("POST /action", func(w http.ResponseWriter, r *http.Request) {
		action, err := readAction(w, r)
		if err == nil {
			err = handlers.Action(r.Context(), action)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /core", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if !state.ValidID(id) || handlers.CoreImport == nil {
			http.Error(w, "invalid core import", http.StatusBadRequest)
			return
		}
		f, err := os.CreateTemp(filepath.Join(s.Root, "runtime"), ".incoming-core-*")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if err = f.Chmod(0700); err == nil {
			_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, 128<<20))
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = handlers.CoreImport(r.Context(), id, f.Name())
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	// One batch at a time keeps the service-wide delay concurrency bounded.
	delaySlot := make(chan struct{}, 1)
	mux.HandleFunc("POST /delay", func(w http.ResponseWriter, r *http.Request) {
		action, err := readAction(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case delaySlot <- struct{}{}:
			defer func() { <-delaySlot }()
		case <-r.Context().Done():
			return
		}
		if r.Context().Err() != nil {
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		encoder := json.NewEncoder(w)
		flusher := w.(http.Flusher)
		err = handlers.Delay(r.Context(), action, func(update any) {
			data, _ := json.Marshal(update)
			encoder.Encode(streamMessage{Result: data})
			flusher.Flush()
		})
		message := streamMessage{Complete: true}
		if err != nil {
			message.Error = err.Error()
		}
		encoder.Encode(message)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
		close(done)
	}()
	return &Server{HTTP: server, Done: done}, nil
}

func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.HTTP.Shutdown(ctx); err != nil {
		return errors.Join(err, s.HTTP.Close())
	}
	return nil
}

func (c *Client) ImportCore(ctx context.Context, id, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return errors.New("core binary must be a regular file no larger than 128 MiB")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost/core?id="+url.QueryEscape(id), f)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("system service: %s", bytes.TrimSpace(data))
	}
	return nil
}
