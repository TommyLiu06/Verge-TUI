package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestEnvAutoLoginUsesSavedPreferenceWithoutChangingManualExports(t *testing.T) {
	s, err := state.Open(testutil.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := shellenv.Write(s); err != nil {
		t.Fatal(err)
	}
	capture := func(args ...string) string {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		previous := os.Stdout
		os.Stdout = w
		err = run(append([]string{"--data-dir", s.Root, "env"}, args...))
		os.Stdout = previous
		w.Close()
		data, readErr := io.ReadAll(r)
		r.Close()
		if err != nil || readErr != nil {
			t.Fatal(err, readErr)
		}
		return string(data)
	}
	if got := capture("--auto-login"); !strings.Contains(got, "export http_proxy=") {
		t.Fatal("default login did not export proxy")
	}
	p := state.DefaultPreferences()
	p.ProxyOnLogin = false
	if err := state.WritePreferences(s.Root, p); err != nil {
		t.Fatal(err)
	}
	if got := capture("--auto-login"); got != "" {
		t.Fatal("disabled login emitted exports")
	}
	if got := capture(); !strings.Contains(got, "export http_proxy=") {
		t.Fatal("login preference disabled manual proxy")
	}
	if got := capture("--shell", "fish"); !strings.Contains(got, "set -gx http_proxy ") {
		t.Fatal("manual fish exports missing")
	}
}
