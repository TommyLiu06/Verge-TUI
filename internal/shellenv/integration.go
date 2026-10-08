package shellenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"verge-tui/internal/state"
)

type registration struct {
	RC string `json:"rc"`
	ID string `json:"id"`
}

func registrationPath(s *state.Store) string {
	if info, err := os.Stat(s.Root); err == nil && int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return filepath.Join(state.ShellUsersPath(s), strconv.Itoa(os.Geteuid())+".json")
	}
	return filepath.Join(s.Root, "shell-integration.json")
}
func blockID(s *state.Store) string {
	hash := sha256.Sum256([]byte(s.Root))
	return hex.EncodeToString(hash[:8])
}
func markers(id string) (string, string) {
	return "# >>> verge-tui " + id + " >>>", "# <<< verge-tui " + id + " <<<"
}
func Functions(s *state.Store, executable string) string {
	start, end := markers(blockID(s))
	env := Quote(FilePath(s))
	return fmt.Sprintf(`%s
case ":${PATH-}:" in
    *:%s:*) ;;
    *) export PATH=%s"${PATH:+:$PATH}" ;;
esac
verge_on() {
    if [ ! -r %s ]; then
        printf '%%s\n' 'Proxy environment file is unavailable.' >&2
        return 1
    fi
    . %s
}
verge_off() {
    unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY
}
verge_status() {
    command %s --data-dir %s proxy-status
}
%s
`, start, Quote(filepath.Dir(executable)), Quote(filepath.Dir(executable)), env, env, Quote(executable), Quote(s.Root), end)
}
func stripBlock(data, id string) (string, error) {
	start, end := markers(id)
	lines := strings.SplitAfter(data, "\n")
	inside, found := false, false
	var out strings.Builder
	for _, line := range lines {
		text := strings.TrimSuffix(line, "\n")
		if text == start {
			if inside || found {
				return "", errors.New("duplicate shell integration markers; rc file was not changed")
			}
			inside, found = true, true
			continue
		}
		if text == end {
			if !inside {
				return "", errors.New("incomplete shell integration markers; rc file was not changed")
			}
			inside = false
			continue
		}
		if !inside {
			out.WriteString(line)
		}
	}
	if inside {
		return "", errors.New("incomplete shell integration markers; rc file was not changed")
	}
	return out.String(), nil
}
func readRC(path string) (string, os.FileInfo, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", nil, errors.New("shell rc must be a regular file smaller than 8 MiB")
	}
	data, err := os.ReadFile(path)
	return string(data), info, err
}
func writeRC(path, data string, info os.FileInfo) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".mihomo-rc-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	mode := os.FileMode(0600)
	if info != nil {
		mode = info.Mode().Perm()
		stat := info.Sys().(*syscall.Stat_t)
		if int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
			if err := f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
				return err
			}
		}
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.WriteString(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func Register(s *state.Store, executable, rc string) (string, error) {
	return registerAt(s, executable, rc, registrationPath(s))
}

func registerAt(s *state.Store, executable, rc, metaPath string) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(rc) || strings.ContainsAny(executable+rc, "\r\n\x00") {
		return "", errors.New("shell integration paths must be absolute single-line paths")
	}
	if strings.Contains(filepath.Dir(executable), ":") {
		return "", errors.New("the executable directory cannot contain a colon when adding it to PATH")
	}
	if _, err := ReadFile(FilePath(s)); err != nil {
		return "", err
	}
	if _, err := os.Lstat(rc); err == nil {
		resolved, err := filepath.EvalSymlinks(rc)
		if err != nil {
			return "", err
		}
		rc = resolved
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if data, err := os.ReadFile(metaPath); err == nil {
		var old registration
		if err := json.Unmarshal(data, &old); err != nil {
			return "", err
		}
		if old.RC != rc {
			return "", errors.New("shell integration is already registered in another rc file; remove it first")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	data, info, err := readRC(rc)
	if err != nil {
		return "", err
	}
	data, err = stripBlock(data, blockID(s))
	if err != nil {
		return "", err
	}
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil {
		return "", err
	}
	meta, _ := json.Marshal(registration{RC: rc, ID: blockID(s)})
	if err := state.Write(metaPath, meta, 0600); err != nil {
		return "", err
	}
	if err := writeRC(rc, data+Functions(s, executable), info); err != nil {
		return "", err
	}
	return rc, nil
}
func Remove(s *state.Store) error {
	return removeAt(s, registrationPath(s))
}

func removeAt(s *state.Store, metaPath string) error {
	data, err := os.ReadFile(metaPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var reg registration
	if err := json.Unmarshal(data, &reg); err != nil {
		return err
	}
	if reg.ID != blockID(s) || !filepath.IsAbs(reg.RC) {
		return errors.New("invalid shell integration record")
	}
	rc, info, err := readRC(reg.RC)
	if err != nil {
		return err
	}
	stripped, err := stripBlock(rc, reg.ID)
	if err != nil {
		return err
	}
	if stripped != rc {
		if err := writeRC(reg.RC, stripped, info); err != nil {
			return err
		}
	}
	return os.Remove(metaPath)
}

// RemoveAll is used by uninstallation. Each user's record is removed by a
// process running as that user, so root never edits an untrusted rc path.
func RemoveAll(s *state.Store, executable string) error {
	return removeAll(s, os.Geteuid(), func(uid, gid uint32) error {
		cmd := exec.Command(executable, "--data-dir", s.Root, "shell", "uninstall")
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid}}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("remove shell functions for uid %d: %w: %s", uid, err, strings.TrimSpace(string(out)))
		}
		return nil
	})
}

func removeAll(s *state.Store, uid int, cleanup func(uint32, uint32) error) error {
	if err := Remove(s); err != nil {
		return err
	}
	entries, err := os.ReadDir(state.ShellUsersPath(s))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(state.ShellUsersPath(s), entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || entry.Name() != strconv.Itoa(int(stat.Uid))+".json" {
			return errors.New("invalid per-user shell registration")
		}
		if int(stat.Uid) == uid {
			// A user-owned installation normally has no shared records.
			return errors.New("unexpected shared registration for the installation owner")
		}
		if uid != 0 {
			return errors.New("removing other users' shell functions requires root")
		}
		if err := cleanup(stat.Uid, stat.Gid); err != nil {
			return err
		}
	}
	return nil
}
