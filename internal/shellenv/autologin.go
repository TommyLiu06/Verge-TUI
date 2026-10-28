package shellenv

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"verge-tui/internal/state"
)

// Match the TUI's private preferences for the installation owner, or the
// calling user's preferences when using a shared system installation.
func AutoLoginEnabled(s *state.Store) (bool, error) {
	info, err := os.Stat(s.Root)
	if err != nil {
		return false, err
	}
	root := s.Root
	if int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		root = filepath.Join(state.PreferencesUsersPath(s), strconv.Itoa(os.Geteuid()))
	}
	p, err := state.ReadPreferences(root)
	return p.ProxyOnLogin, err
}
