package tui

import (
	"io"
	"sync"

	"github.com/gdamore/tcell/v3"
)

// Xterm alternate-scroll mode synthesizes arrow keys from the wheel. Save and
// disable it while the TUI handles actual mouse reports, then restore it before
// tcell closes the terminal. Terminals that lack this mode ignore the request.
func prepareWheel(screen tcell.Screen) (func() error, error) {
	tty, ok := screen.Tty()
	if !ok {
		return func() error { return nil }, nil
	}
	write := func(sequence string) error {
		// Use the screen's output lock when available, so capability responses
		// and redraws cannot interleave with these terminal control sequences.
		if locker, ok := screen.(sync.Locker); ok {
			locker.Lock()
			defer locker.Unlock()
		}
		_, err := io.WriteString(tty, sequence)
		return err
	}
	return func() error { return write("\x1b[?1007r") }, write("\x1b[?1007s\x1b[?1007l")
}
