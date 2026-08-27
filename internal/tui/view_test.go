//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

func TestRenderWithBorderAddsBlankPadding(t *testing.T) {
	m := Model{
		height:   8,
		width:    40,
		expanded: map[string]bool{},
		feeds:    emptyFeeds(),
		host:     "github.com",
		status:   "ready",
	}

	lines := strings.Split(m.renderWithBorder(), "\n")
	if len(lines) != 8 {
		t.Fatalf("line count = %d, want 8", len(lines))
	}
	if strings.TrimSpace(lines[0]) != "" || strings.TrimSpace(lines[len(lines)-1]) != "" {
		t.Fatalf("top and bottom border lines should be blank: %q / %q", lines[0], lines[len(lines)-1])
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != 40 {
			t.Fatalf("line %d width = %d, want 40: %q", i, got, line)
		}
		if !strings.HasPrefix(line, " ") || !strings.HasSuffix(line, " ") {
			t.Fatalf("line %d lacks side padding: %q", i, line)
		}
	}
}

func TestRenderIncludesSeparatorBeforeStatus(t *testing.T) {
	m := Model{
		width:    40,
		expanded: map[string]bool{},
		feeds:    emptyFeeds(),
		host:     "github.com",
		status:   "ready",
	}

	lines := strings.Split(m.render(), "\n")
	if len(lines) < 4 {
		t.Fatalf("rendered %d lines, want at least 4", len(lines))
	}
	separator := lines[len(lines)-3]
	if !strings.Contains(separator, "──") {
		t.Fatalf("separator line = %q, want horizontal rule", separator)
	}
	status := lines[len(lines)-2]
	if !strings.Contains(status, "ready") {
		t.Fatalf("status line = %q, want ready status immediately after separator", status)
	}
}

func TestFooterHelpShowsDoneOnlyForImportantNotifications(t *testing.T) {
	m := Model{activeFeed: 0}
	got := m.renderFooterHelp()
	if !strings.Contains(got, "E done") {
		t.Fatalf("important notifications help = %q, want E done", got)
	}
	if strings.Contains(got, "shift+r") {
		t.Fatalf("footer help = %q, want no rate limits shortcut", got)
	}

	m.activeFeed = 1
	if got := m.renderFooterHelp(); strings.Contains(got, "E done") {
		t.Fatalf("pull requests help = %q, want no E done", got)
	}

	m.activeFeed = 2
	if got := m.renderFooterHelp(); strings.Contains(got, "E done") {
		t.Fatalf("issues help = %q, want no E done", got)
	}
}

func TestRenderRateLimits(t *testing.T) {
	m := Model{
		account: "sethrylan",
		rateLimits: github.RateLimits{
			Core: github.RateLimitResource{
				Limit:     15000,
				Remaining: 14065,
				ResetAt:   time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC),
				Used:      935,
			},
			GraphQL: github.RateLimitResource{
				Limit:     10000,
				Remaining: 5761,
				ResetAt:   time.Date(2026, 5, 14, 10, 26, 0, 0, time.UTC),
				Used:      4239,
			},
			Search: github.RateLimitResource{
				Limit:     30,
				Remaining: 30,
				ResetAt:   time.Date(2026, 5, 14, 10, 8, 0, 0, time.UTC),
				Used:      0,
			},
			HyperCore:    github.RateLimitResource{Limit: 1249, Remaining: 1200, Used: 49},
			HyperGraphQL: github.RateLimitResource{Limit: 1249, Remaining: 1000, Used: 249},
			HyperSearch:  github.RateLimitResource{Limit: 7, Remaining: 7},
		},
	}
	view := m.renderRateLimits()
	for _, want := range []string{"GitHub rate limits", "account: sethrylan", "GitHub account", "Hyper budget (<25%)", "1249", "shift+r      close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderRateLimits() = %q, want %q", view, want)
		}
	}
}

func emptyFeeds() map[model.Feed][]model.Item {
	return map[model.Feed][]model.Item{}
}
