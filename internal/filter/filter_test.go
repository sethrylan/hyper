//nolint:testpackage // These tests exercise package internals.
package filter

import (
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

func TestQuery_DefaultRelativeDate(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	query, err := Query(model.FeedMyPullRequests, now)
	if err != nil {
		t.Fatal(err)
	}
	want := "is:open is:pr author:@me archived:false created:>2026-04-12"
	if query != want {
		t.Fatalf("query = %q, want %q", query, want)
	}
}

func TestImportant(t *testing.T) {
	saved := true
	tests := []struct {
		name string
		item model.Item
		want bool
	}{
		{name: "author", item: model.Item{AuthorLogin: "me"}, want: true},
		{name: "assignee", item: model.Item{Assignees: []string{"me"}}, want: true},
		{name: "saved", item: model.Item{Saved: &saved}, want: true},
		{name: "mention", item: model.Item{NotificationReason: "MENTION"}, want: true},
		{name: "reviewer", item: model.Item{Reviewers: []string{"ME"}}, want: true},
		{name: "review request", item: model.Item{ReviewRequests: []string{"me"}}, want: true},
		{name: "not important", item: model.Item{AuthorLogin: "other"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Important(tt.item, "me"); got != tt.want {
				t.Fatalf("Important() = %v, want %v", got, tt.want)
			}
		})
	}
}
