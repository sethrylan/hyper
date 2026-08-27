//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

func TestInitStartsIndependentRefreshesAndTimers(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", m.Init()())
	}
	if len(batch) != 13 {
		t.Fatalf("Init command count = %d, want window, color, four initial refreshes, five timers, progress, and rate limits", len(batch))
	}
	for _, kind := range initialRefreshes() {
		if !m.refreshLoading(kind) {
			t.Fatalf("initial refresh %s is not marked active", refreshName(kind))
		}
	}
}

func TestRefreshIntervals(t *testing.T) {
	want := map[refreshKind]time.Duration{
		refreshPullRequests:  pullRequestRefresh,
		refreshNotifications: notificationRefresh,
		refreshIssues:        issueRefresh,
		refreshMetadata:      metadataRefresh,
		refreshImportant:     fullRefresh,
	}
	for kind, interval := range want {
		if got := refreshInterval(kind); got != interval {
			t.Fatalf("refreshInterval(%s) = %s, want %s", refreshName(kind), got, interval)
		}
	}
}

func TestSelectedCadenceFitsGraphQLBudget(t *testing.T) {
	const (
		fastPullRequestCost = 1
		issueCost           = 1
		metadataCost        = 1
		// The authoritative refresh uses eleven three-point supplemental
		// searches plus one notification-enrichment query.
		importantCost = 34
	)
	used := int(time.Hour/pullRequestRefresh)*fastPullRequestCost +
		int(time.Hour/issueRefresh)*issueCost +
		int(time.Hour/metadataRefresh)*metadataCost +
		int(time.Hour/fullRefresh)*importantCost
	if used >= 1250 {
		t.Fatalf("modeled hourly GraphQL usage = %d, want below 1250", used)
	}
}

func TestNotificationRefreshCommandUsesFeedTimestamp(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	item := model.Item{Host: "github.com", Key: "github.com|PR_one", NodeID: "PR_one", Title: "one", Type: model.ItemTypePullRequest}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {item},
		model.FeedMyPullRequests:         {item},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}
	newerPRRefresh := refreshedAt.Add(time.Minute)
	if err := store.ReplaceFeed("me", "github.com", model.FeedMyPullRequests, []model.Item{item}, newerPRRefresh); err != nil {
		t.Fatal(err)
	}
	service := &refreshServiceStub{}
	m := New(service, store, "github.com")

	msg, ok := m.refreshCmd(refreshNotifications)().(feedRefreshMsg)
	if !ok {
		t.Fatalf("message type = %T, want feedRefreshMsg", msg)
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if service.notificationCalls != 1 {
		t.Fatalf("notification calls = %d, want 1", service.notificationCalls)
	}
	if service.request.Account != "me" || !service.request.Since.Equal(refreshedAt) || len(service.request.Existing) != 1 {
		t.Fatalf("request = %#v, want cached account, Important timestamp, and item", service.request)
	}
}

func TestFeedRefreshUpdatesOnlyCompletedFeed(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	oldImportant := model.Item{Host: "github.com", Key: "github.com|important", Title: "important"}
	oldPR := model.Item{Host: "github.com", Key: "github.com|old", Title: "old", Type: model.ItemTypePullRequest}
	issue := model.Item{Host: "github.com", Key: "github.com|issue", Title: "issue", Type: model.ItemTypeIssue}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {oldImportant},
		model.FeedMyPullRequests:         {oldPR},
		model.FeedMyIssues:               {issue},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")
	newPR := model.Item{Host: "github.com", Key: "github.com|new", Title: "new", Type: model.ItemTypePullRequest}

	updatedModel, _ := m.Update(feedRefreshMsg{kind: refreshPullRequests, result: github.FeedRefreshResult{
		Feed: model.FeedMyPullRequests, Items: []model.Item{newPR}, RefreshedAt: time.Now(),
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if len(updated.feeds[model.FeedMyPullRequests]) != 1 || updated.feeds[model.FeedMyPullRequests][0].Key != newPR.Key {
		t.Fatalf("pull requests = %#v, want only new result", updated.feeds[model.FeedMyPullRequests])
	}
	if len(updated.feeds[model.FeedImportantNotifications]) != 1 || len(updated.feeds[model.FeedMyIssues]) != 1 {
		t.Fatalf("unrelated feeds changed: %#v", updated.feeds)
	}
}

func TestMetadataRefreshUpdatesPullRequestsWithoutAdvancingFeedTimestamp(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	pr := model.Item{Host: "github.com", Key: "github.com|PR_one", NodeID: "PR_one", Title: "old", Type: model.ItemTypePullRequest}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {pr},
		model.FeedMyPullRequests:         {pr},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	updatedModel, _ := m.Update(metadataRefreshMsg{result: github.PullRequestMetadataResult{
		PullRequests: []github.PullRequestMetadata{{NodeID: "PR_one", Title: "new", State: "MERGED", Merged: true}},
		RefreshedAt:  refreshedAt.Add(time.Minute),
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	for _, feed := range []model.Feed{model.FeedImportantNotifications, model.FeedMyPullRequests} {
		item := updated.feeds[feed][0]
		if item.Title != "new" || !item.Merged {
			t.Fatalf("%s item = %#v, want updated metadata", feed, item)
		}
		if got := store.Data().LastRefreshByFeed[feed]; !got.Equal(refreshedAt) {
			t.Fatalf("%s timestamp = %s, want %s", feed, got, refreshedAt)
		}
	}
}

func TestManualRefreshTargetsActiveFeedWhileOtherJobsLoad(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")
	m.loadingRefreshes = map[refreshKind]time.Time{refreshImportant: time.Now()}
	m.loading = true
	m.activeFeed = 1

	updated, cmd := m.handleAction(ActionRefresh)
	if cmd == nil || !updated.refreshLoading(refreshPullRequests) || !updated.refreshLoading(refreshImportant) {
		t.Fatalf("manual refresh did not start PR job alongside existing work: %#v", updated.loadingRefreshes)
	}
}

type refreshServiceStub struct {
	importantCalls    int
	issueCalls        int
	metadataCalls     int
	notificationCalls int
	pullRequestCalls  int
	request           github.NotificationRefreshRequest
}

func (s *refreshServiceStub) CurrentProgress() github.RefreshProgress {
	return github.RefreshProgress{}
}
func (s *refreshServiceStub) RateLimits(context.Context) (github.RateLimits, error) {
	return github.RateLimits{}, nil
}
func (s *refreshServiceStub) RefreshImportant(_ context.Context, account string) (github.FeedRefreshResult, error) {
	s.importantCalls++
	return github.FeedRefreshResult{Account: account, Feed: model.FeedImportantNotifications, RefreshedAt: time.Now()}, nil
}
func (s *refreshServiceStub) RefreshIncrementalNotifications(_ context.Context, request github.NotificationRefreshRequest) (github.FeedRefreshResult, error) {
	s.notificationCalls++
	s.request = request
	return github.FeedRefreshResult{Account: request.Account, Feed: model.FeedImportantNotifications, Items: request.Existing, RefreshedAt: request.Since.Add(notificationRefresh)}, nil
}
func (s *refreshServiceStub) RefreshIssues(context.Context) (github.FeedRefreshResult, error) {
	s.issueCalls++
	return github.FeedRefreshResult{Feed: model.FeedMyIssues, RefreshedAt: time.Now()}, nil
}
func (s *refreshServiceStub) RefreshPullRequestMetadata(context.Context, []string) (github.PullRequestMetadataResult, error) {
	s.metadataCalls++
	return github.PullRequestMetadataResult{RefreshedAt: time.Now()}, nil
}
func (s *refreshServiceStub) RefreshPullRequests(context.Context) (github.FeedRefreshResult, error) {
	s.pullRequestCalls++
	return github.FeedRefreshResult{Feed: model.FeedMyPullRequests, RefreshedAt: time.Now()}, nil
}
