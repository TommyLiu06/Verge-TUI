package tui

import (
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

type Palette struct {
	Base, Card, Sidebar                tcell.Style
	Text, Muted, Border, Accent, Green color.Color
}

// Modern palettes take inspiration from VS Code's default modern themes.
// Other palettes are original restrained and high-contrast variations.
func paletteFor(id string) Palette {
	makePalette := func(bg, panel, side, fg, muted, border, accent, green int32) Palette {
		hex := color.NewHexColor
		return Palette{
			Base:    tcell.StyleDefault.Foreground(hex(fg)).Background(hex(bg)),
			Card:    tcell.StyleDefault.Foreground(hex(fg)).Background(hex(panel)),
			Sidebar: tcell.StyleDefault.Foreground(hex(fg)).Background(hex(side)),
			Text:    hex(fg), Muted: hex(muted), Border: hex(border), Accent: hex(accent), Green: hex(green),
		}
	}
	switch id {
	case "dark":
		return makePalette(0x1e1e1e, 0x252526, 0x181818, 0xd4d4d4, 0xa9a9a9, 0x686868, 0x7da9df, 0x83b88a)
	case "light":
		return makePalette(0xffffff, 0xf6f6f6, 0xeeeeee, 0x202020, 0x626262, 0x949494, 0x245fa0, 0x3d7049)
	case "mist-dark":
		return makePalette(0x252b32, 0x2c333c, 0x20262d, 0xd6dbe1, 0xaab2bc, 0x727e8e, 0xa7b8cb, 0xa5b5a0)
	case "mist-light":
		return makePalette(0xf2f0eb, 0xfaf8f3, 0xe7e4de, 0x383b3e, 0x5f6368, 0x999791, 0x51677c, 0x5b7059)
	case "modern-dark":
		return makePalette(0x1f1f1f, 0x252526, 0x181818, 0xcccccc, 0xa0a0a0, 0x707070, 0x4daafc, 0x89d185)
	case "modern-light":
		return makePalette(0xffffff, 0xf8f8f8, 0xf0f0f0, 0x3b3b3b, 0x616161, 0x909090, 0x005fb8, 0x287738)
	case "vivid-dark":
		return makePalette(0x080c18, 0x11192a, 0x030710, 0xffffff, 0xb7c9df, 0x8399c7, 0x00d8ff, 0x78ff85)
	case "vivid-light":
		return makePalette(0xffffff, 0xf0f5ff, 0xe4eaff, 0x080c20, 0x37456b, 0x496391, 0x5400c9, 0x00662a)
	default:
		return Palette{Base: tcell.StyleDefault, Card: tcell.StyleDefault, Sidebar: tcell.StyleDefault, Text: color.Default, Muted: color.Default, Border: color.Default, Accent: color.PaletteColor(5), Green: color.PaletteColor(2)}
	}
}

func themeLabel(id string) string {
	labels := map[string]string{
		"terminal": "Default Follow Terminal", "dark": "Default Dark", "light": "Default Light",
		"mist-dark": "Mist Dark", "mist-light": "Mist Light", "modern-dark": "Modern Dark", "modern-light": "Modern Light",
		"vivid-dark": "Vivid Dark", "vivid-light": "Vivid Light",
	}
	if value, ok := labels[id]; ok {
		return value
	}
	return labels["terminal"]
}
