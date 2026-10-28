package shellenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"verge-tui/internal/testutil"
)

func TestFishAutoloadAndCleanupPreserveUserConfiguration(t *testing.T) {
	fish := os.Getenv("VERGE_TUI_TEST_FISH")
	if fish == "" {
		var err error
		fish, err = exec.LookPath("fish")
		if err != nil {
			t.Skip("set VERGE_TUI_TEST_FISH to test fish functions")
		}
	}
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(s.Root)
	rc := filepath.Join(root, "config", "fish", "config.fish")
	if err := os.MkdirAll(filepath.Dir(rc), 0700); err != nil {
		t.Fatal(err)
	}
	original := "# custom fish configuration\nset -gx EDITOR vim\n"
	if err := os.WriteFile(rc, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Fish's $literal `bin` \\ path")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "verge-tui")
	testutil.ShellScripts(t, dir)
	c, _ := s.Read()
	exports, err := ExportsForShell(c, "fish")
	if err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\nif [ \"$3\" = env ]; then printf '%s' " + Quote(exports) + "; else printf '%s\\n' \"${http_proxy-unset}\"; fi\n"
	if err := os.WriteFile(executable, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := RegisterShell(s, executable, rc, "fish"); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(rc)
	if string(data) != original {
		t.Fatal("fish registration modified config.fish")
	}
	script := "set -g fish_function_path " + FishQuote(filepath.Join(filepath.Dir(rc), "functions")) + " $fish_function_path\nsource " + FishQuote(filepath.Join(filepath.Dir(rc), "conf.d", "verge-tui.fish")) + "\nsource " + FishQuote(filepath.Join(filepath.Dir(rc), "conf.d", "verge-tui.fish")) + "\n" + `
set -l matches 0
for item in $PATH
    if test "$item" = ` + FishQuote(dir) + `
        set matches (math $matches + 1)
    end
end
test $matches -eq 1; or exit 11
verge-on; or exit 12
test "$http_proxy" = "$https_proxy"; or exit 13
test "$http_proxy" = "$ALL_PROXY"; or exit 14
verge-status
verge-off
set -qgx http_proxy; and exit 15
verge-status
`
	command := exec.Command(fish, "--no-config", "-c", script)
	command.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_DATA_HOME="+filepath.Join(root, "user-data"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"))
	out, err := command.CombinedOutput()
	if err != nil || string(out) != "Proxy environment: enabled.\n"+ProxyURL(c)+"\nProxy environment: disabled.\nunset\n" {
		t.Fatalf("fish functions: %v\n%s", err, out)
	}
	if err := Remove(s); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(rc)
	if string(data) != original {
		t.Fatal("cleanup changed config.fish")
	}
	for _, path := range []string{filepath.Join(filepath.Dir(rc), "functions", "verge-on.fish"), filepath.Join(filepath.Dir(rc), "conf.d", "verge-tui.fish")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("managed fish file was retained", path, err)
		}
	}
}

func TestFishRefusesToOverwriteExistingUserFunctions(t *testing.T) {
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(filepath.Dir(s.Root), "verge-tui")
	testutil.ShellScripts(t, filepath.Dir(executable))
	rc := filepath.Join(filepath.Dir(s.Root), "fish", "config.fish")
	path := filepath.Join(filepath.Dir(rc), "functions", "verge-on.fish")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := "function verge-on; echo custom; end\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterShell(s, executable, rc, "fish"); err == nil {
		t.Fatal("overwrote an unrelated fish function")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("failed registration changed user content")
	}
}

func TestResolveFishRCAndRejectUnsupportedShells(t *testing.T) {
	root := filepath.Dir(store(t).Root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	rc, err := ResolveRC("/usr/bin/fish")
	if err != nil || rc != filepath.Join(root, "config", "fish", "config.fish") {
		t.Fatal(rc, err)
	}
	for _, shell := range []string{"dash", "ash", "ksh", "mksh", "csh", "tcsh", "nu"} {
		if Supported(shell) {
			t.Fatal("unsupported shell was treated as Bash", shell)
		}
		if _, err := ResolveRC(shell); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatal(shell, err)
		}
	}
}
