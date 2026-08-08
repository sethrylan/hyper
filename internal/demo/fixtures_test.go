package demo_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/demo"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

func TestRefreshReturnsCuratedMultiRepoFeeds(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	result, err := demo.NewFixtureClient(now).Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantRefreshedAt := time.Date(2000, time.January, 1, 15, 4, 0, 0, time.UTC)
	if result.Account != "mona" || !result.RefreshedAt.Equal(wantRefreshedAt) {
		t.Fatalf("account/refreshed = %q/%s, want mona/%s", result.Account, result.RefreshedAt, wantRefreshedAt)
	}

	wantCounts := map[model.Feed]int{
		model.FeedImportantNotifications: 6,
		model.FeedMyPullRequests:         5,
		model.FeedMyIssues:               4,
	}
	for feed, want := range wantCounts {
		if got := len(result.Feeds[feed]); got != want {
			t.Fatalf("%s count = %d, want %d", feed, got, want)
		}
	}

	repositories := map[string]bool{}
	for _, item := range result.Feeds[model.FeedImportantNotifications] {
		repositories[item.Repository()] = true
		if !slices.Contains(item.SourceFeeds, model.FeedImportantNotifications) {
			t.Fatalf("item %q is missing Important source feed", item.Title)
		}
	}
	for _, want := range []string{"grafana/grafana", "grafana/k6", "mona/hyper"} {
		if !repositories[want] {
			t.Fatalf("Important fixtures are missing repository %q", want)
		}
	}

	first := result.Feeds[model.FeedImportantNotifications][0]
	if !first.UpdatedAt.Equal(now.Add(-18 * time.Minute)) {
		t.Fatalf("first updated at = %s, want an age relative to fixture construction", first.UpdatedAt)
	}
	if !slices.Contains(first.SourceFeeds, model.FeedMyPullRequests) {
		t.Fatalf("first item feeds = %v, want cross-feed PR", first.SourceFeeds)
	}
}

func TestRefreshReturnsDeepCopies(t *testing.T) {
	client := demo.NewFixtureClient(time.Now())
	first, err := client.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.Feeds[model.FeedImportantNotifications][0].Title = "changed"
	first.Feeds[model.FeedImportantNotifications][0].SourceFeeds[0] = model.FeedMyIssues

	second, err := client.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	item := second.Feeds[model.FeedImportantNotifications][0]
	if item.Title == "changed" || item.SourceFeeds[0] != model.FeedImportantNotifications {
		t.Fatalf("fixture mutated through returned data: %#v", item)
	}
}

func TestRefreshNotificationsIgnoresIncomingState(t *testing.T) {
	client := demo.NewFixtureClient(time.Now())
	result, err := client.RefreshNotifications(t.Context(), github.NotificationRefreshRequest{
		Account:  "someone-else",
		Existing: []model.Item{{Title: "unexpected"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Account != "mona" || len(result.Items) != 6 || result.Items[0].Title == "unexpected" {
		t.Fatalf("notification refresh = %#v, want canonical demo fixtures", result)
	}
}

func TestCanceledRequestsFail(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := demo.NewFixtureClient(time.Now())
	if _, err := client.Refresh(ctx); err == nil {
		t.Fatal("Refresh succeeded with a canceled context")
	}
	if _, err := client.RefreshNotifications(ctx, github.NotificationRefreshRequest{}); err == nil {
		t.Fatal("RefreshNotifications succeeded with a canceled context")
	}
	if _, err := client.RateLimits(ctx); err == nil {
		t.Fatal("RateLimits succeeded with a canceled context")
	}
}
