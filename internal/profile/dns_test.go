package profile

import (
	"os"
	"testing"

	"verge-tui/internal/state"
)

func TestDNSListenFollowsInstallationModeAndOverwrite(t *testing.T) {
	for _, mode := range []string{"system", "user", "", "unknown"} {
		for _, tc := range []struct {
			name, profileListen, overwriteListen, want string
			overwrite                                  bool
		}{
			{"inherit-profile", "0.0.0.0:1053", ":53", "0.0.0.0:1053", false},
			{"overwrite-profile", "0.0.0.0:1053", "127.0.0.1:2053", "127.0.0.1:2053", true},
			{"trim-overwrite", "0.0.0.0:1053", "  [::1]:2053  ", "[::1]:2053", true},
			{"empty-overwrite-inherits", "0.0.0.0:1053", "", "0.0.0.0:1053", true},
			{"blank-overwrite-inherits", "0.0.0.0:1053", " \t ", "0.0.0.0:1053", true},
			{"no-listener", "", ":53", "", false},
			{"empty-overwrite-keeps-no-listener", "", "", "", true},
			{"overwrite-adds-listener", "", ":53", ":53", true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				c := state.Settings{InstallMode: mode, MixedPort: 23456, APIPort: 23457, Secret: "private-controller", ControllerSocket: "/managed/runtime/api.sock", ProxyUser: "private", ProxyPassword: "password", Current: "first", Mode: "rule", Tun: true}
				override := state.DefaultDNS()
				override.Enabled, override.Listen = tc.overwrite, tc.overwriteListen
				c.Engine.DNS = map[string]state.DNSSettings{"first": override}
				doc, err := Parse([]byte(DefaultConfig + "dns: {enable: true, nameserver: [8.8.8.8], nameserver-policy: {example.com: 9.9.9.9}}\nexternal-controller: '0.0.0.0:9090'\ntun: {enable: true}\n"))
				if err != nil {
					t.Fatal(err)
				}
				if tc.profileListen != "" {
					doc["dns"].(map[string]any)["listen"] = tc.profileListen
				}
				payload, err := RuntimeDocument(c, doc)
				if err != nil {
					t.Fatal(err)
				}
				result, err := Parse(payload)
				if err != nil {
					t.Fatal(err)
				}
				dns := result["dns"].(map[string]any)
				want := tc.want
				if mode != "system" {
					want = ""
				}
				if got, present := dns["listen"]; want == "" && present || want != "" && got != want {
					t.Fatalf("DNS listen = %#v (present=%t), want %q", got, present, want)
				}
				if dns["nameserver-policy"] == nil || result["external-controller"] != "" || result["external-controller-unix"] != c.ControllerSocket || result["secret"] != c.Secret {
					t.Fatal("DNS listener handling changed DNS policy or protected controller")
				}
				if got := result["tun"].(map[string]any)["enable"]; got != (mode == "system") {
					t.Fatalf("DNS listener handling changed TUN policy: %v", got)
				}
				if mode != "system" {
					auth := result["authentication"].([]any)
					if len(auth) != 1 || auth[0] != "private:password" {
						t.Fatal("DNS listener handling changed user proxy authentication")
					}
				}
			})
		}
	}
}

func TestDNSListenSelectionKeepsProfilesAndSourceFilesIndependent(t *testing.T) {
	for _, kind := range []string{"local", "remote"} {
		t.Run(kind, func(t *testing.T) {
			s, err := state.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			source := []byte(DefaultConfig + "dns: {enable: true, listen: '0.0.0.0:1053'}\n")
			first, err := Add(s, state.Profile{Type: kind}, source)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Add(s, state.Profile{Type: kind}, source)
			if err != nil {
				t.Fatal(err)
			}
			c, err := s.Read()
			if err != nil {
				t.Fatal(err)
			}
			override := state.DefaultDNS()
			if override.Listen != ":53" || override.Enabled {
				t.Fatal("default DNS settings do not match the disabled Verge overwrite with :53")
			}
			override.Enabled, override.Listen = true, "127.0.0.1:2053"
			c.Engine.DNS = map[string]state.DNSSettings{first.ID: override}
			for _, mode := range []string{"system", "user"} {
				c.InstallMode = mode
				for _, p := range []state.Profile{first, second, first} {
					c.Current = p.ID
					payload, err := Runtime(s, c, p.ID)
					if err != nil {
						t.Fatal(err)
					}
					doc, err := Parse(payload)
					if err != nil {
						t.Fatal(err)
					}
					listen := doc["dns"].(map[string]any)["listen"]
					var want any
					if mode == "system" {
						want = "0.0.0.0:1053"
						if p.ID == first.ID {
							want = override.Listen
						}
					}
					if listen != want {
						t.Fatalf("%s profile %s listen = %#v, want %#v", mode, p.ID, listen, want)
					}
					path, _ := s.ProfilePath(p.ID)
					if original, err := os.ReadFile(path); err != nil || string(original) != string(source) {
						t.Fatal("runtime DNS listener handling changed the source profile", err)
					}
				}
			}
		})
	}
}
