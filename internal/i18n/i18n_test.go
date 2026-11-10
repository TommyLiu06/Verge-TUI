package i18n

import (
	"strings"
	"testing"
)

func TestUpstreamLanguagesAndFallback(t *testing.T) {
	if len(Languages) != 13 {
		t.Fatal("language list differs from Verge")
	}
	for _, language := range Languages {
		if catalog(language) == nil || Names[language] == "" {
			t.Fatal("missing language", language)
		}
		for _, label := range []string{"Home", "Profiles", "Language", "Save", "Theme", "Default Follow Terminal", "DNS Listen"} {
			if catalog(language)[label] == "" {
				t.Fatalf("missing %s in %s", label, language)
			}
		}
	}
	if Text("zh", "Settings") != "设 置" || !strings.HasPrefix(Text("zh", "Core Version: v1"), Text("zh", "Core Version")+": ") {
		t.Fatal("upstream translation/prefix not used")
	}
	if Text("missing", "Save") != "Save" || Text("zh", "unknown TUI text") != "unknown TUI text" {
		t.Fatal("fallback changed")
	}
}
