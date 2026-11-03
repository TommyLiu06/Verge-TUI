package main

import (
	"os"
	"path/filepath"
	"testing"

	"verge-tui/internal/installation"
	"verge-tui/internal/state"
	"verge-tui/internal/updater"
)

func TestUpdateConfigureBooleanFlagsAndDependentPolicies(t *testing.T) {
	prefix := t.TempDir()
	s, err := state.Open(filepath.Join(prefix, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "verge-tui"), []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := installation.Record(prefix, version, ""); err != nil {
		t.Fatal(err)
	}
	u := updater.Updater{Store: s, Version: version}
	invoke := func(args ...string) error {
		return run(append([]string{"--data-dir", s.Root, "update", "configure"}, args...))
	}
	if err := invoke("--check-minutes=60", "--auto-download", "--idle-minutes=10"); err != nil {
		t.Fatal(err)
	}
	options, err := u.Options()
	if err != nil || !options.UpdatesEnabled() {
		t.Fatal("CLI did not enable the complete dependency chain", options, err)
	}
	if err := invoke("--auto-download=false"); err != nil {
		t.Fatal(err)
	}
	options, err = u.Options()
	if err != nil || options.CheckMinutes != 60 || options.AutoDownload || options.IdleMinutes != 0 {
		t.Fatal("explicit false did not disable installation too", options, err)
	}
	if err := invoke("--idle-minutes=5"); err == nil {
		t.Fatal("CLI enabled installation without downloads")
	}
	if err := invoke("--auto-download=true", "--idle-minutes=5"); err != nil {
		t.Fatal(err)
	}
	if err := invoke("--check-minutes=0"); err != nil {
		t.Fatal(err)
	}
	options, err = u.Options()
	if err != nil || options.DownloadsEnabled() || options.IdleMinutes != 0 {
		t.Fatal("disabling checking did not cascade", options, err)
	}
}
