package tui

import (
	"github.com/gdamore/tcell/v3/color"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
	"verge-tui/internal/app"
	"verge-tui/internal/controller"
)

type SearchOptions struct{ MatchCase, WholeWord, Regex bool }
type searchCache struct {
	Query   string
	Options SearchOptions
	Pattern *regexp.Regexp
	Ready   bool
	Invalid bool
}
type ruleSearchCache struct {
	First   *controller.Rule
	Count   int
	Query   string
	Options SearchOptions
	Indices []int
}

func (m *Model) ruleIndices() []int {
	var first *controller.Rule
	if len(m.Rules.Rules) > 0 {
		first = &m.Rules.Rules[0]
	}
	c := &m.RuleSearchCache
	if c.First == first && c.Count == len(m.Rules.Rules) && c.Query == m.Search[5] && c.Options == m.SearchOptions[5] {
		return c.Indices
	}
	*c = ruleSearchCache{First: first, Count: len(m.Rules.Rules), Query: m.Search[5], Options: m.SearchOptions[5]}
	for i, r := range m.Rules.Rules {
		if m.searchMatch(5, r.Payload) {
			c.Indices = append(c.Indices, i)
		}
	}
	return c.Indices
}

func (m *Model) searchMatch(tab int, text string) bool {
	query, options := m.Search[tab], m.SearchOptions[tab]
	if query == "" {
		m.SearchError[tab] = ""
		return true
	}
	if !options.Regex && !options.WholeWord {
		m.SearchError[tab] = ""
		if !options.MatchCase {
			text, query = strings.ToLower(text), strings.ToLower(query)
		}
		return strings.Contains(text, query)
	}
	cache := &m.SearchCache[tab]
	if cache.Query != query || cache.Options != options || !cache.Ready {
		pattern := query
		if !options.Regex {
			pattern = regexp.QuoteMeta(query)
		}
		if options.WholeWord && !options.Regex {
			pattern = `\b(?:` + pattern + `)\b`
		}
		if !options.MatchCase {
			pattern = "(?i)" + pattern
		}
		compiled, err := regexp.Compile(pattern)
		*cache = searchCache{Query: query, Options: options, Pattern: compiled, Ready: true, Invalid: err != nil}

	}
	if cache.Invalid {
		m.SearchError[tab] = m.tr("Invalid regular expression")
		return false
	}
	m.SearchError[tab] = ""
	return cache.Pattern.MatchString(text)
}
func (m *Model) searchOptionsDialog() {
	o := m.SearchOptions[m.Tab]
	m.Modal = &Modal{Title: "Search Options", List: true}
	for _, option := range []struct {
		label, id string
		enabled   bool
	}{{"Match Case", "case", o.MatchCase}, {"Match Whole Word", "word", o.WholeWord}, {"Use Regular Expression", "regex", o.Regex}} {
		label := m.tr(option.label)
		if option.enabled {
			label = "● " + label
		} else {
			label = "○ " + label
		}
		m.Modal.Choices = append(m.Modal.Choices, Choice{label, app.Action{Kind: "search-option", ID: option.id}})
	}
	m.Focus = ""
}

func wrapLogText(text string, width int) []string {
	var lines []string
	width = max(1, width)
	for _, line := range strings.Split(Clean(strings.ReplaceAll(text, "\n", "\u2028")), "\u2028") {
		var out strings.Builder
		size := 0
		it := graphemes.FromString(line)
		for it.Next() {
			cluster := it.Value()
			w := displaywidth.String(cluster)
			if size+w > width && out.Len() > 0 {
				lines = append(lines, out.String())
				out.Reset()
				size = 0
			}
			out.WriteString(cluster)
			size += w
		}
		lines = append(lines, out.String())
	}
	return lines
}

func ruleColor(name string, p Palette) color.Color {
	if name == "DIRECT" {
		return p.Text
	}
	if name == "REJECT" || name == "REJECT-DROP" {
		return color.PaletteColor(1)
	}
	sum := 0
	for _, r := range utf16.Encode([]rune(name)) {
		sum += int(r)
	}
	colors := []color.Color{p.Accent, color.PaletteColor(5), color.PaletteColor(6), color.PaletteColor(3), p.Green}
	return colors[sum%len(colors)]
}
