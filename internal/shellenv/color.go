package shellenv

import (
	"os"
	"strings"

	"golang.org/x/term"
)

func TerminalColor(file *os.File) bool {
	_, disabled := os.LookupEnv("NO_COLOR")
	return !disabled && os.Getenv("TERM") != "dumb" && term.IsTerminal(int(file.Fd()))
}

// Keep core availability and shell environment state independent: a running
// proxy does not mean the calling shell has loaded its environment variables.
func ColorStatus(output string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(output, "\n") {
		text := strings.TrimSuffix(line, "\n")
		colored, good := false, false
		switch {
		case strings.HasPrefix(text, "Core:"):
			colored, good = true, strings.HasPrefix(text, "Core: running")
		case strings.HasPrefix(text, "Proxy listener:"):
			colored, good = true, text == "Proxy listener: listening"
		case strings.HasPrefix(text, "Shell environment:"):
			colored, good = true, text == "Shell environment: loaded and matches this installation"
		case strings.HasPrefix(text, "TUN:"):
			colored, good = true, text == "TUN: true"
		case strings.HasPrefix(text, "  "):
			colored, good = true, strings.HasSuffix(text, ": matches")
		}
		if colored {
			code := "31"
			if good {
				code = "32"
			}
			out.WriteString("\x1b[" + code + "m" + text + "\x1b[0m")
			out.WriteString(strings.TrimPrefix(line, text))
		} else {
			out.WriteString(line)
		}
	}
	return out.String()
}
