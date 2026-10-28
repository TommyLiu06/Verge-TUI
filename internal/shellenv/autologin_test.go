package shellenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestAutoLoginPreferencesDefaultAndExplicitDisable(t *testing.T) {
	s := store(t)
	if enabled, err := AutoLoginEnabled(s); err != nil || !enabled {
		t.Fatal("new preferences must enable login exports", enabled, err)
	}
	if err := state.Write(filepath.Join(s.Root, "preferences.json"), []byte("{\"theme\":\"terminal\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutoLoginEnabled(s); err != nil || !enabled {
		t.Fatal("older preferences without the setting must enable login exports", enabled, err)
	}
	p := state.DefaultPreferences()
	p.ProxyOnLogin = false
	if err := state.WritePreferences(s.Root, p); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutoLoginEnabled(s); err != nil || enabled {
		t.Fatal("explicitly disabled login exports were ignored", enabled, err)
	}
}

func TestLoginShellExportsConfirmAndRespectSessionType(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			program := shell
			if shell == "fish" {
				program = os.Getenv("VERGE_TUI_TEST_FISH")
				if program == "" {
					t.Skip("set VERGE_TUI_TEST_FISH for fish login tests")
				}
			} else if _, err := exec.LookPath(program); err != nil {
				t.Skip(shell + " unavailable")
			}
			for _, tc := range []struct {
				name                        string
				interactive, login, enabled bool
			}{
				{"login-interactive", true, true, true},
				{"login-noninteractive", false, true, true},
				{"nonlogin-interactive", true, false, true},
				{"nonlogin-noninteractive", false, false, true},
				{"disabled-login", true, true, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := store(t)
					if err := Write(s); err != nil {
						t.Fatal(err)
					}
					prefix := filepath.Dir(s.Root)
					testutil.ShellScripts(t, prefix)
					executable := filepath.Join(prefix, "verge-tui")
					c, err := s.Read()
					if err != nil {
						t.Fatal(err)
					}
					fishExports, err := ExportsForShell(c, "fish")
					if err != nil {
						t.Fatal(err)
					}
					stub := "#!/bin/sh\nfish_format=no\nfor arg do\n" +
						"    if [ \"$arg\" = --auto-login ] && [ -f \"${0%/*}/disabled\" ]; then exit 0; fi\n" +
						"    [ \"$arg\" != fish ] || fish_format=yes\ndone\n" +
						"if [ \"$fish_format\" = yes ]; then printf '%s' " + Quote(fishExports) + "; else printf '%s' " + Quote(Exports(c)) + "; fi\n"
					if err := os.WriteFile(executable, []byte(stub), 0700); err != nil {
						t.Fatal(err)
					}
					if !tc.enabled {
						if err := os.WriteFile(filepath.Join(prefix, "disabled"), nil, 0600); err != nil {
							t.Fatal(err)
						}
					}
					want := ""
					if tc.interactive && tc.login && tc.enabled {
						want = ProxyURL(c)
					}
					source := ". " + Quote(ScriptPath(executable, shell)) + " " + Quote(executable) + " " + Quote(s.Root) + " user\n"
					script := source + "[ \"${http_proxy-}\" = " + Quote(want) + " ] || exit 11\n" +
						"verge-on >/dev/null || exit 12\n" +
						"[ \"${http_proxy-}\" = " + Quote(ProxyURL(c)) + " ] || exit 13\n" +
						"verge-off >/dev/null || exit 14\n" + source +
						"[ -z \"${http_proxy-}\" ] || exit 15\nprintf 'passed\\n'\n"
					args := []string{"--noprofile", "--norc"}
					if shell == "zsh" {
						args = []string{"-d", "-f"}
					}
					if shell == "fish" {
						args = []string{"--no-config"}
						source = "source " + FishQuote(ScriptPath(executable, shell)) + " " + FishQuote(executable) + " " + FishQuote(s.Root) + " user\n"
						script = source
						if want == "" {
							script += "set -qgx http_proxy; and exit 11\n"
						} else {
							script += "test \"$http_proxy\" = " + FishQuote(want) + "; or exit 11\n"
						}
						script += "verge-on >/dev/null; or exit 12\n" +
							"test \"$http_proxy\" = " + FishQuote(ProxyURL(c)) + "; or exit 13\n" +
							"verge-off >/dev/null; or exit 14\n" + source +
							"set -qgx http_proxy; and exit 15\nprintf 'passed\\n'\n"
					}
					if tc.interactive {
						args = append(args, "-i")
					}
					if tc.login {
						args = append(args, "-l")
					}
					args = append(args, "-c", script)
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, program, args...)
					for _, entry := range os.Environ() {
						name, _, _ := strings.Cut(entry, "=")
						if name == "HOME" || name == "ZDOTDIR" || name == "BASH_ENV" || name == "ENV" || strings.HasPrefix(name, "XDG_") || name == "TERM" || slices.Contains(Variables, name) {
							continue
						}
						cmd.Env = append(cmd.Env, entry)
					}
					cmd.Env = append(cmd.Env, "HOME="+prefix, "ZDOTDIR="+prefix, "XDG_CONFIG_HOME="+filepath.Join(prefix, "config"), "XDG_DATA_HOME="+filepath.Join(prefix, "user-data"), "XDG_CACHE_HOME="+filepath.Join(prefix, "cache"), "TERM=dumb")
					var stdout, stderr bytes.Buffer
					cmd.Stdout, cmd.Stderr = &stdout, &stderr
					expected := "passed\n"
					if tc.interactive && tc.login && tc.enabled {
						expected = "Proxy environment: enabled.\n" + expected
					}
					if err := cmd.Run(); err != nil || stdout.String() != expected {
						t.Fatalf("login exports: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
					}
				})
			}
		})
	}
}

func TestLoginShellLastInstallationOwnsProxyEnvironment(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			program := shell
			if shell == "fish" {
				program = os.Getenv("VERGE_TUI_TEST_FISH")
				if program == "" {
					t.Skip("set VERGE_TUI_TEST_FISH for fish login tests")
				}
			} else if _, err := exec.LookPath(program); err != nil {
				t.Skip(shell + " unavailable")
			}
			setup := func(label string) (*state.Store, string, string) {
				t.Helper()
				s := store(t)
				if err := Write(s); err != nil {
					t.Fatal(err)
				}
				prefix := filepath.Dir(s.Root)
				testutil.ShellScripts(t, prefix)
				executable := filepath.Join(prefix, "verge-tui")
				c, err := s.Read()
				if err != nil {
					t.Fatal(err)
				}
				fishExports, err := ExportsForShell(c, "fish")
				if err != nil {
					t.Fatal(err)
				}
				stub := "#!/bin/sh\nif [ \"$3\" = proxy-status ]; then printf '%s' " + Quote(label) + "; exit 0; fi\n" +
					"fish_format=no\nfor arg do [ \"$arg\" != fish ] || fish_format=yes; done\n" +
					"if [ \"$fish_format\" = yes ]; then printf '%s' " + Quote(fishExports) + "; else printf '%s' " + Quote(Exports(c)) + "; fi\n"
				if err := os.WriteFile(executable, []byte(stub), 0700); err != nil {
					t.Fatal(err)
				}
				return s, executable, ProxyURL(c)
			}
			a, executableA, proxyA := setup("A")
			b, executableB, proxyB := setup("B")
			source := func(s *state.Store, executable string) string {
				return ". " + Quote(ScriptPath(executable, shell)) + " " + Quote(executable) + " " + Quote(s.Root) + " user\n"
			}
			script := source(a, executableA) + "[ \"${http_proxy-}\" = " + Quote(proxyA) + " ] || exit 11\n" +
				source(b, executableB) + "[ \"${http_proxy-}\" = " + Quote(proxyB) + " ] || exit 12\n" +
				"[ \"$(verge-status)\" = B ] || exit 13\nverge-off >/dev/null || exit 14\n" +
				source(b, executableB) + "[ -z \"${http_proxy-}\" ] || exit 15\nprintf 'passed\\n'\n"
			args := []string{"--noprofile", "--norc", "-i", "-l"}
			if shell == "zsh" {
				args = []string{"-d", "-f", "-i", "-l"}
			}
			if shell == "fish" {
				args = []string{"--no-config", "-i", "-l"}
				source = func(s *state.Store, executable string) string {
					return "source " + FishQuote(ScriptPath(executable, shell)) + " " + FishQuote(executable) + " " + FishQuote(s.Root) + " user\n"
				}
				script = source(a, executableA) + "test \"$http_proxy\" = " + FishQuote(proxyA) + "; or exit 11\n" +
					source(b, executableB) + "test \"$http_proxy\" = " + FishQuote(proxyB) + "; or exit 12\n" +
					"test (verge-status) = B; or exit 13\nverge-off >/dev/null; or exit 14\n" +
					source(b, executableB) + "set -qgx http_proxy; and exit 15\nprintf 'passed\\n'\n"
			}
			args = append(args, "-c", script)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, program, args...)
			for _, entry := range os.Environ() {
				name, _, _ := strings.Cut(entry, "=")
				if name == "HOME" || name == "ZDOTDIR" || name == "BASH_ENV" || name == "ENV" || strings.HasPrefix(name, "XDG_") || name == "TERM" || slices.Contains(Variables, name) {
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			prefix := filepath.Dir(a.Root)
			cmd.Env = append(cmd.Env, "HOME="+prefix, "ZDOTDIR="+prefix, "XDG_CONFIG_HOME="+filepath.Join(prefix, "config"), "XDG_DATA_HOME="+filepath.Join(prefix, "user-data"), "XDG_CACHE_HOME="+filepath.Join(prefix, "cache"), "TERM=dumb")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil || stdout.String() != "Proxy environment: enabled.\nProxy environment: enabled.\npassed\n" {
				t.Fatalf("multiple installation login exports: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
			}
		})
	}
}
