//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sethrylan/hyper/internal/model"
)

func TestItemRowAlignsMetadataColumns(t *testing.T) {
	updatedAt := time.Now().Add(-2 * time.Hour)
	first := layoutItem("Short", "alice", "author", updatedAt)
	second := layoutItem("A much longer title that should not move metadata", "bob", "review_requested", updatedAt)
	layout := itemRowLayoutForItems([]model.Item{first, second}, 100)

	firstRow := ansi.Strip(itemRowWithLayout(first, 100, layout))
	secondRow := ansi.Strip(itemRowWithLayout(second, 100, layout))

	if displayIndex(firstRow, "alice") != displayIndex(secondRow, "bob") {
		t.Fatalf("author columns not aligned:\n%q\n%q", firstRow, secondRow)
	}
	if displayIndex(firstRow, "2h") != displayIndex(secondRow, "2h") {
		t.Fatalf("age columns not aligned:\n%q\n%q", firstRow, secondRow)
	}
}

func TestItemRowDoesNotExpandTitleColumnToFullWidth(t *testing.T) {
	item := layoutItem("Short", "alice", "author", time.Now())
	row := itemRow(item, 100)

	if got := lipgloss.Width(row); got >= 100 {
		t.Fatalf("width = %d, want compact row for short title: %q", got, ansi.Strip(row))
	}
	if displayIndex(ansi.Strip(row), "alice") > 20 {
		t.Fatalf("author column starts too far right: %q", ansi.Strip(row))
	}
}

func TestItemRowTruncatesLongTitle(t *testing.T) {
	item := layoutItem(strings.Repeat("long-title ", 12), "sethrylan", "author", time.Now())
	row := itemRow(item, 72)

	if !strings.Contains(row, "…") {
		t.Fatalf("itemRow() = %q, want truncated title", ansi.Strip(row))
	}
	if got := lipgloss.Width(row); got != 72 {
		t.Fatalf("width = %d, want 72 for %q", got, ansi.Strip(row))
	}
}

func TestItemRowHandlesNarrowWidth(t *testing.T) {
	item := layoutItem("A very long title", "sethrylan", "author", time.Now())
	row := itemRow(item, 10)

	if row == "" {
		t.Fatal("itemRow() returned empty string for narrow width")
	}
	if got := lipgloss.Width(row); got > 10 {
		t.Fatalf("width = %d, want at most 10 for %q", got, ansi.Strip(row))
	}
}

func layoutItem(title, author, reason string, updatedAt time.Time) model.Item {
	return model.Item{
		AuthorLogin:        author,
		NotificationReason: reason,
		State:              "OPEN",
		Title:              title,
		Type:               model.ItemTypeIssue,
		UpdatedAt:          updatedAt,
	}
}

func displayIndex(row, needle string) int {
	before, _, found := strings.Cut(row, needle)
	if !found {
		return -1
	}
	return lipgloss.Width(before)
}
