//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestActionForKey(t *testing.T) {
	tests := map[string]Action{
		"j":         ActionDown,
		"down":      ActionDown,
		"k":         ActionUp,
		"up":        ActionUp,
		"l":         ActionExpand,
		"right":     ActionExpand,
		"h":         ActionCollapse,
		"left":      ActionCollapse,
		"E":         ActionDone,
		"shift+e":   ActionDone,
		"y":         ActionCopy,
		"o":         ActionOpen,
		"enter":     ActionOpen,
		"r":         ActionRefresh,
		"R":         ActionRateLimits,
		"shift+r":   ActionRateLimits,
		"?":         ActionHelp,
		"tab":       ActionNextFeed,
		"shift+tab": ActionPrevFeed,
		"q":         ActionQuit,
		"ctrl+c":    ActionQuit,
	}
	for key, want := range tests {
		if got := ActionForKey(key); got != want {
			t.Fatalf("ActionForKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestShiftTabKeystrokeMapsToPreviousFeed(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if got := ActionForKey(msg.Keystroke()); got != ActionPrevFeed {
		t.Fatalf("ActionForKey(%q) = %q, want %q", msg.Keystroke(), got, ActionPrevFeed)
	}
}

func TestShiftEKeystrokeMapsToDone(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Text: "E", Code: 'e', ShiftedCode: 'E', Mod: tea.ModShift})
	if got := ActionForKey(msg.Keystroke()); got != ActionDone {
		t.Fatalf("ActionForKey(%q) = %q, want %q", msg.Keystroke(), got, ActionDone)
	}
}

func TestShiftRKeystrokeMapsToRateLimits(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Text: "R", Code: 'r', ShiftedCode: 'R', Mod: tea.ModShift})
	if got := ActionForKey(msg.Keystroke()); got != ActionRateLimits {
		t.Fatalf("ActionForKey(%q) = %q, want %q", msg.Keystroke(), got, ActionRateLimits)
	}
}
