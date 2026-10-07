package service

import (
	"strings"
	"testing"
)

func TestServiceEscapingAndPrivileges(t *testing.T) {
	unit, err := Unit("/opt/My App/mihomo-tui", "/srv/proxy%data", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `"/opt/My App/mihomo-tui" --data-dir "/srv/proxy%%data" run`) || strings.Contains(unit, "CAP_NET_ADMIN") {
		t.Fatal("unsafe user service")
	}
	unit, err = Unit("/opt/app/mihomo-tui", "/srv/proxy", "system")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, "CAP_NET_ADMIN") || !strings.Contains(unit, "multi-user.target") {
		t.Fatal("system service lacks TUN privileges")
	}
	if _, err := Unit("/bin/a\nExecStart=/bin/b", "/srv/a", "system"); err == nil {
		t.Fatal("accepted unit injection")
	}
}
