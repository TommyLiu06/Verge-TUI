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
		value, good := "", false
		switch {
		case strings.HasPrefix(text, "Core:"):
			switch {
			case strings.HasPrefix(text, "Core: running"):
				value, good = "running", true
			case strings.Contains(text, "unavailable"):
				value = "unavailable"
			case strings.Contains(text, "reachable"):
				value, good = "reachable", true
			}
		case strings.HasPrefix(text, "Proxy listener:"):
			value = strings.TrimPrefix(text, "Proxy listener: ")
			good = value == "listening"
		case strings.HasPrefix(text, "Shell environment:"):
			if strings.Contains(text, "not loaded") {
				value = "not loaded"
			} else if strings.Contains(text, "partial or different") {
				value = "partial"
			} else {
				value, good = "loaded", true
			}
		case strings.HasPrefix(text, "TUN:"):
			value = strings.TrimPrefix(text, "TUN: ")
			good = value == "true"
		case strings.HasPrefix(text, "  "):
			if _, after, found := strings.Cut(text, ": "); found {
				value = strings.SplitN(after, " ", 2)[0]
				good = value == "matches"
			}
		}
		if value != "" {
			code := "31"
			if good {
				code = "32"
			}
			painted := strings.Replace(text, value, "\x1b["+code+"m"+value+"\x1b[0m", 1)
			if strings.HasPrefix(text, "Core:") {
				painted = strings.Replace(painted, "failed", "\x1b[31mfailed\x1b[0m", 1)
			}
			if strings.HasPrefix(text, "Shell environment:") {
				painted = strings.Replace(painted, "matches", "\x1b[32mmatches\x1b[0m", 1)
				painted = strings.Replace(painted, "different", "\x1b[31mdifferent\x1b[0m", 1)
			}
			out.WriteString(painted)
			out.WriteString(strings.TrimPrefix(line, text))
		} else {
			out.WriteString(line)
		}
	}
	return out.String()
}
