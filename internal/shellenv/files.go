package shellenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func integrationFiles(s *state.Store, executable, rc, shell string) ([]shellFile, string, error) {
	id := blockID(s)
	start, end := markers(id)
	if shell == "fish" {
		dir := filepath.Dir(rc)
		var files []shellFile
		bodies := map[string]string{
			"verge_on":     fmt.Sprintf("function verge_on\n    set -l proxy_commands (command %s --data-dir %s env --shell fish)\n    or return $status\n    printf '%%s\\n' $proxy_commands | source\nend\n", FishQuote(executable), FishQuote(s.Root)),
			"verge_off":    "function verge_off\n    for proxy_variable in http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY\n        set -eg $proxy_variable\n        if set -qU $proxy_variable\n            set -gu $proxy_variable ''\n        end\n    end\nend\n",
			"verge_status": fmt.Sprintf("function verge_status\n    command %s --data-dir %s proxy-status\nend\n", FishQuote(executable), FishQuote(s.Root)),
		}
		for _, name := range []string{"verge_on", "verge_off", "verge_status"} {
			files = append(files, shellFile{filepath.Join(dir, "functions", name+".fish"), start + "\n" + bodies[name] + end + "\n"})
		}
		path := filepath.Join(dir, "conf.d", "verge-tui.fish")
		// Global, session-only PATH changes are removable without leaving
		// fish universal variables behind after uninstalling the application.
		body := fmt.Sprintf("if not contains -- %s $PATH\n    set -gx PATH %s $PATH\nend\n", FishQuote(filepath.Dir(executable)), FishQuote(filepath.Dir(executable)))
		files = append(files, shellFile{path, start + "\n" + body + end + "\n"})
		return files, "", nil
	}
	// Keep all application-owned shell files beside the chosen rc, including
	// custom rc paths; neither tests nor installers need to touch another home.
	path := filepath.Join(filepath.Dir(rc), ".verge-tui", id+".sh")
	loader := fmt.Sprintf("%s\nif [ -r %s ]; then\n    . %s\nfi\n%s\n", start, Quote(path), Quote(path), end)
	return []shellFile{{path, Functions(s, executable)}}, loader, nil
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
	return paths, nil
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
