// Package installation updates application files without reinitializing proxy data.
package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"mihomo-tui/internal/controller"
	"mihomo-tui/internal/core"
	"mihomo-tui/internal/geodata"
	"mihomo-tui/internal/shellenv"
	"mihomo-tui/internal/state"
)

const systemUnit = "/etc/systemd/system/mihomo-tui.service"

type Existing struct {
	Prefix, Mode, Unit, RC, Core string
}

// Inspect reads the formal installation marker without changing application data.
func Inspect(prefix string) (Existing, error) { return inspect(prefix, systemUnit, os.Geteuid()) }

func realPath(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() != dir || (!dir && !info.Mode().IsRegular()) {
		return fmt.Errorf("installation path must be a real %s: %s", map[bool]string{true: "directory", false: "file"}[dir], path)
	}
	return nil
}

func inspect(prefix, systemPath string, uid int) (Existing, error) {
	x := Existing{}
	if !filepath.IsAbs(prefix) || filepath.Clean(prefix) == "/" || strings.ContainsAny(prefix, "\r\n\x00") {
		return x, errors.New("installation prefix must be an absolute single-line directory other than /")
	}
	if err := realPath(prefix, true); err != nil {
		return x, err
	}
	resolved, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		return x, err
	}
	x.Prefix = resolved
	for _, dir := range []string{"data", "data/cores", "data/profiles", "data/runtime"} {
		if err := realPath(filepath.Join(resolved, dir), true); err != nil {
			return x, fmt.Errorf("destination is not a complete installation: %w", err)
		}
	}
	for _, file := range []string{"mihomo-tui", "mihomo-tui.service", "data/settings.json"} {
		if err := realPath(filepath.Join(resolved, file), false); err != nil {
			return x, fmt.Errorf("destination is not a complete installation: %w", err)
		}
	}
	s := &state.Store{Root: filepath.Join(resolved, "data")}
	c, err := s.Read()
	if err != nil {
		return x, err
	}
	x.Mode, x.Core = c.InstallMode, c.Core
	if x.Mode != "system" && x.Mode != "user" {
		return x, errors.New("invalid installation mode")
	}
	if x.Mode == "system" && uid != 0 {
		return x, errors.New("system mode update requires root")
	}
	marker := filepath.Join(resolved, ".mihomo-tui-install")
	if _, err := os.Lstat(marker); err == nil {
		if err := realPath(marker, false); err != nil {
			return x, err
		}
		data, err := os.ReadFile(marker)
		if err != nil {
			return x, err
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if len(lines) != 4 || lines[0] != "mihomo-tui-install-v1" || lines[1] != x.Mode || lines[3] != resolved {
			return x, errors.New("invalid or moved installation marker")
		}
		x.Unit = lines[2]
	} else if os.IsNotExist(err) {
		return x, errors.New("no mihomo-tui installation marker")
	} else {
		return x, err
	}
	if x.Mode == "system" && x.Unit != systemPath {
		return x, errors.New("system installation must have its original system service")
	}
	if x.Unit != "" {
		if !filepath.IsAbs(x.Unit) || strings.ContainsAny(x.Unit, "\r\n\x00") || (x.Mode == "user" && !strings.HasSuffix(x.Unit, "/systemd/user/mihomo-tui.service")) {
			return x, errors.New("unexpected recorded service path")
		}
		if err := realPath(x.Unit, false); err != nil {
			return x, err
		}
		installed, err := os.ReadFile(filepath.Join(resolved, "mihomo-tui.service"))
		if err != nil {
			return x, err
		}
		actual, err := os.ReadFile(x.Unit)
		if err != nil {
			return x, err
		}
		if !bytes.Equal(installed, actual) {
			return x, errors.New("service unit differs from this installation; it will not be changed")
		}
	}
	meta := filepath.Join(s.Root, "shell-integration.json")
	if data, err := os.ReadFile(meta); err == nil {
		if err := realPath(meta, false); err != nil {
			return x, err
		}
		var reg struct {
			RC string `json:"rc"`
		}
		if err := json.Unmarshal(data, &reg); err != nil {
			return x, err
		}
		if !filepath.IsAbs(reg.RC) || strings.ContainsAny(reg.RC, "\r\n\x00") {
			return x, errors.New("invalid recorded shell rc path")
		}
		x.RC = reg.RC
	} else if !os.IsNotExist(err) {
		return x, err
	}
	return x, nil
}

type serviceRunner func(context.Context, string, ...string) (string, error)

func systemctl(ctx context.Context, mode string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if mode == "user" {
		args = append([]string{"--user"}, args...)
	}
	b, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(b)), fmt.Errorf("systemctl %s failed: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Update is called by the checksum-verified bundle only after confirmation.
// Existing data, core binaries and service configuration are never replaced.
func Update(ctx context.Context, bundle, prefix, rc string) error {
	return update(ctx, bundle, prefix, rc, systemUnit, os.Geteuid(), systemctl, waitReady, bundledData)
}

type replaced struct{ target, backup string }
type readiness func(context.Context, *state.Store) error
type dataStager func(context.Context, string, string, string) ([]string, error)

func bundledData(ctx context.Context, bundle, stage, root string) ([]string, error) {
	return geodata.Stage(ctx, filepath.Join(bundle, "geodata"), stage, root, filepath.Join(bundle, "cores", "mihomo"))
}

func update(ctx context.Context, bundle, prefix, rc, systemPath string, uid int, run serviceRunner, ready readiness, stageData dataStager) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	x, err := inspect(prefix, systemPath, uid)
	if err != nil {
		return err
	}
	s := &state.Store{Root: filepath.Join(x.Prefix, "data")}
	updateLock, err := os.OpenFile(filepath.Join(x.Prefix, ".update.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer updateLock.Close()
	if err := unix.Flock(int(updateLock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("another installer is updating this installation")
	}
	if rc != "" && x.RC != "" {
		resolved, err := filepath.EvalSymlinks(rc)
		if err == nil {
			rc = resolved
		}
		if rc != x.RC {
			return errors.New("update must keep the registered shell rc; use shell remove before changing it")
		}
	}
	if rc != "" && (!filepath.IsAbs(rc) || strings.ContainsAny(rc, "\r\n\x00")) {
		return errors.New("shell rc must be an absolute single-line path")
	}
	// Stage all new files before stopping the service. The transaction directory
	// is on the installation filesystem, making executable replacement atomic.
	stage, err := os.MkdirTemp(x.Prefix, ".update-")
	if err != nil {
		return err
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			os.RemoveAll(stage)
		}
	}()
	files := []string{"mihomo-tui", "uninstall.sh", "README.md", "LICENSE", "NOTICE.md", "DEPENDENCIES.txt", "docs", "licenses", ".mihomo-tui-install"}
	for _, name := range files[:len(files)-1] {
		source := filepath.Join(bundle, name)
		if name == "mihomo-tui" {
			source = filepath.Join(bundle, "bin", name)
		}
		if err := copyPath(source, filepath.Join(stage, name)); err != nil {
			return err
		}
	}
	perm := os.FileMode(0700)
	if x.Mode == "system" {
		perm = 0755
	}
	if err := os.Chmod(filepath.Join(stage, "mihomo-tui"), perm); err != nil {
		return err
	}
	if err := state.Write(filepath.Join(stage, ".mihomo-tui-install"), []byte(fmt.Sprintf("mihomo-tui-install-v1\n%s\n%s\n%s\n", x.Mode, x.Unit, x.Prefix)), 0600); err != nil {
		return err
	}
	dataFiles, err := stageData(ctx, bundle, stage, s.Root)
	if err != nil {
		return err
	}
	files = append(files, dataFiles...)
	// Backup generated files and the rc before any writes, including ownership.
	snapshots := []snapshot{}
	paths := []string{shellenv.FilePath(s), filepath.Join(s.Root, "shell-integration.json")}
	if rc != "" {
		if _, err := os.Lstat(rc); err == nil {
			resolved, err := filepath.EvalSymlinks(rc)
			if err != nil {
				return err
			}
			rc = resolved
		} else if !os.IsNotExist(err) {
			return err
		}
		paths = append(paths, rc)
	}
	for _, path := range paths {
		shot, err := capture(path)
		if err != nil {
			return err
		}
		snapshots = append(snapshots, shot)
	}
	// Keep durable private copies as well as in-memory snapshots, so an error
	// during rollback does not discard the only copy of an rc or proxy env.
	if err := saveRecovery(stage, snapshots); err != nil {
		return err
	}
	active := false
	if x.Unit != "" {
		if _, err := run(ctx, x.Mode, "show-environment"); err != nil {
			return err
		}
		status, err := run(ctx, x.Mode, "show", "--property=ActiveState", "--value", "mihomo-tui.service")
		if err != nil {
			return err
		}
		switch status {
		case "active":
			active = true
		case "inactive", "failed":
		default:
			return fmt.Errorf("service is transitioning (%s); retry when it is active or stopped", status)
		}
	}
	changes := []replaced{}
	stopped, modified := false, false
	var lock *os.File
	defer func() {
		if lock != nil {
			lock.Close()
		}
	}()
	defer func() {
		if result == nil {
			return
		}
		var recovery []error
		if modified && active {
			_, err := run(context.Background(), x.Mode, "stop", "mihomo-tui.service")
			recovery = append(recovery, err)
		}
		for i := len(changes) - 1; i >= 0; i-- {
			change := changes[i]
			if err := os.RemoveAll(change.target); err != nil {
				recovery = append(recovery, err)
				continue
			}
			if change.backup != "" {
				recovery = append(recovery, os.Rename(change.backup, change.target))
			}
		}
		if modified {
			for _, shot := range snapshots {
				recovery = append(recovery, shot.restore())
			}
		}
		if lock != nil {
			recovery = append(recovery, unix.Flock(int(lock.Fd()), unix.LOCK_UN))
		}
		if errors.Join(recovery...) == nil && stopped && active {
			_, err := run(context.Background(), x.Mode, "start", "mihomo-tui.service")
			recovery = append(recovery, err)
			if err == nil {
				recovery = append(recovery, ready(context.Background(), s))
			}
		}
		if err := errors.Join(recovery...); err != nil {
			keepBackup = true
			result = fmt.Errorf("%w; rollback incomplete: %v; retained recovery files in %s", result, err, stage)
		} else {
			result = fmt.Errorf("%w; original installation restored", result)
		}
	}()
	if active {
		// A failed stop may still have stopped the process; try restoring state.
		stopped = true
		if _, err := run(ctx, x.Mode, "stop", "mihomo-tui.service"); err != nil {
			return err
		}
	}
	// Hold the core's lock throughout replacement. Foreground cores are not
	// killed by the installer, and a concurrent startup cannot race the swap.
	lock, err = os.OpenFile(filepath.Join(s.Root, "runtime", "core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("core is still running; stop the foreground core before updating")
	}
	for i, name := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(x.Prefix, name)
		change := replaced{target: target}
		if _, err := os.Lstat(target); err == nil {
			if err := realPath(target, name == "docs" || name == "licenses"); err != nil {
				return err
			}
			change.backup = filepath.Join(stage, fmt.Sprintf("backup-%d", i))
			if name == "docs" || name == "licenses" {
				if err := os.Rename(target, change.backup); err != nil {
					return err
				}
			} else if err := copyPath(target, change.backup); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		changes = append(changes, change)
		modified = true
		if err := os.Rename(filepath.Join(stage, name), target); err != nil {
			return err
		}
	}
	if err := shellenv.Write(s); err != nil {
		return err
	}
	if rc != "" {
		if _, err := shellenv.Register(s, filepath.Join(x.Prefix, "mihomo-tui"), rc); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		return err
	}
	if active {
		if _, err := run(ctx, x.Mode, "start", "mihomo-tui.service"); err != nil {
			return err
		}
		status, err := run(ctx, x.Mode, "show", "--property=ActiveState", "--value", "mihomo-tui.service")
		if err != nil {
			return err
		}
		if status != "active" {
			return fmt.Errorf("updated service did not become active (%s)", status)
		}
		if err := ready(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// systemd Type=simple can become active before the core has finished starting.
// Wait for this installation's supervisor and authenticated API before success.
func waitReady(ctx context.Context, s *state.Store) error {
	c, err := s.Read()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := controller.New(c)
	defer client.Close()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		running, err := core.Running(s)
		if err != nil {
			return err
		}
		if running {
			probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			var version struct{ Version string }
			err := client.Request(probe, "GET", "/version", nil, &version)
			cancel()
			if err == nil && version.Version != "" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("service core did not become ready before the startup deadline")
		case <-ticker.C:
		}
	}
}

func saveRecovery(stage string, snapshots []snapshot) error {
	type record struct {
		Path, Backup string
		Existed      bool
		Mode         uint32
		UID, GID     uint32
	}
	records := []record{}
	for i, shot := range snapshots {
		x := record{Path: shot.path, Existed: shot.info != nil}
		if shot.info != nil {
			x.Backup = fmt.Sprintf("snapshot-%d", i)
			x.Mode = uint32(shot.info.Mode().Perm())
			stat := shot.info.Sys().(*syscall.Stat_t)
			x.UID, x.GID = stat.Uid, stat.Gid
			if err := state.Write(filepath.Join(stage, x.Backup), shot.data, 0600); err != nil {
				return err
			}
		}
		records = append(records, x)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return state.Write(filepath.Join(stage, "recovery.json"), data, 0600)
}

func copyPath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular update file: %s", source)
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	return errors.Join(copyErr, syncErr, out.Close())
}

type snapshot struct {
	path string
	data []byte
	info os.FileInfo
}

func capture(path string) (snapshot, error) {
	x := snapshot{path: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return x, nil
	}
	if err != nil {
		return x, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return x, fmt.Errorf("update requires a regular file smaller than 8 MiB: %s", path)
	}
	x.info = info
	x.data, err = os.ReadFile(path)
	return x, err
}

func (x snapshot) restore() error {
	if x.info == nil {
		return os.RemoveAll(x.path)
	}
	if err := state.Write(x.path, x.data, x.info.Mode().Perm()); err != nil {
		return err
	}
	stat := x.info.Sys().(*syscall.Stat_t)
	return os.Chown(x.path, int(stat.Uid), int(stat.Gid))
}
