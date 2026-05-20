//nolint:testpackage // These tests exercise package internals.
package cache

import (
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
