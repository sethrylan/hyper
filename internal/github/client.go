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
	"sync/atomic"
	"time"

	"github.com/sethrylan/hyper/internal/filter"
	"github.com/sethrylan/hyper/internal/model"
)

const (
	maxNotifications = 500
	maxSearchResults = 2000
	maxRetries       = 10
	maxRetryDelay    = 60 * time.Second
)

type Client struct {
	progress   atomic.Value
	host       string
	httpClient *http.Client
	retrySleep func(context.Context, time.Duration) error
	token      string
}

type RefreshProgress struct {
	Detail      string
	DetailStep  int
	DetailTotal int
	Phase       string
	Step        int
	Total       int
}

type RefreshResult struct {
	Account     string
	Feeds       map[model.Feed][]model.Item
	RateWarning string
	RefreshedAt time.Time
}

type RateLimits struct {
	Account string
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

func (c *Client) RateLimits(ctx context.Context) (RateLimits, error) {
	var restResponse struct {
		Resources struct {
			Core   restRateLimitResource `json:"core"`
			Search restRateLimitResource `json:"search"`
		} `json:"resources"`
	}
	if _, err := c.rest(ctx, http.MethodGet, fmt.Sprintf("https://api.%s/rate_limit", c.host), nil, &restResponse); err != nil {
		return RateLimits{}, fmt.Errorf("fetch REST rate limits: %w", err)
	}

	var graphQLResponse struct {
		Data struct {
			RateLimit struct {
				Limit     int       `json:"limit"`
				Remaining int       `json:"remaining"`
				ResetAt   time.Time `json:"resetAt"`
				Used      int       `json:"used"`
			} `json:"rateLimit"`
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := c.graphQL(ctx, `query { rateLimit { limit used remaining resetAt } viewer { login } }`, nil, &graphQLResponse); err != nil {
		return RateLimits{}, fmt.Errorf("fetch GraphQL rate limits: %w", err)
	}

	return RateLimits{
		Account: graphQLResponse.Data.Viewer.Login,
		Core:    restResponse.Resources.Core.Resource(),
		GraphQL: RateLimitResource{
			Limit:     graphQLResponse.Data.RateLimit.Limit,
			Remaining: graphQLResponse.Data.RateLimit.Remaining,
			ResetAt:   graphQLResponse.Data.RateLimit.ResetAt,
			Used:      graphQLResponse.Data.RateLimit.Used,
		},
		Search: restResponse.Resources.Search.Resource(),
	}, nil
}

func (c *Client) Refresh(ctx context.Context) (RefreshResult, error) {
	now := time.Now()
	c.setProgress(RefreshProgress{Phase: "account", Step: 1, Total: 7})
	account, rateWarning, err := c.viewer(ctx)
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
	startFeedRefresh := func(feed model.Feed, progress RefreshProgress, refresh func(context.Context) ([]model.Item, string, error)) {
		go func() {
			c.setProgress(progress)
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

	startFeedRefresh(model.FeedMyPullRequests, RefreshProgress{Phase: "pull requests", Step: 2, Total: 7}, func(ctx context.Context) ([]model.Item, string, error) {
		return c.search(ctx, model.FeedMyPullRequests, now)
	})
	startFeedRefresh(model.FeedMyIssues, RefreshProgress{Phase: "issues", Step: 3, Total: 7}, func(ctx context.Context) ([]model.Item, string, error) {
		return c.search(ctx, model.FeedMyIssues, now)
	})
	startFeedRefresh(model.FeedImportantNotifications, RefreshProgress{Phase: "notifications", Step: 4, Total: 7}, func(ctx context.Context) ([]model.Item, string, error) {
		return c.fetchNotifications(ctx, account)
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
		Account:     account,
		Feeds:       feeds,
		RateWarning: rateWarning,
		RefreshedAt: now,
	}, nil
}

func (c *Client) CurrentProgress() RefreshProgress {
	progress, ok := c.progress.Load().(RefreshProgress)
	if !ok {
		return RefreshProgress{}
	}
	return progress
}

func (c *Client) setProgress(progress RefreshProgress) {
	c.progress.Store(progress)
}

func (p RefreshProgress) String() string {
	if p.Phase == "" {
		return ""
	}
	var b strings.Builder
	if p.Total > 0 && p.Step > 0 {
		b.WriteString(fmt.Sprintf("%d/%d: ", p.Step, p.Total))
	}
	b.WriteString(p.Phase)
	if p.Detail != "" {
		b.WriteString(" ")
		b.WriteString(p.Detail)
	}
	if p.DetailTotal > 0 && p.DetailStep > 0 {
		b.WriteString(fmt.Sprintf(" %d/%d", p.DetailStep, p.DetailTotal))
	} else if p.DetailStep > 0 {
		b.WriteString(fmt.Sprintf(" %d", p.DetailStep))
	}
	return b.String()
}

func (c *Client) viewer(ctx context.Context) (string, string, error) {
	var response struct {
		Data struct {
			RateLimit struct {
				Remaining int `json:"remaining"`
			} `json:"rateLimit"`
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := c.graphQL(ctx, `query { viewer { login } rateLimit { remaining } }`, nil, &response); err != nil {
		return "", "", fmt.Errorf("fetch authenticated user: %w", err)
	}
	return response.Data.Viewer.Login, rateWarning(response.Data.RateLimit.Remaining), nil
}

func (c *Client) fetchNotifications(ctx context.Context, me string) ([]model.Item, string, error) {
	notifications := make([]restNotification, 0, maxNotifications)
	var warning string
	for page := 1; len(notifications) < maxNotifications; page++ {
		c.setProgress(RefreshProgress{Detail: "page", DetailStep: page, Phase: "notifications", Step: 4, Total: 7})
		endpoint := fmt.Sprintf("https://api.%s/notifications?all=true&per_page=100&page=%d", c.host, page)
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
		if len(pageNotifications) < 100 {
			break
		}
	}
	notificationItems := make([]model.Item, 0, len(notifications))
	for index, notification := range notifications {
		c.setProgress(RefreshProgress{DetailStep: index + 1, DetailTotal: len(notifications), Phase: "notification details", Step: 5, Total: 7})
		notificationItems = append(notificationItems, c.restNotificationItem(ctx, notification))
	}
	c.setProgress(RefreshProgress{Phase: "notification enrichment", Step: 6, Total: 7})
	enrichedItems, enrichWarning, err := c.enrichItems(ctx, notificationItems)
	if err != nil {
		warning = joinWarning(warning, "notification subject enrichment failed: "+err.Error())
		enrichedItems = notificationItems
	} else {
		warning = joinWarning(warning, enrichWarning)
	}
	items := importantItems(enrichedItems, me)

	c.setProgress(RefreshProgress{Phase: "supplemental searches", Step: 7, Total: 7})
	supplemental, supplementalWarning := c.fetchSupplementalImportantItems(ctx, me, time.Now())
	warning = joinWarning(warning, supplementalWarning)
	items = mergeItems(items, importantItems(supplemental, me))
	for i := range items {
		items[i] = items[i].WithFeed(model.FeedImportantNotifications)
	}
	return items, warning, nil
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
		c.setProgress(RefreshProgress{DetailStep: start/100 + 1, DetailTotal: (len(ids) + 99) / 100, Phase: "notification enrichment", Step: 6, Total: 7})
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
	for index, source := range queries {
		c.setProgress(RefreshProgress{DetailStep: index + 1, DetailTotal: len(queries), Phase: "supplemental searches", Step: 7, Total: 7})
		queryItems, queryWarning, err := c.searchQuery(ctx, source.Query, maxSearchResults/len(queries))
		if err != nil {
			warning = joinWarning(warning, "supplemental search failed: "+err.Error())
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
		{Query: fmt.Sprintf("is:open archived:false author:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:open archived:false assignee:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:open is:pr archived:false review-requested:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:open archived:false involves:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:closed is:pr archived:false author:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:closed is:pr archived:false assignee:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:closed is:pr archived:false involves:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:closed is:issue archived:false author:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:closed is:issue archived:false assignee:@me updated:>%s", cutoff)},
		{Query: fmt.Sprintf("is:closed is:issue archived:false involves:@me updated:>%s", cutoff)},
		{NotificationReason: "MENTION", Query: fmt.Sprintf("archived:false mentions:@me updated:>%s", cutoff)},
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
	items, warning, err := c.searchQuery(ctx, query, maxSearchResults)
	if err != nil {
		return nil, warning, fmt.Errorf("search %s: %w", feed.Title(), err)
	}
	for i := range items {
		items[i] = items[i].WithFeed(feed)
	}
	return items, warning, nil
}

func (c *Client) searchQuery(ctx context.Context, query string, limit int) ([]model.Item, string, error) {
	var items []model.Item
	var after *string
	var warning string
	for len(items) < limit {
		var response searchResponse
		variables := map[string]any{"query": query, "first": min(100, limit-len(items)), "after": after}
		if err := c.graphQL(ctx, searchGraphQL, variables, &response); err != nil {
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
		raw, err := c.do(ctx, http.MethodPost, endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
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
		if attempt > maxRetries || !retryableGraphQLError(envelope.Errors[0]) {
			break
		}
		delay := retryDelay(attempt + 1)
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

func (c *Client) do(ctx context.Context, method, endpoint, contentType string, body io.Reader) ([]byte, error) {
	raw, _, err := c.doWithHeaders(ctx, method, endpoint, contentType, body)
	return raw, err
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
	defer resp.Body.Close()
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
	return raw, resp.Header, nil
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
	value := headers.Get("X-RateLimit-Remaining")
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
  rateLimit { remaining }
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
  rateLimit { remaining }
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
