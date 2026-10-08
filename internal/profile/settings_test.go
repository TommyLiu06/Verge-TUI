package profile

import (
	"strings"
	"testing"

	"verge-tui/internal/state"
)

func TestSettingsOverridesKeepUserAuthenticationAndLoopbackAPI(t *testing.T) {
	on, off := true, false
	c := state.Settings{InstallMode: "user", MixedPort: 23456, HTTPPort: 23457, SOCKSPort: 23458, APIPort: 23459, Secret: "api-key", ProxyUser: "private-user", ProxyPassword: "private-password", Mode: "rule", Engine: state.EngineSettings{AllowLAN: true, IPv6: &off, UnifiedDelay: &on, LogLevel: "error"}}
	data, err := RuntimeData(c, []byte(DefaultConfig+"ipv6: true\nunified-delay: false\nallow-lan: false\nexternal-controller: '0.0.0.0:9090'\nauthentication: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc["allow-lan"] != true || doc["bind-address"] != "*" || doc["ipv6"] != false || doc["unified-delay"] != true || doc["log-level"] != "error" {
		t.Fatal("core settings were not applied")
	}
	if doc["port"] != 23457 || doc["socks-port"] != 23458 || doc["external-controller"] != "127.0.0.1:23459" || doc["secret"] != "api-key" {
		t.Fatal("optional ports changed protected controller binding")
	}
	auth := doc["authentication"].([]any)
	if len(auth) != 1 || auth[0] != "private-user:private-password" || len(doc["skip-auth-prefixes"].([]any)) != 0 {
		t.Fatal("LAN or optional listeners bypassed user authentication")
	}
	if strings.Contains(string(data), "0.0.0.0:9090") {
		t.Fatal("profile controller override survived")
	}
}
