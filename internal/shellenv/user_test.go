package shellenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDelegatedRegistrationDoesNotInheritRootConfiguration(t *testing.T) {
	env := userEnvironment([]string{"HOME=/root", "USER=root", "XDG_CONFIG_HOME=/root/.config", "ZDOTDIR=/root/zsh", "PATH=/usr/bin", "SUDO_UID=1001"}, "/example/alice", "alice")
	all := strings.Join(env, "\n")
	if strings.Contains(all, "/root") || strings.Contains(all, "SUDO_UID=") || !strings.Contains(all, "HOME=/example/alice") || !strings.Contains(all, "PATH=/usr/bin") {
		t.Fatal(env)
	}
}

func TestRegisteredConfigReportsTheExistingCustomShell(t *testing.T) {
	s := store(t)
	if err := Write(s); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(filepath.Dir(s.Root), "custom-fish", "startup.fish")
	if _, err := RegisterShell(s, "/example/verge-tui", rc, "fish"); err != nil {
		t.Fatal(err)
	}
	path, kind, err := RegisteredConfig(s, os.Geteuid())
	if err != nil || path != rc || kind != "fish" {
		t.Fatal(path, kind, err)
	}
	if os.Geteuid() != 0 {
		if _, _, err := RegisteredConfig(s, 0); err == nil {
			t.Fatal("ordinary user inspected root registration")
		}
	}
}

func TestRegistrationCommandDropsPrivilegesBeforeEditingUserFiles(t *testing.T) {
	cmd := registrationCommand("/example/app/verge-tui", "/example/app/data", 1001, 1002, []uint32{1002, 1003}, "/example/alice", "alice", "fish", "/example/My ' fish/config.fish", []string{"HOME=/root", "XDG_CONFIG_HOME=/root/.config"})
	credential := cmd.SysProcAttr.Credential
	if credential.Uid != 1001 || credential.Gid != 1002 || !reflect.DeepEqual(credential.Groups, []uint32{1002, 1003}) {
		t.Fatal("registration retained root credentials", credential)
	}
	want := []string{"/example/app/verge-tui", "--data-dir", "/example/app/data", "shell", "install", "--shell", "fish", "--rc-file", "/example/My ' fish/config.fish"}
	if !reflect.DeepEqual(cmd.Args, want) || strings.Contains(strings.Join(cmd.Env, "\n"), "/root") {
		t.Fatal("registration lost target parameters or inherited root configuration", cmd.Args, cmd.Env)
	}
}
