package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpCoversSubcommandsWithoutOpeningData(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "must-not-create")
	for topic := range helpTopics {
		for _, prefix := range []bool{false, true} {
			args := []string{"--data-dir", absent}
			if prefix {
				args = append(args, "help")
			}
			args = append(args, strings.Fields(topic)...)
			if !prefix {
				args = append(args, "--help")
			}
			var output bytes.Buffer
			handled, err := printHelp(args, &output)
			if !handled || err != nil || !strings.Contains(output.String(), "Usage:") {
				t.Fatalf("help failed for %q: %v", topic, err)
			}
		}
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--data-dir", absent, "shell", "install", "--rc-file", absent, "--help"}, {"preview", "--page", "settings", "--help"}} {
		if err := run(args); err != nil {
			t.Fatal("help did not exit successfully", args, err)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatal("help initialized data")
	}
	var output bytes.Buffer
	handled, err := printHelp([]string{"profile", "list"}, &output)
	if handled || err != nil || output.Len() != 0 {
		t.Fatal("normal command treated as help")
	}
}
