package updater

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"verge-tui/internal/installation"
)

func installed(root string) bool {
	if filepath.Base(root) != "data" {
		return false
	}
	summary, err := installation.Discover(filepath.Dir(root))
	return err == nil && summary.State == "installed"
}

func (u Updater) Apply(ctx context.Context, expected string, automatic bool, output io.Writer) (err error) {
	lock, err := u.lock("operation")
	if err != nil {
		return err
	}
	defer lock.Close()
	s, err := u.Status()
	if err != nil {
		return err
	}
	if !s.Supported || !s.Ready || !s.Available() || s.Release.Version != expected {
		return errors.New("the requested verified update is no longer ready; check for updates again")
	}
	if automatic {
		if err := u.AllowAutomatic(ctx, s); err != nil {
			return err
		}
	}
	prefix := filepath.Dir(u.Store.Root)
	if _, err := installation.Inspect(prefix); err != nil {
		return err
	}
	summary, err := installation.Discover(prefix)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(u.directory(), ".apply-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	s.Phase, s.Error = "verifying", ""
	if err := u.save(s); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.Phase, s.Error = "error", err.Error()
			s.RetryAfter = time.Now().Add(15 * time.Minute)
		}
		err = errors.Join(err, u.save(s))
	}()
	// Re-extract the hashed archive; never execute a previously extracted tree
	// which could have changed between downloading and privilege elevation.
	bundle, err := unpack(ctx, filepath.Join(u.directory(), "prepared.tar.gz"), stage, *s.Release)
	if err != nil {
		return err
	}
	binary := filepath.Join(bundle, "bin", "verge-tui")
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	data, err := exec.CommandContext(probe, binary, "version").Output()
	cancel()
	if err != nil || strings.TrimSpace(string(data)) != expected {
		return fmt.Errorf("verified bundle executable has an unexpected version: %w", errors.Join(err, errors.New("version mismatch")))
	}
	if automatic {
		// Validation can take seconds on slow disks. Recheck immediately before
		// stopping the service, including changes to the saved policy.
		if err := u.AllowAutomatic(ctx, s); err != nil {
			return err
		}
	}
	s.Phase = "applying"
	if err := u.save(s); err != nil {
		return err
	}
	fmt.Fprintf(output, "Updating Verge-TUI to v%s...\n", expected)
	command := exec.CommandContext(ctx, binary, "installation", "update", "--prefix", prefix)
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return err
	}
	if err := installation.Record(prefix, expected, summary.CommandLink); err != nil {
		return err
	}
	s.CurrentVersion, s.Phase, s.Ready, s.IdleSince = expected, "latest", false, time.Time{}
	if err := os.Remove(filepath.Join(u.directory(), "prepared.tar.gz")); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Fprintf(output, "Updated to v%s.\n", expected)
	return nil
}

// A transient unit runs outside the proxy service's sandbox and cgroup. Stopping
// that service must not kill its updater or make the installation read-only.
func (u Updater) Launch(ctx context.Context, request *Request) error {
	if _, err := installation.Inspect(filepath.Dir(u.Store.Root)); err != nil {
		return err
	}
	c, err := u.Store.Read()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("verge-tui-update-%x", sha256.Sum256([]byte(u.Store.Root)))[:33]
	args := []string{"--quiet", "--collect", "--unit=" + name, "--service-type=exec"}
	if c.InstallMode == "user" {
		args = append(args, "--user")
	}
	args = append(args, "--", filepath.Join(filepath.Dir(u.Store.Root), "verge-tui"), "--data-dir", u.Store.Root, "update", "apply", "--version", request.Version, "--automatic")
	data, err := exec.CommandContext(ctx, "systemd-run", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not start the independent updater: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return nil
}
