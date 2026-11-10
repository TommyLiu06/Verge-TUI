package tui

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"verge-tui/internal/app"
	"verge-tui/internal/state"
)

func TestDNSListenFormIsOnlyAvailableInSystemMode(t *testing.T) {
	for _, mode := range []string{"user", "system"} {
		t.Run(mode, func(t *testing.T) {
			v := fixture()
			v.Settings.InstallMode = mode
			m := New(v)
			m.Activate(app.Action{Kind: "settings-form", ID: "dns"})
			var listen *Field
			for i := range m.Modal.Fields {
				if m.Modal.Fields[i].Key == "listen" {
					listen = &m.Modal.Fields[i]
				}
			}
			if mode == "system" {
				if listen == nil || listen.Label != "DNS Listen" || listen.Text != ":53" {
					t.Fatal("system DNS form did not expose the upstream listen default")
				}
				m.Render(100, 30)
				if m.Focus != Key(app.Action{Kind: "input", ID: "listen"}) {
					t.Fatal("DNS listen field was not keyboard accessible")
				}
				listen.Text = "127.0.0.1:1053"
			} else if listen != nil {
				t.Fatal("user mode exposed an ineffective DNS listen field")
			}
			a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
			if a == nil || a.Kind != "settings-save" || a.ID != "dns" || a.Node != v.Settings.Current {
				t.Fatal("DNS form did not save to its active profile")
			}
			value, exists := a.Fields["listen"]
			if mode == "system" && (!exists || value != "127.0.0.1:1053") || mode == "user" && exists {
				t.Fatal("saved DNS listen field did not follow the installation mode", value, exists)
			}
		})
	}
}

func TestDNSListenFormPreservesBlankInheritanceAndSavedAddress(t *testing.T) {
	for _, address := range []string{"", "127.0.0.1:1053"} {
		t.Run(address, func(t *testing.T) {
			v := fixture()
			v.Settings.InstallMode = "system"
			dns := state.DefaultDNS()
			dns.Listen = address
			v.Settings.Engine.DNS = map[string]state.DNSSettings{v.Settings.Current: dns}
			m := New(v)
			m.Activate(app.Action{Kind: "settings-form", ID: "dns"})
			found := false
			for _, field := range m.Modal.Fields {
				if field.Key == "listen" {
					found = true
					if field.Text != address {
						t.Fatalf("DNS form replaced saved listen %q with %q", address, field.Text)
					}
				}
			}
			if !found {
				t.Fatal("saved DNS listen was not editable")
			}
			a := m.Handle(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone))
			if a == nil || a.Fields["listen"] != address {
				t.Fatal("saving the DNS form replaced the saved listen value")
			}
		})
	}
}
