package shellenv

import (
	"strings"
	"testing"
)

func TestStatusColorsKeepListenerAndEnvironmentIndependent(t *testing.T) {
	plain := "Installation: user\nCore: running (test)\nProxy listener: listening\nShell environment: not loaded\n  http_proxy: unset\n"
	got := ColorStatus(plain)
	for _, expected := range []string{
		"Installation: user\n",
		"\x1b[32mCore: running (test)\x1b[0m\n",
		"\x1b[32mProxy listener: listening\x1b[0m\n",
		"\x1b[31mShell environment: not loaded\x1b[0m\n",
		"\x1b[31m  http_proxy: unset\x1b[0m\n",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("missing color/state %q in %q", expected, got)
		}
	}
	if strings.NewReplacer("\x1b[32m", "", "\x1b[31m", "", "\x1b[0m", "").Replace(got) != plain {
		t.Fatal("coloring changed status content")
	}
}
