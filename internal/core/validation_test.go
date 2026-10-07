package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mihomo-tui/internal/state"
)

func TestValidationTimeoutIsNotReportedAsInvalidProfile(t *testing.T) {
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root, "cores", "slow")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "mihomo"), []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	c.Core = "slow"
	m := &Manager{Store: s}
	start := time.Now()
	err = m.validate(context.Background(), c, []byte("proxies: []\n"), 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "validation timed out") || strings.Contains(err.Error(), "rejected profile") {
		t.Fatalf("incorrect timeout error: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("timed-out validation did not finish promptly")
	}
	files, err := filepath.Glob(filepath.Join(s.Root, "runtime", "check-*.yaml"))
	if err != nil || len(files) != 0 {
		t.Fatal("validation left temporary credentials on disk")
	}
}
