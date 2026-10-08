package installation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"verge-tui/internal/state"
)

const SummaryFile = "installation.json"

var errOtherCommand = errors.New("command path belongs to another file or installation; it will not be overwritten")

// Public discovery contains no controller secrets, ports or subscription data.
// It is only a menu hint; Inspect validates private state after elevation.
type Summary struct {
	Prefix      string `json:"prefix"`
	Mode        string `json:"mode"`
	Version     string `json:"version"`
	State       string `json:"state"`
	CommandLink string `json:"command_link,omitempty"`
}

func Discover(prefix string) (Summary, error) {
	var s Summary
	if !filepath.IsAbs(prefix) || strings.ContainsAny(prefix, "\r\n\x00") {
		return s, errors.New("installation prefix must be absolute")
	}
	info, err := os.Lstat(filepath.Join(prefix, SummaryFile))
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return s, errors.New("invalid public installation summary")
	}
	data, err := os.ReadFile(filepath.Join(prefix, SummaryFile))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Prefix != filepath.Clean(prefix) || (s.Mode != "user" && s.Mode != "system") || (s.State != "installed" && s.State != "retained") || strings.ContainsAny(s.Version, "\r\n\x00") {
		return Summary{}, errors.New("invalid or moved public installation summary")
	}
	return s, nil
}

func Record(prefix, version, link string) error {
	c, err := (&state.Store{Root: filepath.Join(prefix, "data")}).Read()
	if err != nil {
		return err
	}
	s := Summary{Prefix: filepath.Clean(prefix), Mode: c.InstallMode, Version: version, State: "installed", CommandLink: link}
	if _, err := os.Stat(filepath.Join(prefix, "verge-tui")); os.IsNotExist(err) {
		s.State = "retained"
	} else if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return state.Write(filepath.Join(prefix, SummaryFile), append(data, '\n'), 0644)
}

func MarkRetained(prefix string) error {
	s, err := Discover(prefix)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	s.State = "retained"
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return state.Write(filepath.Join(prefix, SummaryFile), append(data, '\n'), 0644)
}

func CheckCommandLink(prefix, path string) error {
	if !filepath.IsAbs(path) || filepath.Base(path) != "verge-tui" || strings.ContainsAny(path, "\r\n\x00") {
		return errors.New("invalid command link path")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errOtherCommand
	}
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	if filepath.Clean(target) != filepath.Join(filepath.Clean(prefix), "verge-tui") {
		return errOtherCommand
	}
	return nil
}

func InstallCommandLink(prefix, path string) error {
	if err := CheckCommandLink(prefix, path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.Symlink(filepath.Join(filepath.Clean(prefix), "verge-tui"), path)
}

func RemoveCommandLink(prefix, path string) error {
	// A replaced link belongs to its new owner. Uninstall must leave it alone.
	if err := CheckCommandLink(prefix, path); errors.Is(err, errOtherCommand) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
