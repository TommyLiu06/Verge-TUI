package app

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

func dnsListenBackend(t *testing.T, mode string) *Backend {
	t.Helper()
	b := settingsBackend(t)
	p, err := profile.Add(b.Store, state.Profile{Type: "local"}, []byte(profile.DefaultConfig+"dns: {enable: true, listen: '127.0.0.1:1053', nameserver: [8.8.8.8]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Store.Update(func(c *state.Settings) error {
		c.InstallMode, c.Current = mode, p.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

func dnsListenFields(listen string) map[string]string {
	return map[string]string{
		"listen": listen, "mode": "fake-ip", "ipv6": "false",
		"nameservers": "1.1.1.1", "default-nameservers": "223.5.5.5",
		"fallback": "", "fake-ip-range": "198.18.0.1/16",
	}
}

func dnsRuntimeListen(t *testing.T, b *Backend) any {
	t.Helper()
	c, err := b.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	data, err := profile.Runtime(b.Store, c, c.Current)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := profile.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc["dns"].(map[string]any)["listen"]
}

func TestSystemDNSListenAcceptsLowHighAndInheritedAddresses(t *testing.T) {
	b := dnsListenBackend(t, "system")
	c, _ := b.Store.Read()
	for _, address := range []string{":53", "0.0.0.0:1053", "127.0.0.1:65535", "[::1]:1053", " 127.0.0.1:2053 \n", " \t\n"} {
		t.Run(strings.TrimSpace(address), func(t *testing.T) {
			if err := b.Execute(context.Background(), Action{Kind: "settings-save", ID: "dns", Node: c.Current, Fields: dnsListenFields(address)}); err != nil {
				t.Fatal(err)
			}
			after, err := b.Store.Read()
			if err != nil {
				t.Fatal(err)
			}
			want := strings.TrimSpace(address)
			if got := after.Engine.DNS[c.Current].Listen; got != want {
				t.Fatalf("saved DNS listen %q, want %q", got, want)
			}
			if want == "" {
				want = "127.0.0.1:1053"
			}
			if got := dnsRuntimeListen(t, b); got != want {
				t.Fatalf("runtime DNS listen %v, want %q", got, want)
			}
		})
	}
}

func TestSystemDNSListenRejectsInvalidAddressesWithoutSaving(t *testing.T) {
	b := dnsListenBackend(t, "system")
	before, _ := b.Store.Read()
	for _, address := range []string{"127.0.0.1", "1053", "127.0.0.1:0", ":65536", ":-1", ":dns", "[::1]", "::1:1053", "127.0.0.1:"} {
		t.Run(address, func(t *testing.T) {
			err := b.Execute(context.Background(), Action{Kind: "settings-save", ID: "dns", Node: before.Current, Fields: dnsListenFields(address)})
			if err == nil {
				t.Fatal("invalid DNS listen address was accepted")
			}
			after, readErr := b.Store.Read()
			if readErr != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected DNS listen modified settings", readErr)
			}
		})
	}
}

func TestSystemDNSListenIsProfileScopedAndDisablingRestoresSource(t *testing.T) {
	b := dnsListenBackend(t, "system")
	ctx := context.Background()
	first, _ := b.Store.Read()
	path, _ := b.Store.ProfilePath(first.Current)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := profile.Add(b.Store, state.Profile{Type: "local"}, []byte(profile.DefaultConfig+"dns: {enable: true, listen: '127.0.0.1:3053', nameserver: [9.9.9.9]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns", Node: first.Current, Fields: dnsListenFields("127.0.0.1:2053")}); err != nil {
		t.Fatal(err)
	}
	fields := dnsListenFields("")
	delete(fields, "listen")
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns", Node: first.Current, Fields: fields}); err != nil {
		t.Fatal(err)
	}
	if got := dnsRuntimeListen(t, b); got != "127.0.0.1:2053" {
		t.Fatal("saving other DNS fields changed the existing listen address", got)
	}
	if err := b.Execute(ctx, Action{Kind: "profile-select", ID: second.ID}); err != nil {
		t.Fatal(err)
	}
	if got := dnsRuntimeListen(t, b); got != "127.0.0.1:3053" {
		t.Fatal("DNS listen override leaked into another profile", got)
	}
	before, _ := b.Store.Read()
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns", Node: first.Current, Fields: dnsListenFields(":53")}); err == nil {
		t.Fatal("stale DNS form updated another active profile")
	}
	after, _ := b.Store.Read()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("stale DNS form modified saved settings")
	}
	if err := b.Execute(ctx, Action{Kind: "profile-select", ID: first.Current}); err != nil {
		t.Fatal(err)
	}
	if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns-toggle", Node: first.Current}); err != nil {
		t.Fatal(err)
	}
	if got := dnsRuntimeListen(t, b); got != "127.0.0.1:1053" {
		t.Fatal("disabling DNS overwrite did not restore the source listen address", got)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(original) {
		t.Fatal("DNS settings modified the original profile", err)
	}
}

func TestDNSListenDefaultsAndUserModeRestriction(t *testing.T) {
	for _, mode := range []string{"user", "system"} {
		t.Run(mode, func(t *testing.T) {
			b := dnsListenBackend(t, mode)
			ctx := context.Background()
			c, _ := b.Store.Read()
			if mode == "user" {
				for _, address := range []string{"", "127.0.0.1:2053"} {
					err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns", Node: c.Current, Fields: dnsListenFields(address)})
					if err == nil || !strings.Contains(err.Error(), "system installation") {
						t.Fatal("user mode accepted a DNS listen setting", err)
					}
					after, _ := b.Store.Read()
					if !reflect.DeepEqual(c, after) {
						t.Fatal("rejected user DNS listener changed settings")
					}
				}
			}
			if err := b.Execute(ctx, Action{Kind: "settings-save", ID: "dns-toggle", Node: c.Current}); err != nil {
				t.Fatal(err)
			}
			after, _ := b.Store.Read()
			if after.Engine.DNS[c.Current].Listen != ":53" {
				t.Fatal("new DNS overwrite did not retain the upstream listen default")
			}
			got := dnsRuntimeListen(t, b)
			if mode == "system" && got != ":53" || mode == "user" && got != nil {
				t.Fatal("DNS runtime listener did not follow the installation mode", got)
			}
		})
	}
}
