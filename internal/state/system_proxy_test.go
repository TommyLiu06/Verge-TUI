package state

import (
	"reflect"
	"testing"
)

func TestSystemProxyAcceptsSupportedHostsAndBypassFormats(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "localhost", "proxy.example.com"} {
		p := DefaultSystemProxy()
		p.Host, p.UseDefaultBypass = host, false
		p.Bypass = "localhost, .example.com; *.example.org\n10.0.0.0/8, fe80::/10, ::1, [::1]:443, example.com:80"
		if err := p.Validate(); err != nil {
			t.Fatal(host, err)
		}
		if len(p.IgnoreHosts()) != 8 {
			t.Fatal("bypass parsing dropped entries")
		}
	}
	if !reflect.DeepEqual(DefaultSystemProxy().IgnoreHosts(), DefaultProxyBypass()) {
		t.Fatal("default bypass differs from Verge's Linux list")
	}
	p := DefaultSystemProxy()
	p.UseDefaultBypass = false
	if len(p.IgnoreHosts()) != 0 {
		t.Fatal("an empty custom list did not clear exclusions")
	}
}

func TestSystemProxyRejectsInvalidHostsAndBypass(t *testing.T) {
	for _, host := range []string{"", "http://127.0.0.1", "localhost:7890", "[::1]", "bad'host", "bad\nhost", "fe80::1%bad\nzone", "-bad.example"} {
		p := DefaultSystemProxy()
		p.Host = host
		if p.Validate() == nil {
			t.Fatal("accepted invalid host", host)
		}
	}
	for _, bypass := range []string{"*example.com", "192.168.*", "http://example.com", "10.0.0.0/99", "example.com:70000", "example.com:no", "bad'host"} {
		p := DefaultSystemProxy()
		p.UseDefaultBypass, p.Bypass = false, bypass
		if p.Validate() == nil {
			t.Fatal("accepted invalid bypass", bypass)
		}
	}
}
