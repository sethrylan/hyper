//nolint:testpackage // These tests exercise package internals.
package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

func TestReconcileDone(t *testing.T) {
	doneAt := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	unchanged := model.Item{Key: "unchanged", UpdatedAt: doneAt.Add(-time.Minute)}
	updated := model.Item{Key: "updated", UpdatedAt: doneAt.Add(time.Minute)}
	done := map[string]DoneState{
		"unchanged": {DoneAt: doneAt, UpdatedAt: unchanged.UpdatedAt},
		"updated":   {DoneAt: doneAt, UpdatedAt: doneAt.Add(-time.Minute)},
	}

	got := ReconcileDone([]model.Item{unchanged, updated}, done)
	if len(got) != 1 || got[0].Key != "updated" || got[0].Done {
		t.Fatalf("reconciled items = %#v, want only updated item as not done", got)
	}
	if _, ok := done["updated"]; ok {
		t.Fatal("stale done state should be cleared")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, openErr := Open(path)
	if openErr != nil {
		t.Fatal(openErr)
	}
	refreshedAt := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	item := model.Item{Host: "github.com", Key: "issue", Title: "issue", Type: model.ItemTypeIssue}
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyIssues: {item},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}

	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := loaded.Data()
	if data.Version != schemaVersion || data.Account != "me" || data.Host != "github.com" {
		t.Fatalf("cache identity = %#v", data)
	}
	feed := data.Feeds[model.FeedMyIssues]
	if len(feed.Items) != 1 || feed.Items[0].Key != item.Key || !feed.RefreshedAt.Equal(refreshedAt) {
		t.Fatalf("issue feed = %#v", feed)
	}
}

func TestReplaceFeedsPreservesUnspecifiedFeedsAndIndependentCopies(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	second := first.Add(5 * time.Second)
	important := model.Item{Key: "shared", Title: "rich", NotificationThreadID: "thread"}
	pullRequest := model.Item{Key: "shared", Title: "old", Type: model.ItemTypePullRequest}
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {important},
		model.FeedMyPullRequests:         {pullRequest},
	}, first); err != nil {
		t.Fatal(err)
	}
	pullRequest.Title = "new"
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {pullRequest},
	}, second); err != nil {
		t.Fatal(err)
	}

	data := store.Data()
	if got := data.Feeds[model.FeedImportantNotifications]; got.Items[0].Title != "rich" || !got.RefreshedAt.Equal(first) {
		t.Fatalf("important feed changed: %#v", got)
	}
	if got := data.Feeds[model.FeedMyPullRequests]; got.Items[0].Title != "new" || !got.RefreshedAt.Equal(second) {
		t.Fatalf("pull request feed = %#v", got)
	}
}

func TestReplaceFeedsClearsFeedsWhenIdentityChanges(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceFeeds("old-account", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {{Key: "private-notification"}},
		model.FeedMyPullRequests:         {{Key: "private-pr"}},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceFeeds("new-account", "github.com", map[model.Feed][]model.Item{
		model.FeedMyIssues: {{Key: "new-issue"}},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	data := store.Data()
	if len(data.Feeds) != 1 || len(data.Feeds[model.FeedMyIssues].Items) != 1 {
		t.Fatalf("feeds after account change = %#v, want only new account's issue feed", data.Feeds)
	}
}

func TestSetIdentityClearsAndPersistsFeedsBeforeRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if replaceErr := store.ReplaceFeeds("old-account", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {{Key: "private-notification"}},
		model.FeedMyPullRequests:         {{Key: "private-pr"}},
	}, time.Now()); replaceErr != nil {
		t.Fatal(replaceErr)
	}
	if identityErr := store.SetIdentity("new-account", "github.com"); identityErr != nil {
		t.Fatal(identityErr)
	}

	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := reloaded.Data()
	if data.Account != "new-account" || data.Host != "github.com" || len(data.Feeds) != 0 {
		t.Fatalf("cache after identity reset = %#v, want new identity with no feeds", data)
	}
}

func TestLegacyCacheDropsFeedsButPreservesDoneState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	legacy := `{"account":"me","items":{"item":{"key":"item"}},"feed_item_ids":{"important_notifications":["item"]},"done":{"item":{"done_at":"2026-08-27T14:00:00Z","updated_at":"2026-08-27T13:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := store.Data()
	if len(data.Feeds) != 0 || data.Account != "" {
		t.Fatalf("legacy feed data was not reset: %#v", data)
	}
	if _, ok := data.Done["item"]; !ok {
		t.Fatal("legacy Done marker was not preserved")
	}
}

func TestOpenDefaultUsesHyperDirectoryInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".hyper", "cache.json"); store.path != want {
		t.Fatalf("default cache path = %q, want %q", store.path, want)
	}
}

func TestDoneStateOnlyChangesImportantFeed(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{Key: "shared", Title: "item", UpdatedAt: time.Now()}
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {item},
		model.FeedMyPullRequests:         {item},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	doneAt := item.UpdatedAt.Add(time.Hour)
	if err := store.MarkDone(item, doneAt); err != nil {
		t.Fatal(err)
	}
	data := store.Data()
	if !data.Feeds[model.FeedImportantNotifications].Items[0].Done {
		t.Fatal("important item was not marked done")
	}
	if data.Feeds[model.FeedMyPullRequests].Items[0].Done {
		t.Fatal("pull request copy should not inherit local Done state")
	}
	if err := store.MarkUndone(data.Feeds[model.FeedImportantNotifications].Items[0]); err != nil {
		t.Fatal(err)
	}
	data = store.Data()
	if data.Feeds[model.FeedImportantNotifications].Items[0].Done {
		t.Fatal("important item is still marked done")
	}
}

func TestReplaceFeedsClearsDoneFieldsOutsideImportant(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{Key: "pr", Done: true, DoneAt: time.Now(), Type: model.ItemTypePullRequest}
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {item},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := store.Data().Feeds[model.FeedMyPullRequests].Items[0]
	if got.Done || !got.DoneAt.IsZero() {
		t.Fatalf("non-important item retained Done state: %#v", got)
	}
}
