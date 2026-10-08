package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"verge-tui/internal/controller"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

type Status struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Core    string    `json:"core"`
}
type Manager struct {
	Store *state.Store
	cmd   *exec.Cmd
	done  chan error
	lock  *os.File
}

type boundedLog struct {
	mu        sync.Mutex
	file      *os.File
	remaining int
}

func (w *boundedLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	keep := min(n, w.remaining)
	if keep > 0 {
		if _, err := w.file.Write(p[:keep]); err != nil {
			return 0, err
		}
		w.remaining -= keep
	}
	return n, nil
}

func Running(s *state.Store) (bool, error) {
	lock, err := os.OpenFile(filepath.Join(s.Root, "runtime", "core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}

func ReadStatus(s *state.Store) Status {
	var status Status
	b, err := os.ReadFile(filepath.Join(s.Root, "runtime", "status.json"))
	if err == nil {
		json.Unmarshal(b, &status)
	}
	return status
}
func (m *Manager) Validate(ctx context.Context, c state.Settings, payload []byte) error {
	return m.validate(ctx, c, payload, 45*time.Second)
}

func (m *Manager) validate(ctx context.Context, c state.Settings, payload []byte, timeout time.Duration) error {
	path, err := Path(m.Store, c.Core)
	if err != nil {
		return errors.New("install and select a managed core first")
	}
	f, err := os.CreateTemp(filepath.Join(m.Store.Root, "runtime"), "check-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(payload); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, path, "-t", "-d", filepath.Join(m.Store.Root, "runtime"), "-f", f.Name()).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(b))
		if len(text) > 2000 {
			text = text[len(text)-2000:]
		}
		for _, secret := range []string{c.Secret, c.ProxyPassword} {
			if secret != "" {
				text = strings.ReplaceAll(text, secret, "[redacted]")
			}
		}
		if ctx.Err() != nil {
			if parent.Err() != nil {
				return fmt.Errorf("core validation interrupted: %w\n%s", parent.Err(), text)
			}
			return fmt.Errorf("core validation timed out after %s; required geodata or provider files may be unavailable: %w\n%s", timeout, ctx.Err(), text)
		}
		return fmt.Errorf("core rejected profile: %w\n%s", err, text)
	}
	return nil
}
func (m *Manager) Start(ctx context.Context) error {
	if m.cmd != nil {
		return errors.New("core already owned by this process")
	}
	c, err := m.Store.Read()
	if err != nil {
		return err
	}
	if c.InstallMode == "system" && os.Geteuid() != 0 {
		return errors.New("system mode must be started by the system service as root")
	}
	lock, err := os.OpenFile(filepath.Join(m.Store.Root, "runtime", "core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return errors.New("another process or service owns the core")
	}
	keep := false
	defer func() {
		if !keep {
			lock.Close()
		}
	}()
	if len(c.ControllerSocket) > 107 {
		return errors.New("installation path is too long for the core control socket (maximum 107 bytes)")
	}
	ports := []int{c.MixedPort, c.HTTPPort, c.SOCKSPort}
	if c.ControllerEnabled {
		_, port, err := net.SplitHostPort(c.ControllerAddress)
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(port)
		if err != nil {
			return err
		}
		ports = append(ports, n)
	}
	for _, port := range ports {
		if port == 0 {
			continue
		}
		if err := state.CheckPort(port); err != nil {
			return err
		}
	}
	payload, err := profile.Runtime(m.Store, c, c.Current)
	if err != nil {
		return err
	}
	if err := m.Validate(ctx, c, payload); err != nil {
		return err
	}
	config := filepath.Join(m.Store.Root, "runtime", "config.yaml")
	if err := state.Write(config, payload, 0600); err != nil {
		return err
	}
	path, _ := Path(m.Store, c.Core)
	log, err := os.OpenFile(filepath.Join(m.Store.Root, "runtime", "core.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, "-d", filepath.Join(m.Store.Root, "runtime"), "-f", config)
	output := &boundedLog{file: log, remaining: 2 << 20}
	cmd.Stdout = output
	cmd.Stderr = output
	// A supervisor crash must not leave an unmanaged proxy running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		log.Close()
		return err
	}
	m.cmd = cmd
	m.done = make(chan error, 1)
	m.lock = lock
	keep = true
	done := m.done
	go func() { done <- cmd.Wait(); close(done); log.Close() }()
	client := controller.New(c)
	defer client.Close()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case err := <-m.done:
			m.release()
			return fmt.Errorf("core exited during startup: %v (see runtime/core.log)", err)
		case <-ctx.Done():
			m.Stop()
			return ctx.Err()
		case <-deadline.C:
			m.Stop()
			return errors.New("core startup timed out; see runtime/core.log")
		case <-ticker.C:
			var v struct{ Version string }
			probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			err := client.Request(probe, "GET", "/version", nil, &v)
			cancel()
			if err == nil {
				b, _ := json.Marshal(Status{PID: cmd.Process.Pid, Started: time.Now(), Core: c.Core})
				if err := state.Write(filepath.Join(m.Store.Root, "runtime", "status.json"), b, 0600); err != nil {
					m.Stop()
					return err
				}
				return nil
			}
		}
	}
}
func (m *Manager) release() {
	os.Remove(filepath.Join(m.Store.Root, "runtime", "status.json"))
	if m.lock != nil {
		m.lock.Close()
	}
	m.lock = nil
	m.cmd = nil
}
func (m *Manager) Owned() bool        { return m.cmd != nil }
func (m *Manager) Done() <-chan error { return m.done }
func (m *Manager) Stop() error {
	if m.cmd == nil {
		return nil
	}
	err := m.cmd.Process.Signal(unix.SIGTERM)
	select {
	case <-m.done:
	case <-time.After(5 * time.Second):
		m.cmd.Process.Kill()
		<-m.done
	}
	m.release()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
func (m *Manager) Apply(ctx context.Context, mutate func(*state.Settings) error) error {
	var before state.Settings
	restart := false
	err := m.Store.Update(func(c *state.Settings) error {
		before = *c
		previous := *c
		if err := mutate(c); err != nil {
			return err
		}
		payload, err := profile.Runtime(m.Store, *c, c.Current)
		if err != nil {
			return err
		}
		if c.Core == "" {
			return nil
		}
		if err := m.Validate(ctx, *c, payload); err != nil {
			return err
		}
		// Send the reload to the existing controller, even if settings change.
		client := controller.New(previous)
		defer client.Close()
		running, err := Running(m.Store)
		if err != nil {
			return err
		}
		restart = running && (previous.ControllerEnabled != c.ControllerEnabled || previous.ControllerAddress != c.ControllerAddress || previous.Secret != c.Secret)

		if running && !restart {
			if err := client.Reload(ctx, payload); err != nil {
				return err
			}
		}
		return state.Write(filepath.Join(m.Store.Root, "runtime", "config.yaml"), payload, 0600)
	})
	if err != nil || !restart {
		return err
	}
	if !m.Owned() {
		client := controller.New(before)
		defer client.Close()
		if err := client.Request(ctx, "POST", "/restart", nil, nil); err != nil {
			return err
		}
		current, err := m.Store.Read()
		if err != nil {
			return err
		}
		return waitController(ctx, before, current)
	}
	if err := m.Stop(); err != nil {
		return err
	}
	if err := m.Start(ctx); err != nil {
		restoreErr := m.Store.Update(func(c *state.Settings) error { *c = before; return nil })
		return errors.Join(err, restoreErr, errors.New("previous settings restored; core stopped"))
	}
	return nil
}
func (m *Manager) Use(ctx context.Context, id string) error {
	path, err := Path(m.Store, id)
	if err != nil {
		return err
	}
	if _, err := Version(ctx, path); err != nil {
		return err
	}
	c, err := m.Store.Read()
	if err != nil {
		return err
	}
	previous := c.Core
	if previous == id {
		return nil
	}
	if !m.Owned() {
		lock, err := os.OpenFile(filepath.Join(m.Store.Root, "runtime", "core.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer lock.Close()
		if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			return errors.New("stop the service before selecting another core version")
		}
		c.Core = id
		payload, err := profile.Runtime(m.Store, c, c.Current)
		if err != nil {
			return err
		}
		if err := m.Validate(ctx, c, payload); err != nil {
			return err
		}
		return m.Store.Update(func(c *state.Settings) error { c.Core = id; return nil })
	}
	c.Core = id
	payload, err := profile.Runtime(m.Store, c, c.Current)
	if err != nil {
		return err
	}
	if err := m.Validate(ctx, c, payload); err != nil {
		return err
	}
	if err := m.Stop(); err != nil {
		return err
	}
	if err := m.Store.Update(func(c *state.Settings) error { c.Core = id; return nil }); err != nil {
		return err
	}
	if err := m.Start(ctx); err != nil {
		// Restore the selection without launching another binary after a failure.
		restoreErr := m.Store.Update(func(c *state.Settings) error { c.Core = previous; return nil })
		return errors.Join(err, restoreErr, errors.New("previous core selection restored; core stopped"))
	}
	return nil
}

// The Linux core restart API uses exec, keeping the owning supervisor's PID
// and process handle intact. Confirm the new TCP policy and private IPC.
func waitController(ctx context.Context, before, current state.Settings) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	internal := controller.New(current)
	defer internal.Close()
	external := controller.New(state.Settings{APIPort: current.APIPort, Secret: current.Secret})
	defer external.Close()
	localAddress := func(address string) string {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(current.APIPort))
		}
		if host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		if host == "::" {
			host = "::1"
		}
		return net.JoinHostPort(host, port)
	}
	external.Base = "http://" + localAddress(current.ControllerAddress)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready := false
		if current.ControllerEnabled {
			probe, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
			ready = external.Request(probe, "GET", "/version", nil, nil) == nil
			cancel()
		} else {
			conn, err := net.DialTimeout("tcp", localAddress(before.ControllerAddress), 100*time.Millisecond)
			if err != nil {
				ready = true
			} else {
				conn.Close()
			}
		}
		if ready {
			probe, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
			err := internal.Request(probe, "GET", "/version", nil, nil)
			cancel()
			if err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("core controller restart did not become ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
