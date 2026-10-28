package shellenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"verge-tui/internal/state"
)

func FishQuote(value string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(value) + "'"
}

func ExportsForShell(c state.Settings, shell string) (string, error) {
	shell = NormalizeShell(shell)
	if !Supported(shell) {
		return "", errors.New("unsupported shell export format")
	}
	if shell != "fish" {
		return Exports(c), nil
	}
	var out strings.Builder
	values := Expected(c)
	for _, name := range Variables {
		fmt.Fprintf(&out, "set -gx %s %s\n", name, FishQuote(values[name]))
	}
	return out.String(), nil
}

func inferShell(rc string) string {
	if strings.HasSuffix(rc, ".fish") {
		return "fish"
	}
	if filepath.Base(rc) == ".zshrc" {
		return "zsh"
	}
	return "bash"
}

type shellFile struct{ Path, Data string }

// Scripts ship with the application; registration only writes references.
func ScriptPath(executable, shell string) string {
	root := filepath.Join(filepath.Dir(executable), "shell")
	if shell == "fish" {
		return filepath.Join(root, "fish", "init.fish")
	}
	return filepath.Join(root, "proxy.sh")
}

func verifyScripts(executable, shell string) error {
	paths := []string{ScriptPath(executable, shell)}
	if shell == "fish" {
		for _, name := range []string{"verge-on", "verge-off", "verge-status", "_verge_tui_message"} {
			paths = append(paths, filepath.Join(filepath.Dir(executable), "shell", "fish", "functions", name+".fish"))
		}
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("bundled shell script unavailable; reinstall the offline package: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("bundled shell script must be a real regular file: %s", path)
		}
	}
	return nil
}

func integrationFiles(s *state.Store, executable, rc, shell string) ([]shellFile, string, error) {
	id := blockID(s)
	start, end := markers(id)
	c, err := ReadFile(FilePath(s))
	if err != nil {
		return nil, "", err
	}
	script := ScriptPath(executable, shell)
	if shell == "fish" {
		path := filepath.Join(filepath.Dir(rc), "conf.d", "verge-tui.fish")
		loader := fmt.Sprintf("%s\nif test -r %s\n    source %s %s %s %s\nend\n%s\n", start, FishQuote(script), FishQuote(script), FishQuote(executable), FishQuote(s.Root), FishQuote(c.InstallMode), end)
		return []shellFile{{path, loader}}, "", nil
	}
	loader := fmt.Sprintf("%s\nif [ -r %s ]; then\n    . %s %s %s %s\nfi\n%s\n", start, Quote(script), Quote(script), Quote(executable), Quote(s.Root), Quote(c.InstallMode), end)
	return nil, loader, nil
}

// Only rewrite/remove files carrying this installation's complete markers.
// Refuse pre-existing user functions rather than replacing them silently.
func validateManagedFile(path, id string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed shell file must be a real regular file: %s", path)
	}
	data, _, err := readRC(path)
	if err != nil {
		return err
	}
	start, end := markers(id)
	if !strings.HasPrefix(data, start+"\n") || !strings.HasSuffix(data, end+"\n") {
		return fmt.Errorf("shell file belongs to another configuration: %s", path)
	}
	stripped, err := stripBlock(data, id)
	if err != nil {
		return err
	}
	if stripped != "" {
		return fmt.Errorf("shell file contains user content and will not be overwritten: %s", path)
	}
	return nil
}

func ManagedPaths(s *state.Store, executable, rc, shell string) ([]string, error) {
	files, _, err := integrationFiles(s, executable, rc, shell)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	var reg registration
	data, err := os.ReadFile(registrationPath(s))
	if err == nil {
		if err := json.Unmarshal(data, &reg); err != nil {
			return nil, err
		}
		if reg.ID != blockID(s) {
			return nil, errors.New("invalid shell registration")
		}
		for _, path := range append(append([]string(nil), reg.Files...), reg.ExtraRCs...) {
			if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
				return nil, errors.New("invalid managed shell path")
			}
			if !containsPath(paths, path) {
				paths = append(paths, path)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	login, _, err := loginCandidate(rc, shell, reg)
	if err != nil {
		return nil, err
	}
	resolvedRC, err := resolveRCPath(rc)
	if err != nil {
		return nil, err
	}
	if login != "" && login != resolvedRC && !containsPath(paths, login) {
		paths = append(paths, login)
	}
	return paths, nil
}

func containsPath(paths []string, path string) bool {
	for _, item := range paths {
		if item == path {
			return true
		}
	}
	return false
}

func RegisteredShell(s *state.Store, rc string) string {
	data, err := os.ReadFile(registrationPath(s))
	if err == nil {
		var reg registration
		if json.Unmarshal(data, &reg) == nil && reg.Shell != "" {
			return reg.Shell
		}
	}
	return inferShell(rc)
}

func RegisteredRC(s *state.Store) (string, error) {
	data, err := os.ReadFile(registrationPath(s))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var reg registration
	if err := json.Unmarshal(data, &reg); err != nil {
		return "", err
	}
	if reg.ID != blockID(s) || !filepath.IsAbs(reg.RC) {
		return "", errors.New("invalid shell registration")
	}
	return reg.RC, nil
}

func RegisteredConfig(s *state.Store, uid int) (string, string, error) {
	if uid != os.Geteuid() && os.Geteuid() != 0 {
		return "", "", errors.New("inspecting another user's shell registration requires root")
	}
	info, err := os.Stat(s.Root)
	if os.IsNotExist(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(s.Root, "shell-integration.json")
	if uid != int(info.Sys().(*syscall.Stat_t).Uid) {
		path = filepath.Join(state.ShellUsersPath(s), strconv.Itoa(uid)+".json")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var reg registration
	if err := json.Unmarshal(data, &reg); err != nil {
		return "", "", err
	}
	if reg.ID != blockID(s) || !filepath.IsAbs(reg.RC) || strings.ContainsAny(reg.RC, "\r\n\x00") {
		return "", "", errors.New("invalid shell registration")
	}
	shell := reg.Shell
	if shell == "" {
		shell = inferShell(reg.RC)
	}
	if !Supported(shell) {
		return "", "", errors.New("invalid registered shell")
	}
	return reg.RC, shell, nil
}
