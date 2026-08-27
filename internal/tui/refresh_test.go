//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

func TestInitStartsTwoRefreshLanesAndOneHeartbeat(t *testing.T) {
	m := newRefreshTestModel(t)
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", m.Init()())
	}
	if len(batch) != 7 {
		t.Fatalf("Init command count = %d, want window, color, two refreshes, heartbeat, spinner, and rate limits", len(batch))
	}
	if !m.pullRequestsLoading || !m.backgroundLoading {
		t.Fatalf("initial refresh lanes = PR %t/background %t, want both active", m.pullRequestsLoading, m.backgroundLoading)
	}
}

func TestSelectedCadenceFitsGraphQLBudget(t *testing.T) {
	const (
		fastPullRequestCost = 1
		fullPullRequestCost = 20
		issueCost           = 1
		importantCost       = 34
	)
	used := int(time.Hour/pullRequestRefresh)*fastPullRequestCost +
		int(time.Hour/fullRefresh)*(fullPullRequestCost+issueCost+importantCost)
	if used >= 1250 {
		t.Fatalf("modeled hourly GraphQL usage = %d, want below 1250", used)
	}
}

func TestNotificationRefreshCommandUsesImportantCursor(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	item := model.Item{Key: "important", Title: "one"}
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {item},
	}, refreshedAt); err != nil {
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
	if service.request.Account != "me" || !service.request.Since.Equal(refreshedAt) || len(service.request.Existing) != 1 {
		t.Fatalf("request = %#v, want cached account, Important cursor, and item", service.request)
	}
}

func TestPullRequestRefreshMergesNewestPageIntoCachedFeed(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Now()
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {{Key: "important"}},
		model.FeedMyPullRequests:         {{Key: "old"}, {Key: "updated", Title: "stale"}},
		model.FeedMyIssues:               {{Key: "issue"}},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")
	newPR := model.Item{Key: "new", Type: model.ItemTypePullRequest}
	updatedPR := model.Item{Key: "updated", Title: "fresh", Type: model.ItemTypePullRequest}

	updatedModel, _ := m.Update(feedRefreshMsg{kind: refreshPullRequests, result: github.FeedRefreshResult{
		Feed: model.FeedMyPullRequests, Items: []model.Item{updatedPR, newPR}, RefreshedAt: refreshedAt.Add(time.Second),
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if got := updated.feeds[model.FeedMyPullRequests]; len(got) != 3 || got[0].Key != "old" || got[1].Title != "fresh" || got[2].Key != newPR.Key {
		t.Fatalf("pull requests = %#v", got)
	}
	if len(updated.feeds[model.FeedImportantNotifications]) != 1 || len(updated.feeds[model.FeedMyIssues]) != 1 {
		t.Fatalf("unrelated feeds changed: %#v", updated.feeds)
	}
}

func TestBackgroundRefreshDoesNotReplacePullRequests(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Now()
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {{Key: "fresh-pr"}},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	updatedModel, _ := m.Update(backgroundRefreshMsg{result: github.RefreshResult{
		Account: "me",
		Feeds: map[model.Feed][]model.Item{
			model.FeedImportantNotifications: {{Key: "important"}},
			model.FeedMyIssues:               {{Key: "issue"}},
		},
		RefreshedAt: refreshedAt.Add(time.Minute),
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if got := updated.feeds[model.FeedMyPullRequests]; len(got) != 1 || got[0].Key != "fresh-pr" {
		t.Fatalf("background refresh replaced pull requests: %#v", got)
	}
}

func TestAuthoritativeBackgroundRefreshReconcilesPullRequests(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Now()
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {{Key: "closed-since-last-full-refresh"}},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	updatedModel, _ := m.Update(backgroundRefreshMsg{result: github.RefreshResult{
		Account: "me",
		Feeds: map[model.Feed][]model.Item{
			model.FeedImportantNotifications: nil,
			model.FeedMyPullRequests:         {{Key: "still-open"}},
			model.FeedMyIssues:               nil,
		},
		RefreshedAt: refreshedAt.Add(time.Minute),
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if got := updated.feeds[model.FeedMyPullRequests]; len(got) != 1 || got[0].Key != "still-open" {
		t.Fatalf("pull requests = %#v, want authoritative replacement", got)
	}
}

func TestRefreshWarningsAreReplacedPerLaneAndCleared(t *testing.T) {
	m := newRefreshTestModel(t)
	now := time.Now()

	updatedModel, _ := m.Update(feedRefreshMsg{kind: refreshPullRequests, result: github.FeedRefreshResult{
		Feed: model.FeedMyPullRequests, RateWarning: "GraphQL remaining: 99", RefreshedAt: now,
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	updatedModel, _ = updated.Update(backgroundRefreshMsg{result: github.RefreshResult{
		Account: "me", Feeds: emptyFeeds(), RateWarning: "REST remaining: 10", RefreshedAt: now,
	}})
	updated, ok = updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if got := updated.rateWarning(); got != "GraphQL remaining: 99; REST remaining: 10" {
		t.Fatalf("warnings = %q, want one warning per lane", got)
	}

	updatedModel, _ = updated.Update(feedRefreshMsg{kind: refreshPullRequests, result: github.FeedRefreshResult{
		Feed: model.FeedMyPullRequests, RefreshedAt: now.Add(time.Second),
	}})
	updated, ok = updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if got := updated.rateWarning(); got != "REST remaining: 10" {
		t.Fatalf("warnings after healthy PR refresh = %q, want background warning only", got)
	}
}

func TestHeartbeatStartsDuePullRequestsAndNotifications(t *testing.T) {
	m := newRefreshTestModel(t)
	m.pullRequestsLoading = false
	m.backgroundLoading = false
	now := time.Now()
	m.nextPullRequests = now
	m.nextNotifications = now
	m.nextBackground = now.Add(time.Minute)

	updatedModel, cmd := m.Update(tickMsg{at: now})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if !updated.pullRequestsLoading || !updated.backgroundLoading {
		t.Fatalf("due lanes = PR %t/background %t, want both active", updated.pullRequestsLoading, updated.backgroundLoading)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("heartbeat command type = %T, want tea.BatchMsg", cmd())
	}
	if _, ok := batch[1]().(feedRefreshMsg); !ok {
		t.Fatalf("first due command = %T, want feedRefreshMsg", batch[1]())
	}
	if msg, ok := batch[2]().(feedRefreshMsg); !ok || msg.kind != refreshNotifications {
		t.Fatalf("second due command = %#v, want notification refresh", msg)
	}
}

func TestAuthoritativeBackgroundRefreshTakesPrecedence(t *testing.T) {
	m := newRefreshTestModel(t)
	m.pullRequestsLoading = true
	m.backgroundLoading = false
	now := time.Now()
	m.nextNotifications = now
	m.nextBackground = now

	_, cmd := m.Update(tickMsg{at: now})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("heartbeat command type = %T, want tea.BatchMsg", cmd())
	}
	if _, ok := batch[1]().(backgroundRefreshMsg); !ok {
		t.Fatalf("due background command = %T, want authoritative refresh", batch[1]())
	}
}

func TestHeartbeatDoesNotOverlapEitherLane(t *testing.T) {
	m := newRefreshTestModel(t)
	now := time.Now()
	m.nextPullRequests = now
	m.nextNotifications = now
	m.nextBackground = now
	wantPullRequests := m.nextPullRequests
	wantNotifications := m.nextNotifications
	wantBackground := m.nextBackground

	updatedModel, cmd := m.Update(tickMsg{at: now})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if !updated.pullRequestsLoading || !updated.backgroundLoading {
		t.Fatal("running lanes should remain active")
	}
	if cmd == nil {
		t.Fatal("next heartbeat was not scheduled")
	}
	if !updated.nextPullRequests.Equal(wantPullRequests) ||
		!updated.nextNotifications.Equal(wantNotifications) ||
		!updated.nextBackground.Equal(wantBackground) {
		t.Fatalf("running lanes advanced their schedules: %#v", updated)
	}
}

func TestRefreshErrorPreservesCachedFeed(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceFeeds("me", "github.com", map[model.Feed][]model.Item{
		model.FeedMyPullRequests: {{Key: "cached"}},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	updatedModel, _ := m.Update(feedRefreshMsg{kind: refreshPullRequests, err: errors.New("budget exhausted")})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want Model", updatedModel)
	}
	if got := updated.feeds[model.FeedMyPullRequests]; len(got) != 1 || got[0].Key != "cached" {
		t.Fatalf("cached pull requests changed after error: %#v", got)
	}
	if updated.pullRequestsLoading || !strings.Contains(updated.status, "refresh deferred") {
		t.Fatalf("refresh error state = loading %t/status %q", updated.pullRequestsLoading, updated.status)
	}
}

func TestManualRefreshTargetsLane(t *testing.T) {
	m := newRefreshTestModel(t)
	m.pullRequestsLoading = false
	m.backgroundLoading = true
	m.activeFeed = 1

	updated, cmd := m.handleAction(ActionRefresh)
	if cmd == nil || !updated.pullRequestsLoading || !updated.backgroundLoading {
		t.Fatalf("manual PR refresh did not start beside background lane: %#v", updated)
	}

	updated.backgroundLoading = false
	updated.activeFeed = 2
	updated, cmd = updated.handleAction(ActionRefresh)
	if cmd == nil || !updated.backgroundLoading {
		t.Fatal("manual issue refresh did not start authoritative background lane")
	}
	if _, ok := cmd().(backgroundRefreshMsg); !ok {
		t.Fatalf("manual issue command = %T, want backgroundRefreshMsg", cmd())
	}
}

func newRefreshTestModel(t *testing.T) Model {
	t.Helper()
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(&refreshServiceStub{}, store, "github.com")
}

type refreshServiceStub struct {
	backgroundCalls   int
	notificationCalls int
	pullRequestCalls  int
	request           github.NotificationRefreshRequest
}

func (s *refreshServiceStub) RateLimits(context.Context) (github.RateLimits, error) {
	return github.RateLimits{}, nil
}

func (s *refreshServiceStub) RefreshBackground(context.Context) (github.RefreshResult, error) {
	s.backgroundCalls++
	return github.RefreshResult{
		Account: "me",
		Feeds: map[model.Feed][]model.Item{
			model.FeedImportantNotifications: nil,
			model.FeedMyIssues:               nil,
		},
		RefreshedAt: time.Now(),
	}, nil
}

func (s *refreshServiceStub) RefreshNotifications(_ context.Context, request github.NotificationRefreshRequest) (github.FeedRefreshResult, error) {
	s.notificationCalls++
	s.request = request
	return github.FeedRefreshResult{
		Account: request.Account, Feed: model.FeedImportantNotifications, Items: request.Existing, RefreshedAt: time.Now(),
	}, nil
}

func (s *refreshServiceStub) RefreshPullRequests(context.Context) (github.FeedRefreshResult, error) {
	s.pullRequestCalls++
	return github.FeedRefreshResult{Feed: model.FeedMyPullRequests, RefreshedAt: time.Now()}, nil
}
