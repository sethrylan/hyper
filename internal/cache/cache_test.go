//nolint:testpackage // These tests exercise package internals.
package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/model"
	"github.com/sethrylan/hyper/internal/quota"
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
	if len(got) != 1 {
		t.Fatalf("reconciled item count = %d, want 1", len(got))
	}
	if got[0].Key != "updated" || got[0].Done {
		t.Fatal("updated item should be re-added as not done")
	}
	if _, ok := done["updated"]; ok {
		t.Fatal("stale done state should be cleared")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{
		Host:            "github.com",
		Key:             "github.com|I_1",
		NodeID:          "I_1",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "issue",
		Type:            model.ItemTypeIssue,
		UpdatedAt:       time.Now().UTC(),
		URL:             "https://github.com/owner/repo/issues/1",
	}
	if replaceErr := store.Replace("me", "github.com", map[model.Feed][]model.Item{model.FeedMyIssues: {item}}, time.Now().UTC()); replaceErr != nil {
		t.Fatal(replaceErr)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := loaded.Data()
	if data.Account != "me" {
		t.Fatalf("account = %q, want me", data.Account)
	}
	if _, ok := data.Items[item.Key]; !ok {
		t.Fatalf("item %q missing after round trip", item.Key)
	}
	if len(data.FeedItemIDs[model.FeedMyIssues]) != 1 {
		t.Fatalf("feed item count = %d, want 1", len(data.FeedItemIDs[model.FeedMyIssues]))
	}
}

func TestReplaceFeedPreservesOtherFeedsAndIndependentTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	second := first.Add(5 * time.Second)
	pr := model.Item{Host: "github.com", Key: "pr", Title: "pr", Type: model.ItemTypePullRequest}
	issue := model.Item{Host: "github.com", Key: "issue", Title: "issue", Type: model.ItemTypeIssue}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {pr},
		model.FeedMyIssues:       {issue},
	}, first); err != nil {
		t.Fatal(err)
	}
	pr.Title = "new"
	if err := store.ReplaceFeed("me", "github.com", model.FeedMyPullRequests, []model.Item{pr}, second); err != nil {
		t.Fatal(err)
	}
	data := store.Data()
	if len(data.FeedItemIDs[model.FeedMyIssues]) != 1 || data.Items["issue"].Title != "issue" {
		t.Fatalf("issue feed changed: %#v", data)
	}
	if !data.LastRefreshByFeed[model.FeedMyPullRequests].Equal(second) || !data.LastRefreshByFeed[model.FeedMyIssues].Equal(first) {
		t.Fatalf("feed timestamps = %#v, want independent times", data.LastRefreshByFeed)
	}
}

func TestFeedReplacementPreservesQuotaState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	state := quota.State{Host: "github.com", Account: "me", Resources: map[quota.Resource]quota.Window{
		quota.ResourceGraphQL: {Limit: 5000, Used: 42, ResetAt: time.Now().Add(time.Hour)},
	}}
	if saveErr := store.SaveQuotaState(state); saveErr != nil {
		t.Fatal(saveErr)
	}
	if replaceErr := store.ReplaceFeed("me", "github.com", model.FeedMyPullRequests, nil, time.Now()); replaceErr != nil {
		t.Fatal(replaceErr)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.QuotaState().Resources[quota.ResourceGraphQL].Used; got != 42 {
		t.Fatalf("persisted GraphQL usage = %d, want 42", got)
	}
}

func TestLeanFeedRefreshPreservesSharedNotificationMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	rich := model.Item{
		Host: "github.com", Key: "shared", NodeID: "PR_one", Title: "old", Type: model.ItemTypePullRequest,
		NotificationThreadID: "thread", Reviewers: []string{"me"}, UpdatedAt: updatedAt,
	}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {rich},
		model.FeedMyPullRequests:         {rich},
	}, updatedAt); err != nil {
		t.Fatal(err)
	}
	lean := model.Item{Host: "github.com", Key: "shared", NodeID: "PR_one", Title: "new", Type: model.ItemTypePullRequest, UpdatedAt: updatedAt.Add(time.Minute)}
	if err := store.ReplaceFeed("me", "github.com", model.FeedMyPullRequests, []model.Item{lean}, updatedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	item := store.Data().Items[lean.Key]
	if item.Title != "new" || item.NotificationThreadID != "thread" || len(item.Reviewers) != 1 {
		t.Fatalf("merged item = %#v, want fresh fields and rich notification metadata", item)
	}
}

func TestOpenDefaultUsesHyperDirectoryInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".hyper", "cache.json")
	if store.path != want {
		t.Fatalf("default cache path = %q, want %q", store.path, want)
	}
}

func TestMarkDoneAllowsNonNotificationItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{
		Host:            "github.com",
		Key:             "github.com|I_2",
		NodeID:          "I_2",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "supplemental issue",
		Type:            model.ItemTypeIssue,
		UpdatedAt:       time.Date(2026, 5, 13, 13, 0, 0, 0, time.UTC),
		URL:             "https://github.com/owner/repo/issues/2",
	}
	doneAt := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	if err := store.MarkDone(item, doneAt); err != nil {
		t.Fatal(err)
	}
	data := store.Data()
	if _, ok := data.Done[item.Key]; !ok {
		t.Fatal("non-notification item was not recorded as done")
	}
	if !data.Items[item.Key].Done {
		t.Fatal("non-notification item was not marked done in cache")
	}
}

func TestMarkUndoneClearsDoneState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{
		Host:            "github.com",
		Key:             "github.com|I_3",
		NodeID:          "I_3",
		RepositoryName:  "repo",
		RepositoryOwner: "owner",
		Title:           "done issue",
		Type:            model.ItemTypeIssue,
		UpdatedAt:       time.Date(2026, 5, 13, 13, 0, 0, 0, time.UTC),
		URL:             "https://github.com/owner/repo/issues/3",
	}
	if err := store.MarkDone(item, item.UpdatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	item.Done = true
	item.DoneAt = item.UpdatedAt.Add(time.Hour)
	if err := store.MarkUndone(item); err != nil {
		t.Fatal(err)
	}

	data := store.Data()
	if _, ok := data.Done[item.Key]; ok {
		t.Fatal("done state was not cleared")
	}
	if data.Items[item.Key].Done {
		t.Fatal("cached item is still marked done")
	}
	if !data.Items[item.Key].DoneAt.IsZero() {
		t.Fatal("cached item still has DoneAt")
	}
}

func TestReplaceDoesNotFilterDoneOutsideImportantNotifications(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := store.MarkDone(item, item.DoneAt); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {item},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	data := store.Data()
	if len(data.FeedItemIDs[model.FeedMyPullRequests]) != 1 {
		t.Fatalf("my pull request count = %d, want 1", len(data.FeedItemIDs[model.FeedMyPullRequests]))
	}
	if data.Items[item.Key].Done {
		t.Fatal("my pull request item should not keep local done state")
	}
	if !data.Items[item.Key].DoneAt.IsZero() {
		t.Fatal("my pull request item should not keep DoneAt")
	}
	if _, ok := data.Done[item.Key]; !ok {
		t.Fatal("important done state should remain tracked separately")
	}
}
