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

func TestInitStartsOneAutomaticTimer(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", m.Init()())
	}
	if len(batch) != 4 {
		t.Fatalf("Init command count = %d, want window, color, initial refresh, and one timer", len(batch))
	}
}

func TestAutomaticRefreshKind(t *testing.T) {
	lastFull := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	m := Model{lastFullRefreshAt: lastFull}

	if got := m.automaticRefreshKind(lastFull.Add(4*time.Minute + 59*time.Second)); got != refreshNotifications {
		t.Fatalf("refresh kind before five minutes = %v, want notifications", got)
	}
	if got := m.automaticRefreshKind(lastFull.Add(5 * time.Minute)); got != refreshFull {
		t.Fatalf("refresh kind at five minutes = %v, want full", got)
	}
}

func TestNotificationRefreshCommandUsesCachedState(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	item := model.Item{Host: "github.com", Key: "github.com|one", Title: "one"}
	if err := store.Replace("me", "github.com", map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {item},
	}, refreshedAt); err != nil {
		t.Fatal(err)
	}
	service := &refreshServiceStub{}
	m := New(service, store, "github.com")

	msg, ok := m.notificationRefreshCmd()().(notificationRefreshMsg)
	if !ok {
		t.Fatalf("message type = %T, want notificationRefreshMsg", msg)
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if service.fullCalls != 0 || service.notificationCalls != 1 {
		t.Fatalf("full/notification calls = %d/%d, want 0/1", service.fullCalls, service.notificationCalls)
	}
	if service.request.Account != "me" || !service.request.Since.Equal(refreshedAt) || len(service.request.Existing) != 1 {
		t.Fatalf("request = %#v, want cached account, timestamp, and item", service.request)
	}
}

func TestNotificationRefreshPreservesOtherFeeds(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	oldImportant := model.Item{Host: "github.com", Key: "github.com|old", Title: "old"}
	pullRequest := model.Item{Host: "github.com", Key: "github.com|pr", Title: "pr", Type: model.ItemTypePullRequest}
	issue := model.Item{Host: "github.com", Key: "github.com|issue", Title: "issue", Type: model.ItemTypeIssue}
	feeds := map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {oldImportant},
		model.FeedMyPullRequests:         {pullRequest},
		model.FeedMyIssues:               {issue},
	}
	if err := store.Replace("me", "github.com", feeds, time.Now()); err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")
	m.rateWarning = "GraphQL rate limit low"
	newImportant := model.Item{Host: "github.com", Key: "github.com|new", Title: "new"}

	updatedModel, _ := m.Update(notificationRefreshMsg{result: github.NotificationRefreshResult{
		Account:     "me",
		Items:       []model.Item{oldImportant, newImportant},
		RateWarning: "REST rate limit low",
		RefreshedAt: time.Now(),
	}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updatedModel)
	}
	if len(updated.feeds[model.FeedImportantNotifications]) != 2 || len(updated.feeds[model.FeedMyPullRequests]) != 1 || len(updated.feeds[model.FeedMyIssues]) != 1 {
		t.Fatalf("feed counts = %d/%d/%d, want 2/1/1", len(updated.feeds[model.FeedImportantNotifications]), len(updated.feeds[model.FeedMyPullRequests]), len(updated.feeds[model.FeedMyIssues]))
	}
	if updated.rateWarning != "GraphQL rate limit low; REST rate limit low" {
		t.Fatalf("rate warning = %q, want prior GraphQL and new REST warnings", updated.rateWarning)
	}
}

type refreshServiceStub struct {
	fullCalls         int
	notificationCalls int
	request           github.NotificationRefreshRequest
}

func (s *refreshServiceStub) CurrentProgress() github.RefreshProgress {
	return github.RefreshProgress{}
}

func (s *refreshServiceStub) RateLimits(context.Context) (github.RateLimits, error) {
	return github.RateLimits{}, nil
}

func (s *refreshServiceStub) Refresh(context.Context) (github.RefreshResult, error) {
	s.fullCalls++
	return github.RefreshResult{}, nil
}

func (s *refreshServiceStub) RefreshNotifications(_ context.Context, request github.NotificationRefreshRequest) (github.NotificationRefreshResult, error) {
	s.notificationCalls++
	s.request = request
	return github.NotificationRefreshResult{
		Account:     request.Account,
		Items:       request.Existing,
		RefreshedAt: request.Since.Add(time.Minute),
	}, nil
}
