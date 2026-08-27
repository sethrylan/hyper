//nolint:revive // Internal package exports are shared across command and tests.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sethrylan/hyper/internal/filter"
	"github.com/sethrylan/hyper/internal/model"
	"github.com/sethrylan/hyper/internal/quota"
)

const (
	maxNotifications     = 500
	notificationPageSize = 50
	maxSearchResults     = 2000
	maxRetries           = 10
	maxRetryDelay        = 60 * time.Second
	fastPullRequestPoll  = 5 * time.Second

	notificationReasonMention = "MENTION"
)

var errGraphQLRateLimitExhausted = errors.New("github graphql rate limit exhausted")

type pullRequestPriorityKey struct{}

type Client struct {
	budget     *quota.Manager
	host       string
	httpClient *http.Client
	retrySleep func(context.Context, time.Duration) error
	token      string
}

type RefreshResult struct {
	Account     string
	Feeds       map[model.Feed][]model.Item
	RateWarning string
	RefreshedAt time.Time
}

type FeedRefreshResult struct {
	Account     string
	Feed        model.Feed
	Items       []model.Item
	RateWarning string
	RefreshedAt time.Time
}

type NotificationRefreshRequest struct {
	Account  string
	Existing []model.Item
	Since    time.Time
}

type RateLimits struct {
	Core    RateLimitResource
	GraphQL RateLimitResource
	Search  RateLimitResource
}

type RateLimitResource struct {
	Limit     int
	Remaining int
	ResetAt   time.Time
	Used      int
}

type restRateLimitResource struct {
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"`
	Used      int   `json:"used"`
}

func (r restRateLimitResource) Resource() RateLimitResource {
	return RateLimitResource{
		Limit:     r.Limit,
		Remaining: r.Remaining,
		ResetAt:   time.Unix(r.Reset, 0),
		Used:      r.Used,
	}
}

func NewClient(host, token string) *Client {
	return &Client{
		host:       host,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		retrySleep: sleepContext,
		token:      token,
	}
}

func NewBudgetedClient(host, token string, budget *quota.Manager) *Client {
	client := NewClient(host, token)
	client.budget = budget
	return client
}

func (c *Client) RateLimits(ctx context.Context) (RateLimits, error) {
	var restResponse struct {
		Resources struct {
			Core    restRateLimitResource `json:"core"`
			GraphQL restRateLimitResource `json:"graphql"`
			Search  restRateLimitResource `json:"search"`
		} `json:"resources"`
	}
	if _, err := c.rest(ctx, http.MethodGet, fmt.Sprintf("https://api.%s/rate_limit", c.host), nil, &restResponse); err != nil {
		return RateLimits{}, fmt.Errorf("fetch REST rate limits: %w", err)
	}
	if c.budget != nil {
		now := time.Now()
		for resource, limit := range map[quota.Resource]restRateLimitResource{
			quota.ResourceCore:    restResponse.Resources.Core,
			quota.ResourceGraphQL: restResponse.Resources.GraphQL,
			quota.ResourceSearch:  restResponse.Resources.Search,
		} {
			if err := c.budget.Configure(resource, limit.Limit, time.Unix(limit.Reset, 0), now); err != nil {
				return RateLimits{}, fmt.Errorf("configure Hyper API budget: %w", err)
			}
		}
	}

	limits := RateLimits{
		Core:    restResponse.Resources.Core.Resource(),
		GraphQL: restResponse.Resources.GraphQL.Resource(),
		Search:  restResponse.Resources.Search.Resource(),
	}
	return limits, nil
}

func (c *Client) RefreshPullRequests(ctx context.Context) (FeedRefreshResult, error) {
	now := time.Now()
	items, warning, err := c.searchFirstPage(context.WithValue(ctx, pullRequestPriorityKey{}, true), model.FeedMyPullRequests, now)
	if err != nil {
		return FeedRefreshResult{}, err
	}
	return FeedRefreshResult{Feed: model.FeedMyPullRequests, Items: items, RateWarning: warning, RefreshedAt: now}, nil
}

func (c *Client) RefreshNotifications(ctx context.Context, request NotificationRefreshRequest) (FeedRefreshResult, error) {
	now := time.Now()
	account, warning, err := c.resolveAccount(ctx, request.Account)
	if err != nil {
		return FeedRefreshResult{}, err
	}
	items, notificationWarning, err := c.fetchRESTNotificationItems(ctx, request.Since)
	if err != nil {
		return FeedRefreshResult{}, err
	}
	return FeedRefreshResult{
		Account:     account,
		Feed:        model.FeedImportantNotifications,
		Items:       mergeShortImportantItems(request.Existing, items, account),
		RateWarning: joinWarning(warning, notificationWarning),
		RefreshedAt: now,
	}, nil
}

func (c *Client) VerifyAccount(ctx context.Context, expected string) error {
	account, _, err := c.resolveAccount(ctx, "")
	if err != nil {
		return err
	}
	if !strings.EqualFold(account, expected) {
		return fmt.Errorf("authenticated token belongs to %q, but GitHub CLI active account is %q", account, expected)
	}
	return nil
}

func (c *Client) resolveAccount(ctx context.Context, account string) (string, string, error) {
	if account != "" {
		return account, "", nil
	}
	var response struct {
		Login string `json:"login"`
	}
	remaining, err := c.rest(ctx, http.MethodGet, fmt.Sprintf("https://api.%s/user", c.host), nil, &response)
	if err != nil {
		return "", "", fmt.Errorf("fetch authenticated user: %w", err)
	}
	return response.Login, rateWarning(remaining), nil
}

func (c *Client) RefreshBackground(ctx context.Context) (RefreshResult, error) {
	now := time.Now()
	verifiedAccount, rateWarning, err := c.resolveAccount(ctx, "")
	if err != nil {
		return RefreshResult{}, err
	}

	refreshCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type feedRefreshResult struct {
		err     error
		feed    model.Feed
		items   []model.Item
		warning string
	}

	results := make(chan feedRefreshResult, 3)
	startFeedRefresh := func(feed model.Feed, refresh func(context.Context) ([]model.Item, string, error)) {
		go func() {
			items, warning, err := refresh(refreshCtx)
			if err != nil {
				cancel()
			}
			results <- feedRefreshResult{
				err:     err,
				feed:    feed,
				items:   items,
				warning: warning,
			}
		}()
	}

	startFeedRefresh(model.FeedMyIssues, func(ctx context.Context) ([]model.Item, string, error) {
		return c.search(ctx, model.FeedMyIssues, now)
	})
	startFeedRefresh(model.FeedMyPullRequests, func(ctx context.Context) ([]model.Item, string, error) {
		return c.search(ctx, model.FeedMyPullRequests, now)
	})
	startFeedRefresh(model.FeedImportantNotifications, func(ctx context.Context) ([]model.Item, string, error) {
		return c.fetchNotifications(ctx, verifiedAccount)
	})

	feeds := map[model.Feed][]model.Item{}
	var firstErr error
	for range 3 {
		result := <-results
		if result.err != nil {
			if firstErr == nil || errors.Is(firstErr, context.Canceled) {
				firstErr = result.err
			}
			continue
		}
		rateWarning = joinWarning(rateWarning, result.warning)
		feeds[result.feed] = result.items
	}
	if firstErr != nil {
		return RefreshResult{}, firstErr
	}

	return RefreshResult{
		Account:     verifiedAccount,
		Feeds:       feeds,
		RateWarning: rateWarning,
		RefreshedAt: now,
	}, nil
}

func (c *Client) fetchNotifications(ctx context.Context, me string) ([]model.Item, string, error) {
	notificationItems, warning, err := c.fetchRESTNotificationItems(ctx, time.Time{})
	if err != nil {
		return nil, warning, err
	}
	enrichedItems, enrichWarning, err := c.enrichItems(ctx, notificationItems)
	if err != nil {
		warning = joinWarning(warning, "notification subject enrichment failed: "+err.Error())
		enrichedItems = notificationItems
	} else {
		warning = joinWarning(warning, enrichWarning)
	}
	items := importantItems(enrichedItems, me)

	supplemental, supplementalWarning := c.fetchSupplementalImportantItems(ctx, me, time.Now())
	warning = joinWarning(warning, supplementalWarning)
	items = mergeItems(items, importantItems(supplemental, me))
	for i := range items {
		items[i] = items[i].WithFeed(model.FeedImportantNotifications)
	}
	return items, warning, nil
}

func (c *Client) fetchRESTNotificationItems(ctx context.Context, since time.Time) ([]model.Item, string, error) {
	notifications := make([]restNotification, 0, maxNotifications)
	var warning string
	for page := 1; len(notifications) < maxNotifications; page++ {
		query := url.Values{
			"all":      {"true"},
			"page":     {strconv.Itoa(page)},
			"per_page": {strconv.Itoa(notificationPageSize)},
		}
		if !since.IsZero() {
			query.Set("since", since.Add(-time.Second).UTC().Format(time.RFC3339))
		}
		endpoint := fmt.Sprintf("https://api.%s/notifications?%s", c.host, query.Encode())
		var pageNotifications []restNotification
		remaining, err := c.rest(ctx, http.MethodGet, endpoint, nil, &pageNotifications)
		if err != nil {
			return nil, warning, fmt.Errorf("fetch notifications: %w", err)
		}
		warning = joinWarning(warning, rateWarning(remaining))
		if len(pageNotifications) == 0 {
			break
		}
		remainingCap := maxNotifications - len(notifications)
		if len(pageNotifications) > remainingCap {
			pageNotifications = pageNotifications[:remainingCap]
		}
		notifications = append(notifications, pageNotifications...)
		if len(pageNotifications) < notificationPageSize {
			break
		}
	}
	notificationItems := make([]model.Item, 0, len(notifications))
	for _, notification := range notifications {
		notificationItems = append(notificationItems, c.restNotificationItem(ctx, notification))
	}
	return notificationItems, warning, nil
}

func (c *Client) restNotificationItem(ctx context.Context, n restNotification) model.Item {
	read := !n.Unread
	item := model.Item{
		Host:                 c.host,
		Key:                  model.StableKey(c.host, "", n.Subject.URL, n.ID),
		NotificationReason:   n.Reason,
		NotificationThreadID: n.ID,
		Read:                 &read,
		RepositoryName:       n.Repository.Name,
		RepositoryOwner:      n.Repository.Owner.Login,
		Title:                n.Subject.Title,
		Type:                 notificationSubjectType(n.Subject.Type),
		UpdatedAt:            n.UpdatedAt,
		URL:                  c.subjectHTMLURL(n.Subject.URL),
	}
	if n.Repository.FullName != "" && item.RepositoryOwner == "" {
		parts := strings.SplitN(n.Repository.FullName, "/", 2)
		if len(parts) == 2 {
			item.RepositoryOwner = parts[0]
			item.RepositoryName = parts[1]
		}
	}
	if n.Subject.URL == "" {
		return item
	}
	var detail restSubjectDetail
	if _, err := c.rest(ctx, http.MethodGet, n.Subject.URL, nil, &detail); err != nil {
		return item
	}
	item.AuthorLogin = detail.User.Login
	item.CreatedAt = detail.CreatedAt
	item.Draft = detail.Draft
	item.Merged = detail.Merged
	item.NodeID = detail.NodeID
	item.State = detail.State
	item.StateReason = detail.StateReason
	item.Key = model.StableKey(c.host, detail.NodeID, detail.HTMLURL, n.ID)
	if detail.HTMLURL != "" {
		item.URL = detail.HTMLURL
	}
	if !detail.UpdatedAt.IsZero() {
		item.UpdatedAt = detail.UpdatedAt
	}
	item.Assignees = logins(detail.Assignees)
	item.ReviewRequests = logins(detail.RequestedReviewers)
	return item
}

func importantItems(items []model.Item, me string) []model.Item {
	result := make([]model.Item, 0, len(items))
	for _, item := range items {
		if filter.Important(item, me) {
			result = append(result, item)
		}
	}
	return result
}

func (c *Client) enrichItems(ctx context.Context, items []model.Item) ([]model.Item, string, error) {
	subjects, warning, err := c.fetchSubjectsByNodeID(ctx, itemNodeIDs(items))
	if err != nil {
		return nil, warning, err
	}
	enriched := make([]model.Item, 0, len(items))
	for _, item := range items {
		subject, ok := subjects[item.NodeID]
		if ok {
			item = mergeItem(item, subject.Item(c.host))
		}
		enriched = append(enriched, item)
	}
	return enriched, warning, nil
}

func itemNodeIDs(items []model.Item) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.NodeID == "" {
			continue
		}
		if _, ok := seen[item.NodeID]; ok {
			continue
		}
		seen[item.NodeID] = struct{}{}
		ids = append(ids, item.NodeID)
	}
	return ids
}

func (c *Client) fetchSubjectsByNodeID(ctx context.Context, ids []string) (map[string]nodeSubject, string, error) {
	subjects := map[string]nodeSubject{}
	var warning string
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		var response nodesResponse
		if err := c.graphQL(ctx, nodesGraphQL, map[string]any{"ids": ids[start:end]}, &response); err != nil {
			return nil, warning, err
		}
		warning = joinWarning(warning, rateWarning(response.Data.RateLimit.Remaining))
		for _, node := range response.Data.Nodes {
			if node == nil || node.ID == "" {
				continue
			}
			subjects[node.ID] = *node
		}
	}
	return subjects, warning, nil
}

func (c *Client) fetchSupplementalImportantItems(ctx context.Context, me string, now time.Time) ([]model.Item, string) {
	queries := supplementalImportantQueries(now)
	var items []model.Item
	var warning string
	for _, source := range queries {
		queryItems, queryWarning, err := c.searchQuery(ctx, source.Query, maxSearchResults/len(queries))
		if err != nil {
			warning = joinWarning(warning, "supplemental search failed: "+err.Error())
			if errors.Is(err, errGraphQLRateLimitExhausted) {
				break
			}
			continue
		}
		warning = joinWarning(warning, queryWarning)
		queryItems = withNotificationReason(queryItems, source.NotificationReason)
		items = mergeItems(items, queryItems)
	}
	return items, warning
}

type supplementalImportantQuery struct {
	NotificationReason string
	Query              string
}

func supplementalImportantQueries(now time.Time) []supplementalImportantQuery {
	cutoff := now.AddDate(0, 0, -30).Format("2006-01-02")
	return []supplementalImportantQuery{
		{Query: "is:open archived:false author:@me updated:>" + cutoff},
		{Query: "is:open archived:false assignee:@me updated:>" + cutoff},
		{Query: "is:open is:pr archived:false review-requested:@me updated:>" + cutoff},
		{Query: "is:open archived:false involves:@me updated:>" + cutoff},
		{Query: "is:closed is:pr archived:false author:@me updated:>" + cutoff},
		{Query: "is:closed is:pr archived:false assignee:@me updated:>" + cutoff},
		{Query: "is:closed is:pr archived:false involves:@me updated:>" + cutoff},
		{Query: "is:closed is:issue archived:false author:@me updated:>" + cutoff},
		{Query: "is:closed is:issue archived:false assignee:@me updated:>" + cutoff},
		{Query: "is:closed is:issue archived:false involves:@me updated:>" + cutoff},
		{NotificationReason: notificationReasonMention, Query: "archived:false mentions:@me updated:>" + cutoff},
	}
}

func withNotificationReason(items []model.Item, reason string) []model.Item {
	if reason == "" {
		return items
	}
	for i := range items {
		items[i].NotificationReason = reason
	}
	return items
}

func mergeItems(base, incoming []model.Item) []model.Item {
	merged := make([]model.Item, 0, len(base)+len(incoming))
	index := map[string]int{}
	for _, item := range base {
		index[item.Key] = len(merged)
		merged = append(merged, item)
	}
	for _, item := range incoming {
		if existingIndex, ok := index[item.Key]; ok {
			merged[existingIndex] = mergeItem(merged[existingIndex], item)
			continue
		}
		index[item.Key] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func mergeShortImportantItems(existing, changed []model.Item, me string) []model.Item {
	items := append([]model.Item(nil), existing...)
	index := make(map[string]int, len(items))
	threadIndex := make(map[string]int, len(items))
	for i, item := range items {
		index[item.Key] = i
		if item.NotificationThreadID != "" {
			threadIndex[item.NotificationThreadID] = i
		}
	}
	for _, item := range changed {
		existingIndex, ok := index[item.Key]
		if !ok && item.NotificationThreadID != "" {
			existingIndex, ok = threadIndex[item.NotificationThreadID]
		}
		if ok {
			item = mergeItem(item, items[existingIndex])
			items[existingIndex] = item.WithFeed(model.FeedImportantNotifications)
			index[item.Key] = existingIndex
			continue
		}
		if !filter.Important(item, me) {
			continue
		}
		item = item.WithFeed(model.FeedImportantNotifications)
		index[item.Key] = len(items)
		if item.NotificationThreadID != "" {
			threadIndex[item.NotificationThreadID] = len(items)
		}
		items = append(items, item)
	}
	return items
}

func mergeItem(base, incoming model.Item) model.Item {
	if base.AuthorLogin == "" {
		base.AuthorLogin = incoming.AuthorLogin
	}
	if base.CreatedAt.IsZero() {
		base.CreatedAt = incoming.CreatedAt
	}
	base.Draft = base.Draft || incoming.Draft
	base.Merged = base.Merged || incoming.Merged
	if base.NodeID == "" {
		base.NodeID = incoming.NodeID
		base.Key = model.StableKey(base.Host, base.NodeID, base.URL, base.Key)
	}
	if base.RepositoryName == "" {
		base.RepositoryName = incoming.RepositoryName
	}
	if base.RepositoryOwner == "" {
		base.RepositoryOwner = incoming.RepositoryOwner
	}
	if base.State == "" {
		base.State = incoming.State
	}
	if base.StateReason == "" {
		base.StateReason = incoming.StateReason
	}
	if base.NotificationReason == "" {
		base.NotificationReason = incoming.NotificationReason
	}
	if base.NotificationThreadID == "" {
		base.NotificationThreadID = incoming.NotificationThreadID
	}
	if base.Read == nil {
		base.Read = incoming.Read
	}
	if base.Saved == nil {
		base.Saved = incoming.Saved
	}
	if base.Title == "" {
		base.Title = incoming.Title
	}
	if base.Type == "" || base.Type == model.ItemTypeNotification {
		base.Type = incoming.Type
	}
	if incoming.UpdatedAt.After(base.UpdatedAt) || base.UpdatedAt.IsZero() {
		base.UpdatedAt = incoming.UpdatedAt
	}
	if base.URL == "" {
		base.URL = incoming.URL
	}
	base.Assignees = mergeStrings(base.Assignees, incoming.Assignees)
	base.Reviewers = mergeStrings(base.Reviewers, incoming.Reviewers)
	base.ReviewRequests = mergeStrings(base.ReviewRequests, incoming.ReviewRequests)
	base.SourceFeeds = mergeFeeds(base.SourceFeeds, incoming.SourceFeeds)
	return base
}

func mergeStrings(base, incoming []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(base)+len(incoming))
	for _, value := range append(base, incoming...) {
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func mergeFeeds(base, incoming []model.Feed) []model.Feed {
	seen := map[model.Feed]struct{}{}
	result := make([]model.Feed, 0, len(base)+len(incoming))
	for _, feed := range append(base, incoming...) {
		if _, ok := seen[feed]; ok {
			continue
		}
		seen[feed] = struct{}{}
		result = append(result, feed)
	}
	return result
}

func (c *Client) subjectHTMLURL(apiURL string) string {
	prefix := "https://api." + c.host + "/repos/"
	path := strings.TrimPrefix(apiURL, prefix)
	if path == apiURL {
		return apiURL
	}
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return apiURL
	}
	switch parts[2] {
	case "issues":
		return "https://" + c.host + "/" + parts[0] + "/" + parts[1] + "/" + parts[2] + "/" + parts[3]
	case "pulls":
		return "https://" + c.host + "/" + parts[0] + "/" + parts[1] + "/pull/" + parts[3]
	default:
		return apiURL
	}
}

func (c *Client) search(ctx context.Context, feed model.Feed, now time.Time) ([]model.Item, string, error) {
	query, err := filter.Query(feed, now)
	if err != nil {
		return nil, "", err
	}
	items, warning, err := c.searchQueryWithDocument(ctx, query, maxSearchResults, authoredSearchGraphQL)
	if err != nil {
		return nil, warning, fmt.Errorf("search %s: %w", feed.Title(), err)
	}
	for i := range items {
		items[i] = items[i].WithFeed(feed)
	}
	return items, warning, nil
}

func (c *Client) searchFirstPage(ctx context.Context, feed model.Feed, now time.Time) ([]model.Item, string, error) {
	query, err := filter.Query(feed, now)
	if err != nil {
		return nil, "", err
	}
	query += " sort:updated-desc"
	var response searchResponse
	variables := map[string]any{"query": query, "first": 100, "after": nil}
	if err := c.graphQL(ctx, authoredSearchGraphQL, variables, &response); err != nil {
		return nil, "", fmt.Errorf("search %s: %w", feed.Title(), err)
	}
	items := make([]model.Item, 0, len(response.Data.Search.Nodes))
	for _, node := range response.Data.Search.Nodes {
		item := node.Item(c.host)
		if item.Key != "" {
			items = append(items, item.WithFeed(feed))
		}
	}
	return items, rateWarning(response.Data.RateLimit.Remaining), nil
}

func (c *Client) searchQuery(ctx context.Context, query string, limit int) ([]model.Item, string, error) {
	return c.searchQueryWithDocument(ctx, query, limit, searchGraphQL)
}

func (c *Client) searchQueryWithDocument(ctx context.Context, query string, limit int, document string) ([]model.Item, string, error) {
	var items []model.Item
	var after *string
	var warning string
	for len(items) < limit {
		var response searchResponse
		variables := map[string]any{"query": query, "first": min(100, limit-len(items)), "after": after}
		if err := c.graphQL(ctx, document, variables, &response); err != nil {
			return nil, warning, err
		}
		warning = joinWarning(warning, rateWarning(response.Data.RateLimit.Remaining))
		for _, node := range response.Data.Search.Nodes {
			item := node.Item(c.host)
			if item.Key != "" {
				items = append(items, item)
			}
		}
		if !response.Data.Search.PageInfo.HasNextPage || response.Data.Search.PageInfo.EndCursor == "" {
			break
		}
		cursor := response.Data.Search.PageInfo.EndCursor
		after = &cursor
	}
	return items, warning, nil
}

func (c *Client) graphQL(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("encode graphql request: %w", err)
	}
	endpoint := fmt.Sprintf("https://api.%s/graphql", c.host)
	var lastErr error
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		var envelope graphQLEnvelope
		raw, headers, err := c.doWithHeaders(ctx, http.MethodPost, endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			if rateLimitExhausted(headers) {
				return fmt.Errorf("%w: %w", errGraphQLRateLimitExhausted, err)
			}
			return err
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("decode graphql envelope: %w", err)
		}
		if len(envelope.Errors) == 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("decode graphql response: %w", err)
			}
			return nil
		}
		lastErr = fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
		if rateLimitExhausted(headers) {
			return fmt.Errorf("%w: %w", errGraphQLRateLimitExhausted, lastErr)
		}
		if attempt > maxRetries || !retryableGraphQLError(envelope.Errors[0]) {
			break
		}
		delay := retryDelay(attempt + 1)
		if retryAfter := retryAfterDelay(headers); retryAfter > delay {
			delay = retryAfter
		}
		if err := c.retrySleep(ctx, delay); err != nil {
			return fmt.Errorf("retry wait canceled after %s: %w", delay, err)
		}
	}
	return lastErr
}

type graphQLEnvelope struct {
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func retryableGraphQLError(err graphQLError) bool {
	value := strings.ToLower(err.Type + " " + err.Message)
	return strings.Contains(value, "rate_limit") ||
		strings.Contains(value, "rate limit") ||
		strings.Contains(value, "timeout") ||
		strings.Contains(value, "temporarily unavailable")
}

func (c *Client) rest(ctx context.Context, method, endpoint string, body io.Reader, out any) (int, error) {
	raw, headers, err := c.doWithHeaders(ctx, method, endpoint, "application/json", body)
	if err != nil {
		return 0, err
	}
	if out == nil {
		return remaining(headers), nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return remaining(headers), fmt.Errorf("decode REST response: %w", err)
	}
	return remaining(headers), nil
}

func (c *Client) doWithHeaders(ctx context.Context, method, endpoint, contentType string, body io.Reader) ([]byte, http.Header, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, nil, fmt.Errorf("read request body: %w", err)
		}
	}

	var lastHeaders http.Header
	var lastErr error
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		raw, headers, err := c.doOnce(ctx, method, endpoint, contentType, bodyBytes)
		if err == nil {
			return raw, headers, nil
		}
		lastHeaders = headers
		lastErr = err
		if attempt > maxRetries || !retryable(err, headers) {
			break
		}
		delay := retryDelay(attempt + 1)
		if retryAfter := retryAfterDelay(headers); retryAfter > delay {
			delay = retryAfter
		}
		if err := c.retrySleep(ctx, delay); err != nil {
			return nil, lastHeaders, fmt.Errorf("retry wait canceled after %s: %w", delay, err)
		}
	}
	return nil, lastHeaders, lastErr
}

func (c *Client) doOnce(ctx context.Context, method, endpoint, contentType string, bodyBytes []byte) ([]byte, http.Header, error) {
	reservation, err := c.reserveRequest(ctx, endpoint, bodyBytes)
	if err != nil {
		return nil, nil, err
	}
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "hyper")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.Header, statusError{
			body:     strings.TrimSpace(string(raw)),
			endpoint: endpoint,
			status:   resp.Status,
			code:     resp.StatusCode,
		}
	}
	if c.budget != nil && reservation.Cost > 0 && reservation.Resource == quota.ResourceGraphQL {
		var metering struct {
			Data struct {
				RateLimit struct {
					Cost int `json:"cost"`
				} `json:"rateLimit"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &metering) == nil && metering.Data.RateLimit.Cost > 0 {
			if err := c.budget.Reconcile(reservation, metering.Data.RateLimit.Cost); err != nil {
				return nil, resp.Header, fmt.Errorf("reconcile Hyper API usage: %w", err)
			}
		}
	}
	return raw, resp.Header, nil
}

func (c *Client) reserveRequest(ctx context.Context, endpoint string, body []byte) (quota.Reservation, error) {
	if c.budget == nil || strings.HasSuffix(endpoint, "/rate_limit") {
		return quota.Reservation{}, nil
	}
	resource := quota.ResourceCore
	cost := 1
	if strings.HasSuffix(endpoint, "/graphql") {
		resource = quota.ResourceGraphQL
		var err error
		cost, err = graphQLReservationCost(body)
		if err != nil {
			return quota.Reservation{}, err
		}
	}
	now := time.Now()
	minimumRemaining := 0
	if resource == quota.ResourceGraphQL && ctx.Value(pullRequestPriorityKey{}) != true {
		state := c.budget.Status(now)
		resetAt := state.Resources[quota.ResourceGraphQL].ResetAt
		if resetAt.IsZero() {
			resetAt = now.Add(time.Hour)
		}
		if remaining := resetAt.Sub(now); remaining > 0 {
			minimumRemaining = int((remaining + fastPullRequestPoll - 1) / fastPullRequestPoll)
		}
	}
	reservation, err := c.budget.ReserveKeeping(resource, cost, minimumRemaining, now)
	if err != nil {
		return quota.Reservation{}, err
	}
	return reservation, nil
}

func graphQLReservationCost(body []byte) (int, error) {
	query := string(body)
	switch {
	case strings.Contains(query, "HyperAuthoredSearch"):
		// One search connection requesting at most 100 nodes costs at most one point.
		return 1, nil
	case strings.Contains(query, "HyperSearch"):
		// One search connection plus at most three nested connections for each
		// of 100 results costs three points; keep two points of safety margin.
		return 5, nil
	case strings.Contains(query, "HyperNodes"):
		// At most three nested connections for each of 100 nodes costs three points.
		return 3, nil
	default:
		return 0, errors.New("GraphQL document has no API cost upper bound")
	}
}

type statusError struct {
	body     string
	endpoint string
	status   string
	code     int
}

func (e statusError) Error() string {
	return fmt.Sprintf("%s returned %s: %s", e.endpoint, e.status, e.body)
}

func retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	seconds := math.Pow(2, float64(attempt)) - 2
	delay := time.Duration(seconds) * time.Second
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func retryable(err error, headers http.Header) bool {
	if err == nil {
		return false
	}
	if rateLimitExhausted(headers) {
		return false
	}
	var status statusError
	if errors.As(err, &status) {
		switch status.code {
		case http.StatusForbidden:
			body := strings.ToLower(status.body)
			return headers.Get("Retry-After") != "" ||
				strings.Contains(body, "rate limit") ||
				strings.Contains(body, "secondary rate") ||
				strings.Contains(body, "abuse")
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return true
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "temporary failure") ||
		strings.Contains(message, "server closed idle connection")
}

func rateLimitExhausted(headers http.Header) bool {
	return headers != nil && strings.TrimSpace(headers.Get("X-Ratelimit-Remaining")) == "0"
}

func retryAfterDelay(headers http.Header) time.Duration {
	if headers == nil {
		return 0
	}
	value := headers.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return time.Until(when)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func remaining(headers http.Header) int {
	value := headers.Get("X-Ratelimit-Remaining")
	if value == "" {
		return -1
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return n
}

func rateWarning(remaining int) string {
	if remaining >= 0 && remaining < 100 {
		return fmt.Sprintf("rate limit low (%d remaining)", remaining)
	}
	return ""
}

func joinWarning(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case strings.Contains(a, b):
		return a
	default:
		return a + "; " + b
	}
}

func notificationSubjectType(value string) model.ItemType {
	switch strings.ToLower(value) {
	case "pullrequest", "pull_request", "pull request":
		return model.ItemTypePullRequest
	case "issue":
		return model.ItemTypeIssue
	case "discussion":
		return model.ItemTypeDiscussion
	default:
		return model.ItemTypeNotification
	}
}

func logins(users []user) []string {
	values := make([]string, 0, len(users))
	for _, user := range users {
		if user.Login != "" {
			values = append(values, user.Login)
		}
	}
	return values
}

type user struct {
	Login string `json:"login"`
}

type restNotification struct {
	ID         string      `json:"id"`
	Reason     string      `json:"reason"`
	Repository restRepo    `json:"repository"`
	Subject    restSubject `json:"subject"`
	Unread     bool        `json:"unread"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type restRepo struct {
	FullName string `json:"full_name"`
	Name     string `json:"name"`
	Owner    user   `json:"owner"`
}

type restSubject struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	URL   string `json:"url"`
}

type restSubjectDetail struct {
	Assignees          []user    `json:"assignees"`
	CreatedAt          time.Time `json:"created_at"`
	Draft              bool      `json:"draft"`
	HTMLURL            string    `json:"html_url"`
	Merged             bool      `json:"merged"`
	NodeID             string    `json:"node_id"`
	RequestedReviewers []user    `json:"requested_reviewers"`
	State              string    `json:"state"`
	StateReason        string    `json:"state_reason"`
	UpdatedAt          time.Time `json:"updated_at"`
	User               user      `json:"user"`
}

const nodesGraphQL = `
query HyperNodes($ids: [ID!]!) {
  nodes(ids: $ids) {
    __typename
    ... on Issue {
      id
      title
      url
      createdAt
      updatedAt
      state
      stateReason
      author { login }
      repository { name owner { login } }
      assignees(first: 20) { nodes { login } }
    }
    ... on PullRequest {
      id
      title
      url
      createdAt
      updatedAt
      isDraft
      merged
      state
      author { login }
      repository { name owner { login } }
      assignees(first: 20) { nodes { login } }
      reviewRequests(first: 20) {
        nodes {
          requestedReviewer {
            ... on User { login }
            ... on Bot { login }
            ... on Team { login: combinedSlug }
          }
        }
      }
      latestReviews(first: 20) { nodes { author { login } } }
    }
    ... on Discussion {
      id
      title
      url
      createdAt
      updatedAt
      author { login }
      repository { name owner { login } }
    }
  }
  rateLimit { cost limit remaining resetAt }
}`

type nodesResponse struct {
	Data struct {
		Nodes     []*nodeSubject `json:"nodes"`
		RateLimit struct {
			Remaining int `json:"remaining"`
		} `json:"rateLimit"`
	} `json:"data"`
}

type nodeSubject struct {
	TypeName      string                 `json:"__typename"`
	Assignees     struct{ Nodes []user } `json:"assignees"`
	Author        user                   `json:"author"`
	CreatedAt     time.Time              `json:"createdAt"`
	ID            string                 `json:"id"`
	IsDraft       bool                   `json:"isDraft"`
	LatestReviews struct {
		Nodes []review `json:"nodes"`
	} `json:"latestReviews"`
	Merged     bool `json:"merged"`
	Repository struct {
		Name  string `json:"name"`
		Owner user   `json:"owner"`
	} `json:"repository"`
	ReviewRequests struct {
		Nodes []reviewRequest `json:"nodes"`
	} `json:"reviewRequests"`
	State       string    `json:"state"`
	StateReason string    `json:"stateReason"`
	Title       string    `json:"title"`
	UpdatedAt   time.Time `json:"updatedAt"`
	URL         string    `json:"url"`
}

func (n nodeSubject) Item(host string) model.Item {
	return model.Item{
		Assignees:       logins(n.Assignees.Nodes),
		AuthorLogin:     n.Author.Login,
		CreatedAt:       n.CreatedAt,
		Draft:           n.IsDraft,
		Host:            host,
		Key:             model.StableKey(host, n.ID, n.URL, n.Title),
		Merged:          n.Merged,
		NodeID:          n.ID,
		RepositoryName:  n.Repository.Name,
		RepositoryOwner: n.Repository.Owner.Login,
		Reviewers:       reviewAuthorLogins(n.LatestReviews.Nodes),
		ReviewRequests:  reviewerLogins(n.ReviewRequests.Nodes),
		State:           n.State,
		StateReason:     n.StateReason,
		Title:           n.Title,
		Type:            notificationSubjectType(n.TypeName),
		UpdatedAt:       n.UpdatedAt,
		URL:             n.URL,
	}
}

type review struct {
	Author user `json:"author"`
}

type reviewRequest struct {
	RequestedReviewer user `json:"requestedReviewer"`
}

func reviewerLogins(requests []reviewRequest) []string {
	values := make([]string, 0, len(requests))
	for _, request := range requests {
		if request.RequestedReviewer.Login != "" {
			values = append(values, request.RequestedReviewer.Login)
		}
	}
	return values
}

func reviewAuthorLogins(reviews []review) []string {
	values := make([]string, 0, len(reviews))
	for _, review := range reviews {
		if review.Author.Login != "" {
			values = append(values, review.Author.Login)
		}
	}
	return values
}

const authoredSearchGraphQL = `
query HyperAuthoredSearch($query: String!, $first: Int!, $after: String) {
  search(query: $query, type: ISSUE, first: $first, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on Issue {
        __typename
        id
        title
        url
        createdAt
        state
        stateReason
        updatedAt
        author { login }
        repository { name owner { login } }
      }
      ... on PullRequest {
        __typename
        id
        title
        url
        createdAt
        isDraft
        merged
        state
        updatedAt
        author { login }
        repository { name owner { login } }
      }
    }
  }
  rateLimit { cost limit remaining resetAt }
}`

const searchGraphQL = `
query HyperSearch($query: String!, $first: Int!, $after: String) {
  search(query: $query, type: ISSUE, first: $first, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      ... on Issue {
        __typename
        id
        title
        url
        createdAt
        state
        stateReason
        updatedAt
        author { login }
        repository { name owner { login } }
        assignees(first: 20) { nodes { login } }
      }
      ... on PullRequest {
        __typename
        id
        title
        url
        createdAt
        isDraft
        merged
        state
        updatedAt
        author { login }
        repository { name owner { login } }
        assignees(first: 20) { nodes { login } }
        reviewRequests(first: 20) {
          nodes {
            requestedReviewer {
              ... on User { login }
              ... on Bot { login }
              ... on Team { login: combinedSlug }
            }
          }
        }
        latestReviews(first: 50) { nodes { author { login } } }
      }
    }
  }
  rateLimit { cost limit remaining resetAt }
}`

type searchResponse struct {
	Data struct {
		RateLimit struct {
			Remaining int `json:"remaining"`
		} `json:"rateLimit"`
		Search struct {
			Nodes    []searchNode `json:"nodes"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"search"`
	} `json:"data"`
}

type searchNode struct {
	TypeName      string                 `json:"__typename"`
	Assignees     struct{ Nodes []user } `json:"assignees"`
	Author        user                   `json:"author"`
	CreatedAt     time.Time              `json:"createdAt"`
	ID            string                 `json:"id"`
	IsDraft       bool                   `json:"isDraft"`
	LatestReviews struct {
		Nodes []review `json:"nodes"`
	} `json:"latestReviews"`
	Repository struct {
		Name  string `json:"name"`
		Owner user   `json:"owner"`
	} `json:"repository"`
	ReviewRequests struct {
		Nodes []reviewRequest `json:"nodes"`
	} `json:"reviewRequests"`
	Merged      bool      `json:"merged"`
	State       string    `json:"state"`
	StateReason string    `json:"stateReason"`
	Title       string    `json:"title"`
	UpdatedAt   time.Time `json:"updatedAt"`
	URL         string    `json:"url"`
}

func (n searchNode) Item(host string) model.Item {
	itemType := model.ItemTypeIssue
	if n.TypeName == "PullRequest" {
		itemType = model.ItemTypePullRequest
	}
	item := model.Item{
		Assignees:       logins(n.Assignees.Nodes),
		AuthorLogin:     n.Author.Login,
		CreatedAt:       n.CreatedAt,
		Draft:           n.IsDraft,
		Host:            host,
		Key:             model.StableKey(host, n.ID, n.URL, n.Title),
		Merged:          n.Merged,
		NodeID:          n.ID,
		RepositoryName:  n.Repository.Name,
		RepositoryOwner: n.Repository.Owner.Login,
		State:           n.State,
		StateReason:     n.StateReason,
		Title:           n.Title,
		Type:            itemType,
		UpdatedAt:       n.UpdatedAt,
		URL:             n.URL,
	}
	item.Reviewers = reviewAuthorLogins(n.LatestReviews.Nodes)
	item.ReviewRequests = reviewerLogins(n.ReviewRequests.Nodes)
	return item
}
