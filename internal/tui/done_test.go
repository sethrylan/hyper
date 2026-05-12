package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

func TestRefreshUsesReconciledDoneCache(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}

	updatedAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := model.Item{
		Host:            "github.com",
		Key:             "github.com|I_done",
		NodeID:          "I_done",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "done issue",
		Type:            model.ItemTypeIssue,
		UpdatedAt:       updatedAt,
		URL:             "https://github.com/owner/repo/issues/1",
	}
	if err := store.MarkDone(item, updatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	m := Model{
		activeFeed:     0,
		expanded:       map[string]bool{},
		feeds:          map[model.Feed][]model.Item{model.FeedImportantNotifications: {item}},
		host:           "github.com",
		selectedByFeed: map[model.Feed]int{},
		store:          store,
	}
	updated, _ := m.Update(refreshMsg{
		result: github.RefreshResult{
			Account: "me",
			Feeds: map[model.Feed][]model.Item{
				model.FeedImportantNotifications: {item},
			},
			RefreshedAt: updatedAt.Add(2 * time.Hour),
		},
	})
	got := updated.(Model)
	if len(got.feeds[model.FeedImportantNotifications]) != 0 {
		t.Fatalf("important feed count = %d, want done item hidden after refresh", len(got.feeds[model.FeedImportantNotifications]))
	}
}

func TestDoneActionMarksVisibleDoneItemUndone(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}

	doneAt := time.Date(2026, 5, 13, 13, 0, 0, 0, time.UTC)
	item := model.Item{
		Done:            true,
		DoneAt:          doneAt,
		Host:            "github.com",
		Key:             "github.com|I_done",
		NodeID:          "I_done",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "done issue",
		Type:            model.ItemTypeIssue,
		UpdatedAt:       doneAt.Add(-time.Hour),
		URL:             "https://github.com/owner/repo/issues/1",
	}
	if err := store.MarkDone(item, doneAt); err != nil {
		t.Fatal(err)
	}

	m := Model{
		activeFeed:     0,
		expanded:       map[string]bool{},
		feeds:          map[model.Feed][]model.Item{model.FeedImportantNotifications: {item}},
		host:           "github.com",
		selectedByFeed: map[model.Feed]int{},
		store:          store,
	}
	m.rebuildRows()

	updated, _ := m.handleAction(ActionDone)
	if updated.status != "marked undone" {
		t.Fatalf("status = %q, want marked undone", updated.status)
	}
	updatedItem := updated.feeds[model.FeedImportantNotifications][0]
	if updatedItem.Done || !updatedItem.DoneAt.IsZero() {
		t.Fatalf("item after toggle = %#v, want not done", updatedItem)
	}
	if _, ok := store.Data().Done[item.Key]; ok {
		t.Fatal("done state was not removed from cache")
	}
}

func TestDoneActionIsIgnoredOutsideImportantNotifications(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{
		Host:            "github.com",
		Key:             "github.com|PR_1",
		NodeID:          "PR_1",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "open pull request",
		Type:            model.ItemTypePullRequest,
		UpdatedAt:       time.Date(2026, 5, 13, 13, 0, 0, 0, time.UTC),
		URL:             "https://github.com/owner/repo/pull/1",
	}
	m := Model{
		activeFeed:     1,
		expanded:       map[string]bool{},
		feeds:          map[model.Feed][]model.Item{model.FeedMyPullRequests: {item}},
		host:           "github.com",
		selectedByFeed: map[model.Feed]int{},
		store:          store,
	}
	m.rebuildRows()

	updated, _ := m.handleAction(ActionDone)
	if updated.status != "local done is only available in Important Notifications" {
		t.Fatalf("status = %q, want unavailable message", updated.status)
	}
	if _, ok := store.Data().Done[item.Key]; ok {
		t.Fatal("my pull request was stored as locally done")
	}
	if updated.feeds[model.FeedMyPullRequests][0].Done {
		t.Fatal("my pull request was marked done in the TUI")
	}
}

func TestFeedsFromCacheClearsDoneOutsideImportantNotifications(t *testing.T) {
	item := model.Item{
		Done:            true,
		DoneAt:          time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC),
		Host:            "github.com",
		Key:             "github.com|PR_1",
		NodeID:          "PR_1",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "open pull request",
		Type:            model.ItemTypePullRequest,
		UpdatedAt:       time.Date(2026, 5, 13, 13, 0, 0, 0, time.UTC),
		URL:             "https://github.com/owner/repo/pull/1",
	}
	feeds := feedsFromCache(cache.Data{
		FeedItemIDs: map[model.Feed][]string{
			model.FeedImportantNotifications: {item.Key},
			model.FeedMyPullRequests:         {item.Key},
		},
		Items: map[string]model.Item{item.Key: item},
	})

	if !feeds[model.FeedImportantNotifications][0].Done {
		t.Fatal("important notification should preserve done state")
	}
	if feeds[model.FeedMyPullRequests][0].Done {
		t.Fatal("my pull requests should clear done state")
	}
}
