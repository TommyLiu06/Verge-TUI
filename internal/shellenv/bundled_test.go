package shellenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"verge-tui/internal/testutil"
)

func TestMissingBundledScriptsDoNotGenerateOrRegisterFunctions(t *testing.T) {
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Dir(s.Root)
	rc := filepath.Join(prefix, ".bashrc")
	if err := os.WriteFile(rc, []byte("# existing configuration\n"), 0640); err != nil {
		t.Fatal(err)
	}
	_, err := RegisterShell(s, filepath.Join(prefix, "verge-tui"), rc, "bash")
	if err == nil || !strings.Contains(err.Error(), "bundled shell script unavailable") {
		t.Fatal("missing assets were not reported", err)
	}
	data, _ := os.ReadFile(rc)
	if string(data) != "# existing configuration\n" {
		t.Fatal("missing assets changed the rc")
	}
	for _, path := range []string{registrationPath(s), filepath.Join(prefix, "shell"), filepath.Join(prefix, ".verge-tui")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("missing scripts were generated or registered", path, err)
		}
	}
}

func TestRegistrationRefreshRemovesObsoleteOwnedFilesAndPreservesBundledAssets(t *testing.T) {
	for _, shell := range []string{"bash", "fish"} {
		t.Run(shell, func(t *testing.T) {
			s := store(t)
			if err := Write(s); err != nil {
				t.Fatal(err)
			}
			prefix := filepath.Dir(s.Root)
			testutil.ShellScripts(t, prefix)
			executable := filepath.Join(prefix, "verge-tui")
			rc := filepath.Join(prefix, "config", "rc")
			if _, err := RegisterShell(s, executable, rc, shell); err != nil {
				t.Fatal(err)
			}
			asset := ScriptPath(executable, shell)
			before, _ := os.ReadFile(asset)
			obsolete := filepath.Join(prefix, ".verge-tui", "obsolete.sh")
			if shell == "fish" {
				obsolete = filepath.Join(filepath.Dir(rc), "functions", "verge-on.fish")
			}
			if err := os.MkdirAll(filepath.Dir(obsolete), 0700); err != nil {
				t.Fatal(err)
			}
			start, end := markers(blockID(s))
			if err := os.WriteFile(obsolete, []byte(start+"\n# obsolete owned implementation\n"+end+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(registrationPath(s))
			var reg registration
			if err := json.Unmarshal(data, &reg); err != nil {
				t.Fatal(err)
			}
			reg.Files = append(reg.Files, obsolete)
			data, _ = json.Marshal(reg)
			if err := os.WriteFile(registrationPath(s), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := RegisterShell(s, executable, rc, shell); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(obsolete); !os.IsNotExist(err) {
				t.Fatal("registration refresh retained an obsolete owned implementation", err)
			}
			if err := Remove(s); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(asset)
			if err != nil || string(after) != string(before) {
				t.Fatal("user registration modified or removed a bundled script", err)
			}
		})
	}
}
