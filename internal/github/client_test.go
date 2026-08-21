//nolint:testpackage // These tests exercise package internals.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	items, _, err := client.fetchRESTNotificationItems(t.Context(), time.Date(2026, 8, 7, 15, 3, 5, 0, time.UTC), 1, 2, 2)
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

	items, _, err := client.fetchRESTNotificationItems(t.Context(), time.Time{}, 1, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != maxNotifications || calls != maxNotifications/notificationPageSize {
		t.Fatalf("items/calls = %d/%d, want %d/%d", len(items), calls, maxNotifications, maxNotifications/notificationPageSize)
	}
}

func TestRefreshNotificationsIsAdditiveAndRefreshesPullRequestMetadata(t *testing.T) {
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
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/notifications":
			return jsonValueResponse([]restNotification{
				{ID: "1", Reason: "subscribed", Subject: restSubject{Title: "new title", Type: "PullRequest", URL: "https://api.github.com/repos/owner/repo/pulls/1"}, UpdatedAt: updated},
				{ID: "2", Reason: "mention", Subject: restSubject{Title: "mentioned"}, UpdatedAt: updated},
			}), nil
		case "/repos/owner/repo/pulls/1":
			return jsonValueResponse(restSubjectDetail{HTMLURL: "https://github.com/owner/repo/pull/1", NodeID: "PR_1", UpdatedAt: updated}), nil
		case "/graphql":
			return jsonResponse(`{"data":{"nodes":[{"__typename":"PullRequest","id":"PR_1","title":"latest title","updatedAt":"2026-08-07T15:04:00Z","isDraft":false,"merged":true,"state":"MERGED"}],"rateLimit":{"remaining":4999}}}`), nil
		default:
			t.Fatalf("unexpected request path %q", req.URL.Path)
			return nil, nil
		}
	})}

	result, err := client.RefreshNotifications(t.Context(), NotificationRefreshRequest{
		Account:            "me",
		Existing:           []model.Item{existing, retained},
		PullRequestNodeIDs: []string{"PR_1"},
		Since:              time.Date(2026, 8, 7, 15, 3, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("items = %d, want updated, retained, and new mention", len(result.Items))
	}
	if result.Items[0].Title != "new title" || !containsFoldForTest(result.Items[0].Reviewers, "me") {
		t.Fatalf("updated item = %#v, want fresh REST fields plus cached reviewer", result.Items[0])
	}
	if result.Items[1].Key != retained.Key {
		t.Fatalf("retained item = %#v, want existing item preserved", result.Items[1])
	}
	if len(result.PullRequests) != 1 {
		t.Fatalf("pull request metadata = %#v, want one update", result.PullRequests)
	}
	metadata := result.PullRequests[0]
	if metadata.NodeID != "PR_1" || metadata.Title != "latest title" || metadata.State != "MERGED" || !metadata.Merged {
		t.Fatalf("pull request metadata = %#v, want latest title and merged state", metadata)
	}
}

func TestPullRequestMetadataRefreshDeduplicatesAndBatchesNodeIDs(t *testing.T) {
	var requests [][]string
	client := NewClient("github.com", "token")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Variables struct {
				IDs []string `json:"ids"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body.Variables.IDs)
		return jsonResponse(`{"data":{"nodes":[],"rateLimit":{"remaining":4999}}}`), nil
	})}

	ids := make([]string, 0, 103)
	for i := range 101 {
		ids = append(ids, fmt.Sprintf("PR_%d", i))
	}
	ids = append(ids, "", "PR_0")
	if _, _, err := client.fetchPullRequestMetadata(t.Context(), ids); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || len(requests[0]) != 100 || len(requests[1]) != 1 {
		t.Fatalf("metadata request batches = %#v, want 100 and 1 unique node IDs", requests)
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

func TestRefreshFetchesFeedsInParallel(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	client := NewClient("github.com", "token")
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
		case req.URL.Path == "/graphql" && strings.Contains(body, "query { viewer"):
			return jsonResponse(`{"data":{"viewer":{"login":"me"},"rateLimit":{"remaining":500}}}`), nil
		case req.URL.Path == "/graphql" && strings.Contains(body, "is:open is:pr"):
			started <- "pull requests"
			<-release
			return emptySearchResponse(), nil
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
		_, err := client.Refresh(t.Context())
		done <- err
	}()

	seen := map[string]bool{}
	for range 3 {
		select {
		case feed := <-started:
			seen[feed] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for parallel feed refreshes; started feeds = %#v", seen)
		}
	}

	select {
	case err := <-done:
		t.Fatalf("Refresh completed before all feed refreshes started: %v", err)
	default:
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Refresh to complete")
	}

	for _, feed := range []string{"pull requests", "issues", "notifications"} {
		if !seen[feed] {
			t.Fatalf("feed %q was not refreshed in parallel; started feeds = %#v", feed, seen)
		}
	}
}

func TestMergeItemPreservesSupplementalMentionReason(t *testing.T) {
	base := model.Item{Key: "one", Title: "issue"}
	incoming := model.Item{Key: "one", NotificationReason: "MENTION"}
	if got := mergeItem(base, incoming).NotificationReason; got != "MENTION" {
		t.Fatalf("NotificationReason = %q, want MENTION", got)
	}
}

func TestRefreshProgressString(t *testing.T) {
	progress := RefreshProgress{
		DetailStep:  3,
		DetailTotal: 7,
		Phase:       "supplemental searches",
		Step:        7,
		Total:       7,
	}
	if got := progress.String(); got != "7/7: supplemental searches 3/7" {
		t.Fatalf("RefreshProgress.String() = %q, want progress text", got)
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
