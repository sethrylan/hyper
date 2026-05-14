package tui

import (
	"strings"
	"testing"

	"github.com/sethrylan/hyper/internal/model"
)

func TestTypeIcon(t *testing.T) {
	tests := []struct {
		name string
		item model.Item
		want string
	}{
		{
			name: "open issue uses circle with dot",
			item: model.Item{State: "OPEN", Type: model.ItemTypeIssue},
			want: "◉",
		},
		{
			name: "pull request uses branch glyph",
			item: model.Item{State: "OPEN", Type: model.ItemTypePullRequest},
			want: "⎇",
		},
		{
			name: "closed not planned issue uses empty set",
			item: model.Item{State: "CLOSED", StateReason: "NOT_PLANNED", Type: model.ItemTypeIssue},
			want: "∅",
		},
		{
			name: "closed completed issue keeps circle with dot",
			item: model.Item{State: "CLOSED", StateReason: "COMPLETED", Type: model.ItemTypeIssue},
			want: "◉",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := typeIcon(tt.item); !strings.Contains(got, tt.want) {
				t.Fatalf("typeIcon() = %q, want glyph %q", got, tt.want)
			}
		})
	}
}

func TestTypeIconIsBold(t *testing.T) {
	icon := typeIcon(model.Item{State: "OPEN", Type: model.ItemTypePullRequest})
	if !strings.Contains(icon, "\x1b[1;") {
		t.Fatalf("typeIcon() = %q, want bold ANSI style", icon)
	}
}

func TestIconColor(t *testing.T) {
	tests := []struct {
		name string
		item model.Item
		want string
	}{
		{
			name: "open issue is green",
			item: model.Item{State: "OPEN", Type: model.ItemTypeIssue},
			want: "10",
		},
		{
			name: "closed issue is purple",
			item: model.Item{State: "CLOSED", Type: model.ItemTypeIssue},
			want: "5",
		},
		{
			name: "closed not planned issue is gray",
			item: model.Item{State: "CLOSED", StateReason: "NOT_PLANNED", Type: model.ItemTypeIssue},
			want: "8",
		},
		{
			name: "draft pull request is gray",
			item: model.Item{Draft: true, State: "OPEN", Type: model.ItemTypePullRequest},
			want: "8",
		},
		{
			name: "merged pull request is purple",
			item: model.Item{Merged: true, State: "MERGED", Type: model.ItemTypePullRequest},
			want: "5",
		},
		{
			name: "closed unmerged pull request is red",
			item: model.Item{State: "CLOSED", Type: model.ItemTypePullRequest},
			want: "9",
		},
		{
			name: "closed merged pull request is purple",
			item: model.Item{Merged: true, State: "CLOSED", Type: model.ItemTypePullRequest},
			want: "5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := iconColor(tt.item); got != tt.want {
				t.Fatalf("iconColor() = %q, want %q", got, tt.want)
			}
		})
	}
}
