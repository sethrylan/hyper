package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

func TestCacheStatus(t *testing.T) {
	if got := cacheStatus(cache.Data{}); got != "cache empty" {
		t.Fatalf("cacheStatus(empty) = %q, want cache empty", got)
	}

	data := cache.Data{
		FeedItemIDs: map[model.Feed][]string{
			model.FeedImportantNotifications: {"one", "two"},
			model.FeedMyIssues:               {"three"},
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

func TestRenderStatusIncludesRefreshProgress(t *testing.T) {
	m := Model{
		account:   "me",
		host:      "github.com",
		loading:   true,
		loadingAt: time.Now().Add(-75 * time.Second),
		refreshProgress: github.RefreshProgress{
			DetailStep:  3,
			DetailTotal: 7,
			Phase:       "supplemental searches",
			Step:        5,
			Total:       7,
		},
	}

	status := m.renderStatus()
	if !strings.Contains(status, "refreshing 5/7: supplemental searches 3/7") {
		t.Fatalf("renderStatus() = %q, want progress text", status)
	}
	if !strings.Contains(status, "1m15s") {
		t.Fatalf("renderStatus() = %q, want elapsed duration", status)
	}
}
