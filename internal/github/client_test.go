//nolint:testpackage // These tests exercise package internals.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

func TestRateLimitsUsesRESTOnly(t *testing.T) {
	var calls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet || req.URL.Path != "/rate_limit" {
			t.Fatalf("request = %s %s, want GET /rate_limit", req.Method, req.URL.Path)
		}
		return jsonResponse(`{"resources":{"core":{"limit":5000,"used":10,"remaining":4990,"reset":100},"graphql":{"limit":5000,"used":5000,"remaining":0,"reset":200},"search":{"limit":30,"used":2,"remaining":28,"reset":300}}}`), nil
	})}

	limits, err := client.RateLimits(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 REST request", calls)
	}
	if limits.GraphQL.Limit != 5000 || limits.GraphQL.Used != 5000 || limits.GraphQL.Remaining != 0 || !limits.GraphQL.ResetAt.Equal(time.Unix(200, 0)) {
		t.Fatalf("GraphQL limits = %#v, want exhausted REST resource", limits.GraphQL)
	}
}

func TestPullRequestRefreshUsesLightweightAuthoredSearch(t *testing.T) {
	client := NewClient("github.com", "token")
	client.account = "me"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "HyperAuthoredSearch") || strings.Contains(string(body), "latestReviews") || strings.Contains(string(body), "assignees") {
			t.Fatalf("authored query is not lightweight: %s", body)
		}
		return jsonResponse(`{"data":{"search":{"pageInfo":{"hasNextPage":false},"nodes":[]},"rateLimit":{"cost":1,"remaining":4999}}}`), nil
	})}

	result, err := client.RefreshPullRequests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Account != "me" || result.Feed != model.FeedMyPullRequests {
		t.Fatalf("account/feed = %q/%s, want me/My Pull Requests", result.Account, result.Feed)
	}
}

func TestPullRequestRefreshFetchesOnlyNewestPage(t *testing.T) {
	var calls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "sort:updated-desc") {
			t.Fatalf("query = %s, want newest-first sort", body)
		}
		return jsonResponse(`{"data":{"search":{"pageInfo":{"hasNextPage":true,"endCursor":"next"},"nodes":[{"__typename":"PullRequest","id":"PR_1","title":"one","url":"https://github.com/o/r/pull/1","repository":{"name":"r","owner":{"login":"o"}}}]},"rateLimit":{"cost":1,"remaining":4999}}}`), nil
	})}

	result, err := client.RefreshPullRequests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("GraphQL calls = %d, want exactly one even when another page exists", calls)
	}
	if len(result.Items) != 1 || result.Items[0].Key == "" {
		t.Fatalf("items = %#v, want the first-page pull request", result.Items)
	}
}

func TestPullRequestRefreshContinuesWhenGitHubRateLimitIsLow(t *testing.T) {
	calls := 0
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(`{"data":{"search":{"pageInfo":{"hasNextPage":false},"nodes":[]},"rateLimit":{"cost":1,"remaining":1}}}`), nil
	})}

	result, err := client.RefreshPullRequests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want request to proceed", calls)
	}
	if !strings.Contains(result.RateWarning, "rate limit low") {
		t.Fatalf("rate warning = %q, want low-limit warning", result.RateWarning)
	}
}

func TestAccountVerificationDispatchesRequest(t *testing.T) {
	client := NewClient("github.com", "token")
	calls := 0
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/user" {
			t.Fatalf("path = %q, want /user", req.URL.Path)
		}
		return jsonResponse(`{"login":"new-account"}`), nil
	})}

	if err := client.VerifyAccount(t.Context(), "new-account"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want one account verification request", calls)
	}
	if client.account != "new-account" {
		t.Fatalf("verified account = %q, want new-account", client.account)
	}
}

func TestAccountVerificationRejectsTokenMismatch(t *testing.T) {
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"login":"token-owner"}`), nil
	})}

	if err := client.VerifyAccount(t.Context(), "configured-account"); err == nil {
		t.Fatal("expected mismatched token owner to fail verification")
	}
}

func TestRetryDelay(t *testing.T) {
	tests := map[int]time.Duration{
		1: 0,
		2: 2 * time.Second,
		3: 6 * time.Second,
		4: 14 * time.Second,
		5: 30 * time.Second,
		8: 60 * time.Second,
	}
	for attempt, want := range tests {
		if got := retryDelay(attempt); got != want {
			t.Fatalf("retryDelay(%d) = %s, want %s", attempt, got, want)
		}
	}
}

func TestDoWithHeadersRetriesTransientStatus(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "payload" {
			t.Fatalf("body = %q, want payload", body)
		}
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var delays []time.Duration
	client := NewClient("github.com", "token")
	client.httpClient = server.Client()
	client.retrySleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}

	raw, _, err := client.doWithHeaders(t.Context(), http.MethodPost, server.URL, "application/json", io.NopCloser(&stringReader{s: "payload"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("response = %q, want ok JSON", raw)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Fatalf("delays = %v, want [2s]", delays)
	}
}

func TestDoWithHeadersDoesNotRetryPlainForbidden(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer server.Close()

	client := NewClient("github.com", "token")
	client.httpClient = server.Client()
	client.retrySleep = func(context.Context, time.Duration) error {
		t.Fatal("unexpected retry sleep")
		return nil
	}

	if _, _, err := client.doWithHeaders(t.Context(), http.MethodGet, server.URL, "", nil); err == nil {
		t.Fatal("expected forbidden error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoWithHeadersRetriesContextDeadlineExceeded(t *testing.T) {
	calls := 0
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			Body:       io.NopCloser(&stringReader{s: `{"ok":true}`}),
			Header:     http.Header{},
			Status:     "200 OK",
			StatusCode: http.StatusOK,
		}, nil
	})}
	client.retrySleep = func(context.Context, time.Duration) error { return nil }

	if _, _, err := client.doWithHeaders(t.Context(), http.MethodGet, "https://api.github.com/graphql", "", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestGraphQLDoesNotRetryExhaustedPrimaryRateLimit(t *testing.T) {
	var calls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := jsonResponse(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`)
		response.Header.Set("X-Ratelimit-Remaining", "0")
		response.Header.Set("X-Ratelimit-Reset", "9999999999")
		return response, nil
	})}
	client.retrySleep = func(context.Context, time.Duration) error {
		t.Fatal("unexpected retry sleep")
		return nil
	}

	err := client.graphQL(t.Context(), `query { viewer { login } }`, nil, &struct{}{})
	if !errors.Is(err, errGraphQLRateLimitExhausted) {
		t.Fatalf("error = %v, want exhausted rate limit", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestGraphQLDoesNotRetryHTTPPrimaryRateLimit(t *testing.T) {
	var calls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := jsonResponse(`{"message":"API rate limit exceeded"}`)
		response.Status = "403 Forbidden"
		response.StatusCode = http.StatusForbidden
		response.Header.Set("X-Ratelimit-Remaining", "0")
		return response, nil
	})}
	client.retrySleep = func(context.Context, time.Duration) error {
		t.Fatal("unexpected retry sleep")
		return nil
	}

	err := client.graphQL(t.Context(), `query { viewer { login } }`, nil, &struct{}{})
	if !errors.Is(err, errGraphQLRateLimitExhausted) {
		t.Fatalf("error = %v, want exhausted rate limit", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestSupplementalSearchStopsWhenGraphQLRateLimitIsExhausted(t *testing.T) {
	var calls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := jsonResponse(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`)
		response.Header.Set("X-Ratelimit-Remaining", "0")
		return response, nil
	})}
	client.retrySleep = func(context.Context, time.Duration) error {
		t.Fatal("unexpected retry sleep")
		return nil
	}

	_, warning := client.fetchSupplementalImportantItems(t.Context(), "me", time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC))
	if calls != 1 {
		t.Fatalf("calls = %d, want supplemental search to stop after 1", calls)
	}
	if !strings.Contains(warning, "rate limit exhausted") {
		t.Fatalf("warning = %q, want exhausted rate limit", warning)
	}
}

func TestNotificationPaginationUsesFiftyItemPagesAndSince(t *testing.T) {
	var pages []string
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/notifications" {
			t.Fatalf("path = %q, want /notifications", req.URL.Path)
		}
		if got := req.URL.Query().Get("per_page"); got != "50" {
			t.Fatalf("per_page = %q, want 50", got)
		}
		if got := req.URL.Query().Get("since"); got != "2026-08-07T15:03:04Z" {
			t.Fatalf("since = %q, want one-second overlap", got)
		}
		page := req.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "1" {
			return notificationPage(50), nil
		}
		return notificationPage(1), nil
	})}

	items, _, err := client.fetchRESTNotificationItems(t.Context(), time.Date(2026, 8, 7, 15, 3, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 51 {
		t.Fatalf("items = %d, want 51", len(items))
	}
	if strings.Join(pages, ",") != "1,2" {
		t.Fatalf("pages = %v, want [1 2]", pages)
	}
}

func TestNotificationPaginationStopsAtCap(t *testing.T) {
	var calls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return notificationPage(50), nil
	})}

	items, _, err := client.fetchRESTNotificationItems(t.Context(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != maxNotifications || calls != maxNotifications/notificationPageSize {
		t.Fatalf("items/calls = %d/%d, want %d/%d", len(items), calls, maxNotifications, maxNotifications/notificationPageSize)
	}
}

func TestRefreshNotificationsIsAdditive(t *testing.T) {
	updated := time.Date(2026, 8, 7, 15, 4, 0, 0, time.UTC)
	existing := model.Item{
		Host:        "github.com",
		Key:         model.StableKey("github.com", "PR_1", "", ""),
		NodeID:      "PR_1",
		Reviewers:   []string{"me"},
		SourceFeeds: []model.Feed{model.FeedImportantNotifications},
		Title:       "old title",
	}
	retained := model.Item{
		Host:        "github.com",
		Key:         "github.com|retained",
		SourceFeeds: []model.Feed{model.FeedImportantNotifications},
		Title:       "retained",
	}

	client := NewClient("github.com", "token")
	client.account = "me"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/notifications":
			return jsonValueResponse([]restNotification{
				{ID: "1", Reason: "subscribed", Subject: restSubject{Title: "new title", Type: "PullRequest", URL: "https://api.github.com/repos/owner/repo/pulls/1"}, UpdatedAt: updated},
				{ID: "2", Reason: "mention", Subject: restSubject{Title: "mentioned"}, UpdatedAt: updated},
			}), nil
		case "/repos/owner/repo/pulls/1":
			return jsonValueResponse(restSubjectDetail{HTMLURL: "https://github.com/owner/repo/pull/1", NodeID: "PR_1", UpdatedAt: updated}), nil
		default:
			t.Fatalf("unexpected request path %q", req.URL.Path)
			return nil, nil
		}
	})}

	result, err := client.RefreshNotifications(t.Context(), NotificationRefreshRequest{
		Existing: []model.Item{existing, retained},
		Since:    time.Date(2026, 8, 7, 15, 3, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	notifications := result.Items
	if len(notifications) != 3 {
		t.Fatalf("items = %d, want updated, retained, and new mention", len(notifications))
	}
	if notifications[0].Title != "new title" || !containsFoldForTest(notifications[0].Reviewers, "me") {
		t.Fatalf("updated item = %#v, want fresh REST fields plus cached reviewer", notifications[0])
	}
	if notifications[1].Key != retained.Key {
		t.Fatalf("retained item = %#v, want existing item preserved", notifications[1])
	}
}

func TestShortMergeMatchesNotificationThreadWhenDetailIsUnavailable(t *testing.T) {
	existing := model.Item{
		Host:                 "github.com",
		Key:                  "github.com|PR_1",
		NodeID:               "PR_1",
		NotificationThreadID: "thread-1",
		Reviewers:            []string{"me"},
		Title:                "old",
	}
	changed := model.Item{
		Host:                 "github.com",
		Key:                  "github.com|https://api.github.com/repos/owner/repo/pulls/1",
		NotificationThreadID: "thread-1",
		Title:                "new",
	}

	items := mergeShortImportantItems([]model.Item{existing}, []model.Item{changed}, "me")
	if len(items) != 1 || items[0].Key != existing.Key || items[0].Title != "new" {
		t.Fatalf("items = %#v, want one updated item matched by notification thread", items)
	}
}

func TestSupplementalImportantQueriesIncludeRecentClosedItems(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	queries := supplementalImportantQueries(now)
	want := []string{
		"is:closed is:pr archived:false author:@me updated:>2026-04-14",
		"is:closed is:pr archived:false assignee:@me updated:>2026-04-14",
		"is:closed is:pr archived:false involves:@me updated:>2026-04-14",
		"is:closed is:issue archived:false author:@me updated:>2026-04-14",
		"is:closed is:issue archived:false assignee:@me updated:>2026-04-14",
		"is:closed is:issue archived:false involves:@me updated:>2026-04-14",
		"archived:false mentions:@me updated:>2026-04-14",
	}

	for _, expected := range want {
		if !containsString(queries, expected) {
			t.Fatalf("supplementalImportantQueries() missing %q in %#v", expected, queries)
		}
	}
}

func TestSupplementalImportantPerQueryLimit(t *testing.T) {
	queries := supplementalImportantQueries(time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	if got := maxSearchResults / len(queries); got != 181 {
		t.Fatalf("per-query supplemental limit = %d, want 181", got)
	}
}

func TestSupplementalMentionsAreAnnotated(t *testing.T) {
	items := withNotificationReason([]model.Item{{Key: "one"}}, "MENTION")
	if got := items[0].NotificationReason; got != "MENTION" {
		t.Fatalf("NotificationReason = %q, want MENTION", got)
	}
}

func TestBackgroundRefreshFetchesFeedsInParallel(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	client := NewClient("github.com", "token")
	client.account = "me"
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		if req.Body != nil {
			raw, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			body = string(raw)
		}

		switch {
		case req.URL.Path == "/graphql" && strings.Contains(body, "is:open is:issue"):
			started <- "issues"
			<-release
			return emptySearchResponse(), nil
		case req.URL.Path == "/notifications":
			started <- "notifications"
			<-release
			return jsonResponse(`[]`), nil
		case req.URL.Path == "/graphql":
			return emptySearchResponse(), nil
		default:
			return jsonResponse(`{"message":"unexpected request"}`), nil
		}
	})}

	done := make(chan error, 1)
	go func() {
		_, err := client.RefreshBackground(t.Context())
		done <- err
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case feed := <-started:
			seen[feed] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for parallel feed refreshes; started feeds = %#v", seen)
		}
	}

	select {
	case err := <-done:
		t.Fatalf("RefreshBackground completed before both feeds started: %v", err)
	default:
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RefreshBackground to complete")
	}

	for _, feed := range []string{"issues", "notifications"} {
		if !seen[feed] {
			t.Fatalf("feed %q was not refreshed in parallel; started feeds = %#v", feed, seen)
		}
	}
}

func TestBackgroundRefreshReusesVerifiedAccount(t *testing.T) {
	var userCalls int
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/user":
			userCalls++
			return jsonResponse(`{"login":"new-account"}`), nil
		case "/notifications":
			return jsonResponse(`[]`), nil
		case "/graphql":
			return emptySearchResponse(), nil
		default:
			t.Fatalf("unexpected request path %q", req.URL.Path)
			return nil, nil
		}
	})}

	if err := client.VerifyAccount(t.Context(), "new-account"); err != nil {
		t.Fatal(err)
	}
	result, err := client.RefreshBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if userCalls != 1 || result.Account != "new-account" {
		t.Fatalf("account/user calls after verification and refresh = %q/%d, want new-account/1", result.Account, userCalls)
	}
	if len(result.Feeds) != 3 {
		t.Fatalf("authoritative feeds = %#v, want all three feeds", result.Feeds)
	}
}

func TestMergeItemPreservesSupplementalMentionReason(t *testing.T) {
	base := model.Item{Key: "one", Title: "issue"}
	incoming := model.Item{Key: "one", NotificationReason: "MENTION"}
	if got := mergeItem(base, incoming).NotificationReason; got != "MENTION" {
		t.Fatalf("NotificationReason = %q, want MENTION", got)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"X-Ratelimit-Remaining": []string{"500"}},
		Status:     "200 OK",
		StatusCode: http.StatusOK,
	}
}

func jsonValueResponse(value any) *http.Response {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return jsonResponse(string(raw))
}

func notificationPage(count int) *http.Response {
	notifications := make([]restNotification, count)
	return jsonValueResponse(notifications)
}

func emptySearchResponse() *http.Response {
	return jsonResponse(`{"data":{"rateLimit":{"remaining":500},"search":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type stringReader struct {
	s string
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}

func containsString(values []supplementalImportantQuery, needle string) bool {
	for _, value := range values {
		if value.Query == needle {
			return true
		}
	}
	return false
}

func containsFoldForTest(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}
