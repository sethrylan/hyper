package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

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

	raw, _, err := client.doWithHeaders(context.Background(), http.MethodPost, server.URL, "application/json", io.NopCloser(&stringReader{s: "payload"}))
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

	if _, _, err := client.doWithHeaders(context.Background(), http.MethodGet, server.URL, "", nil); err == nil {
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

	if _, _, err := client.doWithHeaders(context.Background(), http.MethodGet, "https://api.github.com/graphql", "", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
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
		Step:        5,
		Total:       7,
	}
	if got := progress.String(); got != "5/7: supplemental searches 3/7" {
		t.Fatalf("RefreshProgress.String() = %q, want progress text", got)
	}
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
