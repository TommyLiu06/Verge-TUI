package shellenv

import (
	"strings"
	"testing"
)

func TestStatusColorsKeepListenerAndEnvironmentIndependent(t *testing.T) {
	plain := "Installation: user\nCore: running (test)\nProxy listener: listening\nShell environment: not loaded\n  http_proxy: unset\nTUN: true\nTUN: false\n"
	got := ColorStatus(plain)
	for _, expected := range []string{
		"Installation: user\n",
		"Core: \x1b[32mrunning\x1b[0m (test)\n",
		"Proxy listener: \x1b[32mlistening\x1b[0m\n",
		"Shell environment: \x1b[31mnot loaded\x1b[0m\n",
		"  http_proxy: \x1b[31munset\x1b[0m\n",
		"TUN: \x1b[32mtrue\x1b[0m\n",
		"TUN: \x1b[31mfalse\x1b[0m\n",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("missing color/state %q in %q", expected, got)
		}
	}
	if strings.NewReplacer("\x1b[32m", "", "\x1b[31m", "", "\x1b[0m", "").Replace(got) != plain {
		t.Fatal("coloring changed status content")
	}
}
