package tui

import (
	"strings"
	"testing"

	"verge-tui/internal/app"
)

func TestVergeSearchModesAndInvalidRegex(t *testing.T) {
	m := pageFixture()
	m.Tab = 4
	m.Search[4] = "EXAMPLE"
	if len(m.connectionRows()) != 1 {
		t.Fatal("case-insensitive search failed")
	}
	m.SearchOptions[4].MatchCase = true
	if len(m.connectionRows()) != 0 {
		t.Fatal("case-sensitive search failed")
	}
	m.SearchOptions[4] = SearchOptions{WholeWord: true}
	m.Search[4] = "ample"
	if len(m.connectionRows()) != 0 {
		t.Fatal("whole-word filter accepted a fragment")
	}
	m.SearchOptions[4] = SearchOptions{Regex: true}
	m.Search[4] = `^example\.com$`
	if len(m.connectionRows()) != 1 {
		t.Fatal("anchored host regex failed")
	}
	m.Search[4] = "["
	if len(m.connectionRows()) != 0 || m.SearchError[4] == "" {
		t.Fatal("invalid regex was silently accepted")
	}
	if !strings.Contains(m.Render(120, 42).Plain(), "Invalid regular expression") {
		t.Fatal("invalid regex feedback hidden")
	}
	m.SearchOptions[4] = SearchOptions{}
	m.Search[4] = "example"
	if len(m.connectionRows()) != 1 || m.SearchError[4] != "" {
		t.Fatal("search error did not clear")
	}
}
func TestConnectionTableColumnsSortingAndNarrowViewport(t *testing.T) {
	m := pageFixture()
	m.Tab = 4
	m.ConnectionTable = true
	m.Connections.Connections[1].DownloadSpeed = 12345
	m.Activate(app.Action{Kind: "table-sort", ID: "DL Speed"})
	if m.connectionRows()[0].ID != "b" {
		t.Fatal("speed column did not sort numerically")
	}
	m.Activate(app.Action{Kind: "table-sort", ID: "DL Speed"})
	if m.connectionRows()[0].ID != "a" {
		t.Fatal("second click did not reverse sorting")
	}
	text := m.Render(120, 42).Plain()
	if !strings.Contains(text, "DL Speed") || !strings.Contains(text, "UL Speed") {
		t.Fatal("missing table headers")
	}
	f := m.Render(60, 18)
	found := false
	for _, h := range f.Hits {
		if h.Action.Kind == "connection-detail" {
			found = true
		}
	}
	if !found {
		t.Fatal("table has no visible rows in minimum terminal")
	}
	m.Activate(app.Action{Kind: "table-columns"})
	m.Activate(app.Action{Kind: "table-column", ID: "Host"})
	if m.columnEnabled("Host") {
		t.Fatal("column visibility change ignored")
	}
	m.closeModal()
	m.Activate(app.Action{Kind: "table-pan", Value: "200"})
	m.Render(120, 42)
	if m.TableLeft <= 0 {
		t.Fatal("wide table cannot be scrolled horizontally")
	}
}
func TestRuleSearchCacheAndFullLogWrapping(t *testing.T) {
	m := pageFixture()
	m.Search[5] = "example"
	if len(m.ruleIndices()) != 1 {
		t.Fatal("rule filter failed")
	}
	if allocations := testing.AllocsPerRun(50, func() { m.ruleIndices() }); allocations != 0 {
		t.Fatal("unchanged filter allocated memory", allocations)
	}
	m.Search[5] = ""
	if len(m.ruleIndices()) != 2 {
		t.Fatal("query change reused stale filter")
	}
	lines := wrapLogText(strings.Repeat("长", 100)+"\nnext", 10)
	if len(lines) != 21 || lines[len(lines)-1] != "next" {
		t.Fatal("long log details were truncated", lines)
	}
}
