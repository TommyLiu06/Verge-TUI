package state

import (
	"errors"
	"os"
	"path/filepath"
)

func SharedPath(s *Store) string       { return filepath.Join(s.Root, "shared") }
func ShellUsersPath(s *Store) string   { return filepath.Join(SharedPath(s), "shell-users") }
func DesktopUsersPath(s *Store) string { return filepath.Join(SharedPath(s), "desktop-users") }

// System settings and profiles stay private. Search permission permits access
// only to the deliberately shared socket and per-user shell registrations.
func PrepareShared(s *Store) error {
	if err := os.Chmod(s.Root, 0711); err != nil {
		return err
	}
	for _, dir := range []string{SharedPath(s), ShellUsersPath(s), DesktopUsersPath(s)} {
		if info, err := os.Lstat(dir); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("shared installation paths must be real directories")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if err := os.Chmod(SharedPath(s), 0755); err != nil {
		return err
	}
	for _, dir := range []string{ShellUsersPath(s), DesktopUsersPath(s)} {
		if err := os.Chmod(dir, os.ModeSticky|0777); err != nil {
			return err
		}
	}
	return nil
}
