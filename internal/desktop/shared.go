package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
)

func UserStore(s *state.Store) *state.Store {
	return &state.Store{Root: filepath.Join(state.DesktopUsersPath(s), strconv.Itoa(os.Geteuid()))}
}

func PrepareUser(s *state.Store) error {
	if err := os.Mkdir(s.Root, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(s.Root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return errors.New("desktop backup directory belongs to another user")
	}
	return os.Chmod(s.Root, 0700)
}

func SaveSession(s *state.Store) error {
	values := map[string]string{}
	for _, name := range []string{"XDG_CURRENT_DESKTOP", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"} {
		values[name] = os.Getenv(name)
	}
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return state.Write(filepath.Join(s.Root, "session.json"), data, 0600)
}

func RestoreUser(ctx context.Context, s *state.Store) error {
	user := UserStore(s)
	if _, err := os.Stat(filepath.Join(user.Root, "gnome-proxy-backup.ini")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := PrepareUser(user); err != nil {
		return err
	}
	c, err := shellenv.ReadFile(shellenv.FilePath(s))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(user.Root, "session.json"))
	if err != nil {
		return err
	}
	var saved map[string]string
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	env := append([]string(nil), os.Environ()...)
	for _, name := range []string{"XDG_CURRENT_DESKTOP", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"} {
		for i, entry := range env {
			if strings.HasPrefix(entry, name+"=") {
				env[i] = name + "=" + saved[name]
			}
		}
		env = append(env, name+"="+saved[name])
	}
	run := func(ctx context.Context, name string, args []string, input string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.Output()
		if err != nil {
			return "", errors.New("GNOME session is unavailable; disable System Proxy in that user's TUI before uninstalling")
		}
		return strings.TrimSpace(string(out)), nil
	}
	if err := (GNOME{Store: user, Run: run}).Disable(ctx, c); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(user.Root, "session.json")); err != nil {
		return err
	}
	return os.Remove(user.Root)
}

func RestoreAll(s *state.Store, executable string) error {
	entries, err := os.ReadDir(state.DesktopUsersPath(s))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(state.DesktopUsersPath(s), entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || entry.Name() != strconv.Itoa(int(stat.Uid)) {
			return errors.New("invalid per-user desktop registration")
		}
		if int(stat.Uid) != os.Geteuid() && os.Geteuid() != 0 {
			return errors.New("restoring other users' desktops requires root")
		}
		cmd := exec.Command(executable, "--data-dir", s.Root, "desktop-restore")
		if int(stat.Uid) != os.Geteuid() {
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: stat.Uid, Gid: stat.Gid}}
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("restore desktop for uid %d: %w: %s", stat.Uid, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
