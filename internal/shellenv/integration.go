package shellenv

import (
	"crypto/sha256"
	"encoding/hex"
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
	RC       string   `json:"rc"`
	ID       string   `json:"id"`
	Shell    string   `json:"shell"`
	Files    []string `json:"files"`
	ExtraRCs []string `json:"extra_rcs,omitempty"`
	LoginDir string   `json:"login_dir,omitempty"`
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
	return RegisterShell(s, executable, rc, RegisteredShell(s, rc))
}

func RegisterShell(s *state.Store, executable, rc, shell string) (string, error) {
	return registerShellAt(s, executable, rc, shell, registrationPath(s))
}

func registerAt(s *state.Store, executable, rc, metaPath string) (string, error) {
	return registerShellAt(s, executable, rc, inferShell(rc), metaPath)
}

func registerOne(s *state.Store, executable, rc, shell string, old registration, save func(registration) error) (string, error) {
	originalRC := rc
	shell = NormalizeShell(shell)
	if !Supported(shell) {
		return "", errors.New("shell integration supports Bash, Zsh and fish")
	}
	if !filepath.IsAbs(executable) || !filepath.IsAbs(rc) || strings.ContainsAny(executable+rc, "\r\n\x00") {
		return "", errors.New("shell integration paths must be absolute single-line paths")
	}
	if strings.Contains(filepath.Dir(executable), ":") {
		return "", errors.New("the executable directory cannot contain a colon when adding it to PATH")
	}
	if _, err := ReadFile(FilePath(s)); err != nil {
		return "", err
	}
	if err := verifyScripts(executable, shell); err != nil {
		return "", err
	}
	if _, err := os.Lstat(rc); err == nil && shell != "fish" {
		resolved, err := filepath.EvalSymlinks(rc)
		if err != nil {
			return "", err
		}
		rc = resolved
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, path := range old.Files {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return "", errors.New("invalid managed shell path")
		}
		if err := validateManagedFile(path, blockID(s)); err != nil {
			return "", err
		}
	}
	if shell == "fish" {
		// Prepending the bundled autoload directory must not shadow unrelated
		// user functions. Previously registered owned files may be replaced.
		for _, name := range []string{"verge-on", "verge-off", "verge-status"} {
			path := filepath.Join(filepath.Dir(rc), "functions", name+".fish")
			if err := validateManagedFile(path, blockID(s)); err != nil {
				return "", err
			}
		}
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
	files, loader, err := integrationFiles(s, executable, rc, shell)
	if err != nil {
		return "", err
	}
	loginChanges, loginPaths, loginDir, err := prepareLoginRCs(s, executable, originalRC, rc, shell, old)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if err := validateManagedFile(file.Path, blockID(s)); err != nil {
			return "", err
		}
	}
	reg := registration{RC: rc, ID: blockID(s), Shell: shell, ExtraRCs: loginPaths, LoginDir: loginDir}
	for _, file := range files {
		reg.Files = append(reg.Files, file.Path)
	}
	// Record ownership first, so an interrupted write can still be cleaned up.
	pending := reg
	pending.Files = append([]string(nil), reg.Files...)
	for _, path := range old.Files {
		if !containsPath(pending.Files, path) {
			pending.Files = append(pending.Files, path)
		}
	}
	pending.ExtraRCs = append([]string(nil), reg.ExtraRCs...)
	for _, path := range old.ExtraRCs {
		if !containsPath(pending.ExtraRCs, path) {
			pending.ExtraRCs = append(pending.ExtraRCs, path)
		}
	}
	if err := save(pending); err != nil {
		return "", err
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0700); err != nil {
			return "", err
		}
		if err := state.Write(file.Path, []byte(file.Data), 0600); err != nil {
			return "", err
		}
	}
	if shell != "fish" {
		if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil {
			return "", err
		}
		if err := writeRC(rc, data+loader, info); err != nil {
			return "", err
		}
	}
	for _, change := range loginChanges {
		if err := writeRCChange(change); err != nil {
			return "", err
		}
	}
	for _, path := range old.Files {
		if !containsPath(reg.Files, path) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return "", err
			}
		}
	}
	if err := save(reg); err != nil {
		return "", err
	}
	return rc, nil
}
func Remove(s *state.Store) error {
	return removeAt(s, registrationPath(s))
}

func removeOne(s *state.Store, reg registration) error {
	extraChanges, err := prepareExtraRCRemoval(reg)
	if err != nil {
		return err
	}
	for _, path := range reg.Files {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("invalid managed shell path")
		}
		if err := validateManagedFile(path, reg.ID); err != nil {
			return err
		}
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
	for _, change := range extraChanges {
		if err := writeRCChange(change); err != nil {
			return err
		}
	}
	for _, path := range reg.Files {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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
		if registrationLockEntry(entry.Name(), info) {
			continue
		}
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
