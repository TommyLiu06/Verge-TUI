package shellenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func CurrentRC() (string, error) {
	shell := filepath.Base(os.Getenv("SHELL"))
	if out, err := exec.Command("ps", "-p", strconv.Itoa(os.Getppid()), "-o", "comm=").Output(); err == nil {
		parent := strings.TrimPrefix(strings.TrimSpace(string(out)), "-")
		if parent == "bash" || parent == "zsh" {
			shell = parent
		}
	}
	return resolveRC(shell, os.Getenv("HOME"), os.Getenv("ZDOTDIR"))
}

func resolveRC(shell, home, zdot string) (string, error) {
	if shell == "zsh" && zdot != "" {
		home = zdot
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("shell rc directory must be absolute; use --rc-file")
	}
	switch shell {
	case "bash":
		return filepath.Join(home, ".bashrc"), nil
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	default:
		return "", errors.New("cannot determine Bash or Zsh; use --rc-file")
	}
}
