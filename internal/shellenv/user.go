package shellenv

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// The root installer delegates registration to the initiating account instead
// of writing an untrusted user rc with root permissions.
func InstallAsUser(executable, root string, uid int, shell, rc string) error {
	if os.Geteuid() != 0 {
		return errors.New("registering another user's shell requires root")
	}
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return err
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return err
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return err
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, value := range groupIDs {
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return err
		}
		groups = append(groups, uint32(n))
	}
	cmd := registrationCommand(executable, root, uint32(uid), uint32(gid), groups, u.HomeDir, u.Username, shell, rc, os.Environ())
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}

func registrationCommand(executable, root string, uid, gid uint32, groups []uint32, home, name, shell, rc string, env []string) *exec.Cmd {
	args := []string{"--data-dir", root, "shell", "install"}
	if shell != "" {
		args = append(args, "--shell", shell)
	}
	if rc != "" {
		args = append(args, "--rc-file", rc)
	}
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: groups}}
	cmd.Env = userEnvironment(env, home, name)
	return cmd
}

func userEnvironment(env []string, home, name string) []string {
	var out []string
	for _, value := range env {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "HOME", "USER", "LOGNAME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "ZDOTDIR", "SUDO_UID", "SUDO_GID", "SUDO_USER":
			continue
		}
		out = append(out, value)
	}
	return append(out, "HOME="+home, "USER="+name, "LOGNAME="+name)
}
