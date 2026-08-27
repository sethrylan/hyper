package demo_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
	wantCounts := map[model.Feed]int{
		model.FeedImportantNotifications: 7,
		model.FeedMyPullRequests:         5,
		model.FeedMyIssues:               4,
	}
	for feed, want := range wantCounts {
		cached := data.Feeds[feed]
		if got := len(cached.Items); got != want {
			t.Fatalf("%s count = %d, want %d", feed, got, want)
		}
		if !cached.RefreshedAt.Equal(wantRefreshedAt) {
			t.Fatalf("%s refresh time = %s, want %s", feed, cached.RefreshedAt, wantRefreshedAt)
		}
	}

	const closedPRKey = "github.com|PR_demo_k6_legacy_flags"
	important := data.Feeds[model.FeedImportantNotifications].Items
	index := slices.IndexFunc(important, func(item model.Item) bool { return item.Key == closedPRKey })
	if index < 0 {
		t.Fatalf("closed PR %q is missing", closedPRKey)
	}
	closedPR := important[index]
	if closedPR.Type != model.ItemTypePullRequest || closedPR.State != "closed" || closedPR.Merged {
		t.Fatalf("closed PR = %#v, want closed, unmerged pull request", closedPR)
	}
	if closedPR.AuthorLogin != "mona" {
		t.Fatalf("closed PR author = %q, want mona", closedPR.AuthorLogin)
	}
	if !slices.Equal(closedPR.SourceFeeds, []model.Feed{model.FeedImportantNotifications}) {
		t.Fatalf("closed PR source feeds = %v, want Important Notifications only", closedPR.SourceFeeds)
	}
	if slices.ContainsFunc(data.Feeds[model.FeedMyPullRequests].Items, func(item model.Item) bool { return item.Key == closedPRKey }) {
		t.Fatal("closed PR should not appear in My Pull Requests")
	}
}
