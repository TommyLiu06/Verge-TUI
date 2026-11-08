package shellenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"verge-tui/internal/state"
)

// The primary entry remains at the top level for installer discovery.
type registrationDocument struct {
	registration
	Additional []registration `json:"additional,omitempty"`
}

func loadRegistrations(s *state.Store, path string) ([]registration, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc registrationDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	regs := append([]registration{doc.registration}, doc.Additional...)
	if err := validateRegistrations(s, regs); err != nil {
		return nil, err
	}
	return regs, nil
}

func registrationShell(reg registration) string {
	if reg.Shell != "" {
		return NormalizeShell(reg.Shell)
	}
	return inferShell(reg.RC)
}

func validateRegistrations(s *state.Store, regs []registration) error {
	shells, paths := map[string]bool{}, map[string]bool{}
	for _, reg := range regs {
		shell := registrationShell(reg)
		if reg.ID != blockID(s) || !Supported(shell) || shells[shell] || !validRCPath(reg.RC) {
			return errors.New("invalid or duplicate shell registration")
		}
		shells[shell] = true
		owned := append([]string{reg.RC}, reg.Files...)
		owned = append(owned, reg.ExtraRCs...)
		for _, path := range owned {
			if !validRCPath(path) || paths[path] {
				return errors.New("shell registrations contain an invalid or shared startup path")
			}
			paths[path] = true
		}
	}
	return nil
}

func saveRegistrations(s *state.Store, path string, regs []registration) error {
	if len(regs) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := validateRegistrations(s, regs); err != nil {
		return err
	}
	data, err := json.Marshal(registrationDocument{registration: regs[0], Additional: regs[1:]})
	if err != nil {
		return err
	}
	return state.Write(path, data, 0600)
}

func registrationLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		f.Close()
		return nil, errors.New("invalid shell registration lock")
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func registerShellAt(s *state.Store, executable, rc, shell, metaPath string) (string, error) {
	lock, err := registrationLock(metaPath)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	regs, err := loadRegistrations(s, metaPath)
	if err != nil {
		return "", err
	}
	return registerInSet(s, executable, rc, shell, metaPath, &regs)
}

func registerInSet(s *state.Store, executable, rc, shell, metaPath string, regs *[]registration) (string, error) {
	shell = NormalizeShell(shell)
	resolved := rc
	if shell != "fish" {
		var err error
		resolved, err = resolveRCPath(rc)
		if err != nil {
			return "", err
		}
	}
	index := len(*regs)
	var old registration
	for i, reg := range *regs {
		if registrationShell(reg) == shell {
			if reg.RC != resolved {
				return "", errors.New("this shell is already registered in another rc file; remove its registration before changing it")
			}
			index, old = i, reg
		}
	}
	return registerOne(s, executable, rc, shell, old, func(reg registration) error {
		next := append([]registration(nil), (*regs)...)
		if index == len(next) {
			next = append(next, reg)
		} else {
			next[index] = reg
		}
		if err := saveRegistrations(s, metaPath, next); err != nil {
			return err
		}
		*regs = next
		return nil
	})
}

func RegisteredRCForShell(s *state.Store, shell string) (string, error) {
	regs, err := loadRegistrations(s, registrationPath(s))
	if err != nil {
		return "", err
	}
	for _, reg := range regs {
		if registrationShell(reg) == NormalizeShell(shell) {
			return reg.RC, nil
		}
	}
	return "", nil
}

func RefreshRegistrations(s *state.Store, executable string) error {
	path := registrationPath(s)
	lock, err := registrationLock(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	return refreshRegistrations(s, executable, path, "")
}

func refreshRegistrations(s *state.Store, executable, path, rc string) error {
	regs, err := loadRegistrations(s, path)
	if err != nil {
		return err
	}
	if len(regs) == 0 && rc != "" {
		_, err := registerInSet(s, executable, rc, inferShell(rc), path, &regs)
		return err
	}
	for _, reg := range append([]registration(nil), regs...) {
		if _, err := registerInSet(s, executable, reg.RC, registrationShell(reg), path, &regs); err != nil {
			return err
		}
	}
	return nil
}

// Hold this lock through update snapshots, refresh and any rollback so another
// shell cannot add an entry that the transaction has not backed up.
type RegistrationUpdate struct {
	Paths            []string
	store            *state.Store
	executable, path string
	lock             *os.File
}

func BeginRegistrationUpdate(s *state.Store, executable string) (*RegistrationUpdate, error) {
	path := registrationPath(s)
	lock, err := registrationLock(path)
	if err != nil {
		return nil, err
	}
	paths, err := RegistrationPaths(s, executable)
	if err != nil {
		lock.Close()
		return nil, err
	}
	return &RegistrationUpdate{Paths: paths, store: s, executable: executable, path: path, lock: lock}, nil
}

func (u *RegistrationUpdate) Refresh(rc string) error {
	return refreshRegistrations(u.store, u.executable, u.path, rc)
}

func (u *RegistrationUpdate) Close() error { return u.lock.Close() }

// Include both existing and prospective login files in update recovery.
func RegistrationPaths(s *state.Store, executable string) ([]string, error) {
	path := registrationPath(s)
	regs, err := loadRegistrations(s, path)
	if err != nil {
		return nil, err
	}
	paths := []string{path}
	for _, reg := range regs {
		managed, err := ManagedPaths(s, executable, reg.RC, registrationShell(reg))
		if err != nil {
			return nil, err
		}
		for _, item := range append([]string{reg.RC}, managed...) {
			if !containsPath(paths, item) {
				paths = append(paths, item)
			}
		}
	}
	return paths, nil
}

func removeAt(s *state.Store, metaPath string) error {
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	lock, err := registrationLock(metaPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	regs, err := loadRegistrations(s, metaPath)
	if err != nil {
		return err
	}
	for _, reg := range regs {
		if err := validateRemoval(reg); err != nil {
			return err
		}
	}
	for len(regs) > 0 {
		if err := removeOne(s, regs[0]); err != nil {
			return err
		}
		regs = regs[1:]
		if err := saveRegistrations(s, metaPath, regs); err != nil {
			return err
		}
	}
	return nil
}

func validateRemoval(reg registration) error {
	if _, err := prepareExtraRCRemoval(reg); err != nil {
		return err
	}
	if _, err := prepareRCChange(reg.RC, reg.ID, ""); err != nil {
		return err
	}
	for _, path := range reg.Files {
		if err := validateManagedFile(path, reg.ID); err != nil {
			return err
		}
	}
	return nil
}

func registrationLockEntry(name string, info os.FileInfo) bool {
	uid := int(info.Sys().(*syscall.Stat_t).Uid)
	return info.Mode().IsRegular() && name == strconv.Itoa(uid)+".json.lock" && strings.HasSuffix(name, ".lock")
}
