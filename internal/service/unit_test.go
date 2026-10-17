package service

import (
	"strings"
	"testing"
)

func TestServiceEscapingAndPrivileges(t *testing.T) {
	unit, err := Unit("/opt/My App/verge-tui", "/srv/proxy%data", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `"/opt/My App/verge-tui" --data-dir "/srv/proxy%%data" run`) || strings.Contains(unit, "CAP_NET_ADMIN") {
		t.Fatal("unsafe user service")
	}
	if strings.Contains(unit, "CapabilityBoundingSet") {
		t.Fatal("user service must not set CapabilityBoundingSet; the user manager cannot apply it (service dies with 218/CAPABILITIES)")
	}
	unit, err = Unit("/opt/app/verge-tui", "/srv/proxy", "system")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, "CAP_NET_ADMIN") || !strings.Contains(unit, "CapabilityBoundingSet") || !strings.Contains(unit, "multi-user.target") {
		t.Fatal("system service lacks TUN privileges")
	}
	if _, err := Unit("/bin/a\nExecStart=/bin/b", "/srv/a", "system"); err == nil {
		t.Fatal("accepted unit injection")
	}
	unit, err = Unit("/opt/${APP}/verge-tui", "/srv/${DATA}%proxy", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart=:"/opt/${APP}/verge-tui" --data-dir "/srv/${DATA}%%proxy" run`) || !strings.Contains(unit, `ReadWritePaths="/srv/${DATA}%%proxy"`) {
		t.Fatal("service paths permit variable expansion")
	}
}
