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
	return ResolveRC(CurrentShell())
}

// Prefer the actual interactive parent over the account's login shell. The
// installer passes --shell explicitly because its immediate parent is sh.
func CurrentShell() string {
	shell := NormalizeShell(os.Getenv("SHELL"))
	if out, err := exec.Command("ps", "-p", strconv.Itoa(os.Getppid()), "-o", "comm=").Output(); err == nil {
		parent := NormalizeShell(strings.TrimSpace(string(out)))
		if Supported(parent) {
			shell = parent
		}
	}
	return shell
}

func NormalizeShell(shell string) string {
	shell = strings.TrimPrefix(filepath.Base(shell), "-")
	return shell
}

func Supported(shell string) bool {
	switch NormalizeShell(shell) {
	case "bash", "zsh", "fish":
		return true
	}
	return false
}

func ResolveRC(shell string) (string, error) {
	shell = NormalizeShell(shell)
	home := os.Getenv("HOME")
	if shell == "fish" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		if !filepath.IsAbs(base) {
			return "", errors.New("fish configuration directory must be absolute")
		}
		return filepath.Join(base, "fish", "config.fish"), nil
	}
	return resolveRC(shell, home, os.Getenv("ZDOTDIR"))
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
		return "", errors.New("unsupported shell; select --shell bash, zsh or fish")
	}
}
