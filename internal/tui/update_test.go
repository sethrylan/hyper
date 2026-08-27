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

	"github.com/sethrylan/hyper/internal/autoupdate"
	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

type updateServiceStub struct {
	result autoupdate.Result
}

func (s updateServiceStub) Update(context.Context) autoupdate.Result {
	return s.result
}

func TestInitStartsBackgroundUpdate(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewWithUpdater(&refreshServiceStub{}, store, "github.com", updateServiceStub{})

	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init message type = %T, want tea.BatchMsg", m.Init()())
	}
	if len(batch) != 14 {
		t.Fatalf("Init command count = %d, want refresh scheduling plus update", len(batch))
	}
}

func TestSuccessfulUpdateNoticePersistsAfterRefresh(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(&refreshServiceStub{}, store, "github.com")

	updatedModel, _ := m.Update(updateMsg{result: autoupdate.Result{UpdatedVersion: "1.2.3"}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updatedModel)
	}
	refreshedModel, _ := updated.Update(feedRefreshMsg{kind: refreshPullRequests, result: github.FeedRefreshResult{
		Account:     "me",
		Feed:        model.FeedMyPullRequests,
		Items:       mapFeeds()[model.FeedMyPullRequests],
		RefreshedAt: time.Now(),
	}})
	refreshed, ok := refreshedModel.(Model)
	if !ok {
		t.Fatalf("refreshed model type = %T, want tui.Model", refreshedModel)
	}

	if !strings.Contains(refreshed.renderStatus(), "updated to v1.2.3; restart hyper to use it") {
		t.Fatalf("status = %q, want persistent update notice", refreshed.renderStatus())
	}
}

func TestApplyFailureIsVisible(t *testing.T) {
	m := Model{account: "me", host: "github.com", status: "ready"}
	updatedModel, _ := m.Update(updateMsg{result: autoupdate.Result{ApplyError: errors.New("permission denied")}})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updatedModel)
	}

	status := updated.renderStatus()
	if !strings.Contains(status, "auto-update failed: permission denied") || !strings.Contains(status, "reinstall from GitHub Releases") {
		t.Fatalf("status = %q, want actionable update failure", status)
	}
}

func TestEmptyUpdateResultDoesNotChangeStatus(t *testing.T) {
	m := Model{account: "me", host: "github.com", status: "ready"}
	updatedModel, _ := m.Update(updateMsg{})
	updated, ok := updatedModel.(Model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.Model", updatedModel)
	}
	if updated.updateNotice != "" {
		t.Fatalf("update notice = %q, want empty", updated.updateNotice)
	}
}

func mapFeeds() map[model.Feed][]model.Item {
	return map[model.Feed][]model.Item{}
}
