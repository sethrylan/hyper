package demo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/demo"
	"github.com/sethrylan/hyper/internal/model"
)

func TestCacheJSON(t *testing.T) {
	content := demo.CacheJSON()
	if !json.Valid(content) {
		t.Fatal("demo cache is not valid JSON")
	}

	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := cache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := store.Data()
	if data.Account != "mona" || data.Host != "github.com" {
		t.Fatalf("account/host = %q/%q, want mona/github.com", data.Account, data.Host)
	}
	wantRefreshedAt := time.Date(2000, time.January, 1, 15, 4, 0, 0, time.UTC)
	if !data.LastRefresh.Equal(wantRefreshedAt) {
		t.Fatalf("last refresh = %s, want %s", data.LastRefresh, wantRefreshedAt)
	}
	wantCounts := map[model.Feed]int{
		model.FeedImportantNotifications: 6,
		model.FeedMyPullRequests:         5,
		model.FeedMyIssues:               4,
	}
	for feed, want := range wantCounts {
		if got := len(data.FeedItemIDs[feed]); got != want {
			t.Fatalf("%s count = %d, want %d", feed, got, want)
		}
	}
}
