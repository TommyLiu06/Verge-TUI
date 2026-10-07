package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v3/color"
	"mihomo-tui/internal/controller"
)

func delayValue(p controller.Proxy) int {
	if len(p.History) == 0 {
		return -1
	}
	return p.History[len(p.History)-1].Delay
}

func delayLabel(p controller.Proxy) string {
	delay := delayValue(p)
	switch {
	case delay == -2:
		return "Testing"
	case delay < 0:
		return "Not tested"
	case delay > 100_000:
		return "Error"
	case delay == 0 || delay >= controller.DelayTimeoutMillis:
		return "Timeout"
	default:
		return fmt.Sprintf("%d ms", delay)
	}
}

// Match formatDelayColor in services/delay.ts and the default light/dark
// palettes in pages/_theme.tsx at the pinned Clash Verge Rev revision.
func (m *Model) delayColor(p controller.Proxy) color.Color {
	delay := delayValue(p)
	if delay < 0 {
		return color.Default
	}
	palette := [4]int32{0x06943D, 0x007AFF, 0xFF9500, 0xFF3B30}
	if m.Dark {
		palette = [4]int32{0x30D158, 0x0A84FF, 0xFF9F0A, 0xFF453A}
	}
	grade := 0
	switch {
	case delay == 0 || delay >= controller.DelayTimeoutMillis:
		grade = 3
	case delay >= 400:
		grade = 2
	case delay >= 250:
		grade = 1
	}
	return color.NewHexColor(palette[grade])
}

func darkTerminal() bool {
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	if len(parts) < 2 {
		return false
	}
	background, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || background < 0 || background > 255 {
		return false
	}
	r, g, b := color.PaletteColor(background).RGB()
	return 299*r+587*g+114*b < 128_000
}
