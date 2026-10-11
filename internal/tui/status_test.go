//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/model"
)

func TestCacheStatus(t *testing.T) {
	if got := cacheStatus(cache.Data{}); got != "cache empty" {
		t.Fatalf("cacheStatus(empty) = %q, want cache empty", got)
	}

	data := cache.Data{
		Feeds: map[model.Feed]cache.FeedData{
			model.FeedImportantNotifications: {Items: []model.Item{{Key: "one"}, {Key: "two"}}},
			model.FeedMyIssues:               {Items: []model.Item{{Key: "three"}}},
		},
	}
	if got := cacheStatus(data); got != "cache ready (3 items)" {
		t.Fatalf("cacheStatus(data) = %q, want cache ready count", got)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		1500 * time.Millisecond: "2s",
		75 * time.Second:        "1m15s",
	}
	for input, want := range tests {
		if got := formatDuration(input); got != want {
			t.Fatalf("formatDuration(%s) = %q, want %q", input, got, want)
		}
	}
}

func TestRenderStatusIncludesGenericRefreshProgress(t *testing.T) {
	m := Model{
		account:             "me",
		host:                "github.com",
		pullRequestsLoading: true,
		pullRequestsStart:   time.Now().Add(-75 * time.Second),
	}

	status := m.renderStatus()
	if !strings.Contains(status, "refreshing from GitHub") {
		t.Fatalf("renderStatus() = %q, want generic refresh text", status)
	}
	if !strings.Contains(status, "1m15s") {
		t.Fatalf("renderStatus() = %q, want elapsed duration", status)
	}
}
