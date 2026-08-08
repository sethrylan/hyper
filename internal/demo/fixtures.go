// Package demo provides deterministic data for recording the hyper demo.
package demo

import (
	"context"
	"strconv"
	"time"

	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

const (
	account = "mona"
	host    = "github.com"
)

var refreshedAt = time.Date(2000, time.January, 1, 15, 4, 0, 0, time.UTC)

// FixtureClient implements the TUI's GitHub service contract with deterministic fixtures.
type FixtureClient struct {
	feeds map[model.Feed][]model.Item
}

// NewFixtureClient creates a fixture client whose item ages are relative to now.
func NewFixtureClient(now time.Time) *FixtureClient {
	if now.IsZero() {
		now = time.Now()
	}
	return &FixtureClient{feeds: fixtureFeeds(now)}
}

// CurrentProgress returns no progress because fixture refreshes complete immediately.
func (*FixtureClient) CurrentProgress() github.RefreshProgress {
	return github.RefreshProgress{}
}

// RateLimits returns stable, healthy sample quotas.
func (*FixtureClient) RateLimits(ctx context.Context) (github.RateLimits, error) {
	if err := ctx.Err(); err != nil {
		return github.RateLimits{}, err
	}
	return github.RateLimits{
		Core:    github.RateLimitResource{Limit: 5000, Remaining: 4987, Used: 13},
		GraphQL: github.RateLimitResource{Limit: 5000, Remaining: 4921, Used: 79},
		Search:  github.RateLimitResource{Limit: 30, Remaining: 30},
	}, nil
}

// Refresh returns a fresh copy of every demo feed.
func (s *FixtureClient) Refresh(ctx context.Context) (github.RefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return github.RefreshResult{}, err
	}
	return github.RefreshResult{
		Account:     account,
		Feeds:       cloneFeeds(s.feeds),
		RefreshedAt: refreshedAt,
	}, nil
}

// RefreshNotifications returns a fresh copy of the Important Notifications fixtures.
func (s *FixtureClient) RefreshNotifications(ctx context.Context, _ github.NotificationRefreshRequest) (github.NotificationRefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return github.NotificationRefreshResult{}, err
	}
	return github.NotificationRefreshResult{
		Account:     account,
		Items:       cloneItems(s.feeds[model.FeedImportantNotifications]),
		RefreshedAt: refreshedAt,
	}, nil
}

func fixtureFeeds(now time.Time) map[model.Feed][]model.Item {
	hyperSelection := fixtureItem(now, "PR_demo_hyper_selection", account, "hyper", 9001,
		"Keep selection stable after refresh", model.ItemTypePullRequest, account, -18*time.Minute)
	hyperSelection.NotificationReason = "author"
	hyperSelection = withFeeds(hyperSelection, model.FeedImportantNotifications, model.FeedMyPullRequests)

	hyperDone := fixtureItem(now, "I_demo_hyper_done", account, "hyper", 9002,
		"Document local done behavior", model.ItemTypeIssue, account, -2*time.Hour)
	hyperDone.Assignees = []string{account}
	hyperDone.NotificationReason = "mention"
	hyperDone = withFeeds(hyperDone, model.FeedImportantNotifications, model.FeedMyIssues)

	grafanaProvisioning := fixtureItem(now, "PR_demo_grafana_provisioning", "grafana", "grafana", 99001,
		"Add service account support to provisioning", model.ItemTypePullRequest, "mariana", -47*time.Minute)
	grafanaProvisioning.NotificationReason = "review"
	grafanaProvisioning.ReviewRequests = []string{account}
	grafanaProvisioning = withFeeds(grafanaProvisioning, model.FeedImportantNotifications)

	grafanaVariables := fixtureItem(now, "I_demo_grafana_variables", "grafana", "grafana", 99002,
		"Dashboard variables fail after data source rename", model.ItemTypeIssue, "riley", -5*time.Hour)
	grafanaVariables.Assignees = []string{account}
	grafanaVariables.NotificationReason = "assign"
	grafanaVariables = withFeeds(grafanaVariables, model.FeedImportantNotifications)

	k6Thresholds := fixtureItem(now, "PR_demo_k6_thresholds", "grafana", "k6", 99003,
		"Expose threshold failures in JSON summary", model.ItemTypePullRequest, "daniel", -26*time.Hour)
	k6Thresholds.Merged = true
	k6Thresholds.NotificationReason = "review"
	k6Thresholds.Reviewers = []string{account}
	k6Thresholds.State = "merged"
	k6Thresholds = withFeeds(k6Thresholds, model.FeedImportantNotifications)

	k6Tags := fixtureItem(now, "I_demo_k6_tags", "grafana", "k6", 99004,
		"Preserve tags when retrying requests", model.ItemTypeIssue, "mia", -48*time.Hour)
	k6Tags.NotificationReason = "mention"
	k6Tags.State = "closed"
	k6Tags.StateReason = "not_planned"
	k6Tags = withFeeds(k6Tags, model.FeedImportantNotifications)

	hyperRateLimits := fixtureItem(now, "PR_demo_hyper_rate_limits", account, "hyper", 9003,
		"Add a rate-limit detail view", model.ItemTypePullRequest, account, -3*time.Hour)
	hyperRateLimits = withFeeds(hyperRateLimits, model.FeedMyPullRequests)

	grafanaSearch := fixtureItem(now, "PR_demo_grafana_search", "grafana", "grafana", 99005,
		"Reuse dashboard search credentials", model.ItemTypePullRequest, account, -8*time.Hour)
	grafanaSearch = withFeeds(grafanaSearch, model.FeedMyPullRequests)

	k6BrowserMetrics := fixtureItem(now, "PR_demo_k6_browser_metrics", "grafana", "k6", 99006,
		"Stream browser metrics during teardown", model.ItemTypePullRequest, account, -30*time.Hour)
	k6BrowserMetrics = withFeeds(k6BrowserMetrics, model.FeedMyPullRequests)

	k6ScenarioDocs := fixtureItem(now, "PR_demo_k6_scenario_docs", "grafana", "k6", 99007,
		"Document per-scenario thresholds", model.ItemTypePullRequest, account, -72*time.Hour)
	k6ScenarioDocs.Draft = true
	k6ScenarioDocs = withFeeds(k6ScenarioDocs, model.FeedMyPullRequests)

	hyperRefresh := fixtureItem(now, "I_demo_hyper_refresh", account, "hyper", 9004,
		"Support configurable refresh intervals", model.ItemTypeIssue, account, -7*time.Hour)
	hyperRefresh = withFeeds(hyperRefresh, model.FeedMyIssues)

	grafanaPanelSelection := fixtureItem(now, "I_demo_grafana_panel_selection", "grafana", "grafana", 99008,
		"Keep panel selection after dashboard refresh", model.ItemTypeIssue, account, -28*time.Hour)
	grafanaPanelSelection = withFeeds(grafanaPanelSelection, model.FeedMyIssues)

	k6RetryCounts := fixtureItem(now, "I_demo_k6_retry_counts", "grafana", "k6", 99009,
		"Expose retry counts in summaries", model.ItemTypeIssue, account, -54*time.Hour)
	k6RetryCounts = withFeeds(k6RetryCounts, model.FeedMyIssues)

	return map[model.Feed][]model.Item{
		model.FeedImportantNotifications: {
			hyperSelection,
			hyperDone,
			grafanaProvisioning,
			grafanaVariables,
			k6Thresholds,
			k6Tags,
		},
		model.FeedMyPullRequests: {
			hyperSelection,
			hyperRateLimits,
			grafanaSearch,
			k6BrowserMetrics,
			k6ScenarioDocs,
		},
		model.FeedMyIssues: {
			hyperDone,
			hyperRefresh,
			grafanaPanelSelection,
			k6RetryCounts,
		},
	}
}

func fixtureItem(now time.Time, nodeID, owner, repo string, number int, title string, itemType model.ItemType, author string, updatedAgo time.Duration) model.Item {
	pathType := "issues"
	if itemType == model.ItemTypePullRequest {
		pathType = "pull"
	}
	url := "https://github.com/" + owner + "/" + repo + "/" + pathType + "/" + strconv.Itoa(number)
	read := false
	return model.Item{
		AuthorLogin:     author,
		CreatedAt:       now.Add(updatedAgo - 7*24*time.Hour),
		Host:            host,
		Key:             model.StableKey(host, nodeID, url, title),
		NodeID:          nodeID,
		Read:            &read,
		RepositoryName:  repo,
		RepositoryOwner: owner,
		State:           "open",
		Title:           title,
		Type:            itemType,
		UpdatedAt:       now.Add(updatedAgo),
		URL:             url,
	}
}

func withFeeds(item model.Item, feeds ...model.Feed) model.Item {
	for _, feed := range feeds {
		item = item.WithFeed(feed)
	}
	return item
}

func cloneFeeds(feeds map[model.Feed][]model.Item) map[model.Feed][]model.Item {
	cloned := make(map[model.Feed][]model.Item, len(feeds))
	for feed, items := range feeds {
		cloned[feed] = cloneItems(items)
	}
	return cloned
}

func cloneItems(items []model.Item) []model.Item {
	cloned := make([]model.Item, len(items))
	for index, item := range items {
		item.Assignees = append([]string(nil), item.Assignees...)
		item.Reviewers = append([]string(nil), item.Reviewers...)
		item.ReviewRequests = append([]string(nil), item.ReviewRequests...)
		item.SourceFeeds = append([]model.Feed(nil), item.SourceFeeds...)
		if item.Read != nil {
			value := *item.Read
			item.Read = &value
		}
		if item.Saved != nil {
			value := *item.Saved
			item.Saved = &value
		}
		cloned[index] = item
	}
	return cloned
}
