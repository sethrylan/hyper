package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sethrylan/hyper/internal/model"
)

const (
	itemIndentWidth   = 4
	iconColumnWidth   = 2
	authorColumnWidth = 16
	reasonColumnWidth = 12
	ageColumnWidth    = 6
)

func itemRow(item model.Item, width int) string {
	return itemRowWithLayout(item, width, itemRowLayoutForItems([]model.Item{item}, width))
}

type itemRowLayout struct {
	titleWidth int
}

func itemRowLayoutForItems(items []model.Item, width int) itemRowLayout {
	if width <= 0 {
		width = 100
	}
	titleWidth := 1
	for _, item := range items {
		titleWidth = max(titleWidth, lipgloss.Width(item.Title))
	}
	titleWidth = min(titleWidth, maxItemTitleWidth(width))
	return itemRowLayout{titleWidth: titleWidth}
}

func itemRowWithLayout(item model.Item, width int, layout itemRowLayout) string {
	done := " "
	if item.Done {
		done = "✓"
	}
	reason := ""
	if item.NotificationReason != "" {
		reason = "[" + item.NotificationReason + "]"
	}

	titleWidth := min(layout.titleWidth, maxItemTitleWidth(width))
	if titleWidth < 1 {
		return compactItemRow(done, item, width)
	}

	columns := []string{
		padRight(done, 1),
		padRight(typeIcon(item), iconColumnWidth),
		padRight(item.Title, titleWidth),
		padRight(item.AuthorLogin, authorColumnWidth),
		padRight(reason, reasonColumnWidth),
		padLeft(age(item.UpdatedAt), ageColumnWidth),
	}
	return strings.Repeat(" ", itemIndentWidth) + strings.Join(columns, " ")
}

func maxItemTitleWidth(width int) int {
	return width - itemIndentWidth - 1 - iconColumnWidth - authorColumnWidth - reasonColumnWidth - ageColumnWidth - 5
}

func compactItemRow(done string, item model.Item, width int) string {
	prefix := strings.Repeat(" ", itemIndentWidth) + done + " " + typeIcon(item) + " "
	if width <= lipgloss.Width(prefix) {
		return truncate(prefix, width)
	}
	suffix := " " + padLeft(age(item.UpdatedAt), ageColumnWidth)
	titleWidth := width - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if titleWidth < 1 {
		return truncate(prefix+item.Title, width)
	}
	return prefix + truncate(item.Title, titleWidth) + suffix
}

func padRight(value string, width int) string {
	value = truncate(value, width)
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func padLeft(value string, width int) string {
	value = truncate(value, width)
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return strings.Repeat(" ", padding) + value
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "…")
}
