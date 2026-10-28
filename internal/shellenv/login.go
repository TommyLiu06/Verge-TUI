package shellenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"verge-tui/internal/state"
)

type rcChange struct {
	path, data string
	info       os.FileInfo
	changed    bool
}

func validRCPath(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "\r\n\x00")
}

func resolveRCPath(path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

// Bash reads only the first existing user login profile. Use the directory of
// the configured rc, so custom rc fixtures never inspect the real user's HOME.
func bashLoginRC(dir string) (string, error) {
	if !validRCPath(dir) {
		return "", errors.New("invalid Bash login directory")
	}
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); err == nil {
			return resolveRCPath(path)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return filepath.Join(dir, ".profile"), nil
}

func prepareRCChange(path, id, loader string) (rcChange, error) {
	if !validRCPath(path) {
		return rcChange{}, errors.New("invalid extra shell rc path")
	}
	data, info, err := readRC(path)
	if err != nil {
		return rcChange{}, err
	}
	stripped, err := stripBlock(data, id)
	if err != nil {
		return rcChange{}, err
	}
	if loader != "" {
		if stripped != "" && !strings.HasSuffix(stripped, "\n") {
			stripped += "\n"
		}
		stripped += loader
	}
	return rcChange{path: path, data: stripped, info: info, changed: stripped != data}, nil
}

func writeRCChange(change rcChange) error {
	if !change.changed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(change.path), 0700); err != nil {
		return err
	}
	return writeRC(change.path, change.data, change.info)
}

func loginCandidate(rc, shell string, old registration) (string, string, error) {
	if shell != "bash" {
		return "", "", nil
	}
	dir := filepath.Dir(rc)
	if old.LoginDir != "" {
		dir = old.LoginDir
	}
	path, err := bashLoginRC(dir)
	return path, dir, err
}

func prepareLoginRCs(s *state.Store, executable, originalRC, resolvedRC, shell string, old registration) ([]rcChange, []string, string, error) {
	path, dir, err := loginCandidate(originalRC, shell, old)
	if err != nil {
		return nil, nil, "", err
	}
	if path == resolvedRC {
		path = ""
	}
	var changes []rcChange
	var paths []string
	for _, previous := range old.ExtraRCs {
		if !validRCPath(previous) || previous == old.RC || containsPath(old.Files, previous) {
			return nil, nil, "", errors.New("invalid extra shell rc path")
		}
		if previous == path {
			continue
		}
		change, err := prepareRCChange(previous, blockID(s), "")
		if err != nil {
			return nil, nil, "", err
		}
		changes = append(changes, change)
	}
	if path != "" {
		c, err := ReadFile(FilePath(s))
		if err != nil {
			return nil, nil, "", err
		}
		start, end := markers(blockID(s))
		script := ScriptPath(executable, shell)
		loader := fmt.Sprintf("%s\nif [ -n \"${BASH_VERSION-}\" ] && [ -r %s ]; then\n    . %s %s %s %s\nfi\n%s\n", start, Quote(script), Quote(script), Quote(executable), Quote(s.Root), Quote(c.InstallMode), end)
		change, err := prepareRCChange(path, blockID(s), loader)
		if err != nil {
			return nil, nil, "", err
		}
		changes = append(changes, change)
		paths = append(paths, path)
	}
	return changes, paths, dir, nil
}

func prepareExtraRCRemoval(reg registration) ([]rcChange, error) {
	var changes []rcChange
	for _, path := range reg.ExtraRCs {
		if !validRCPath(path) || path == reg.RC || containsPath(reg.Files, path) {
			return nil, errors.New("invalid extra shell rc path")
		}
		change, err := prepareRCChange(path, reg.ID, "")
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}
