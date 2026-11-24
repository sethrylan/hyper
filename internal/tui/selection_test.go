//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

func TestSelectionSkipsExpandedRepoGroups(t *testing.T) {
	m := Model{
		feeds: map[model.Feed][]model.Item{
			model.FeedImportantNotifications: {
				testItem("one", "repo-a"),
				testItem("two", "repo-b"),
			},
		},
	}
	m.rebuildRows()
	if m.rows[m.selected].kind != rowItem || m.rows[m.selected].item.Title != "one" {
		t.Fatalf("selected row = %#v, want first item", m.rows[m.selected])
	}

	m, _ = m.handleAction(ActionDown)
	if m.rows[m.selected].kind != rowItem || m.rows[m.selected].item.Title != "two" {
		t.Fatalf("selected row = %#v, want second item", m.rows[m.selected])
	}

	m, _ = m.handleAction(ActionUp)
	if m.rows[m.selected].kind != rowItem || m.rows[m.selected].item.Title != "one" {
		t.Fatalf("selected row = %#v, want first item", m.rows[m.selected])
	}
}

func TestCollapsedRepoGroupIsSelectable(t *testing.T) {
	m := Model{
		collapsed: map[string]bool{},
		feeds: map[model.Feed][]model.Item{
			model.FeedImportantNotifications: {
				testItem("one", "repo-a"),
			},
		},
	}
	m.rebuildRows()
	m, _ = m.handleAction(ActionCollapse) // Move to the parent first.
	m, _ = m.handleAction(ActionCollapse)
	if len(m.rows) != 1 || m.rows[m.selected].kind != rowRepo {
		t.Fatalf("collapsed rows = %#v, want only the parent", m.rows)
	}
	m, _ = m.handleAction(ActionExpand)
	if got := m.rows[m.selected]; got.kind != rowItem || got.item.Title != "one" {
		t.Fatalf("selected row after expanding = %#v, want first item", got)
	}
}

func TestSelectionIsRememberedPerFeed(t *testing.T) {
	m := Model{
		feeds: map[model.Feed][]model.Item{
			model.FeedImportantNotifications: {
				testItem("notification-one", "repo-a"),
				testItem("notification-two", "repo-b"),
			},
			model.FeedMyPullRequests: {
				testItem("pull-request-one", "repo-c"),
				testItem("pull-request-two", "repo-d"),
				testItem("pull-request-three", "repo-e"),
			},
		},
	}
	m.rebuildRows()
	m, _ = m.handleAction(ActionDown)
	if got := m.rows[m.selected].item.Title; got != "notification-two" {
		t.Fatalf("selected = %q, want notification-two", got)
	}

	m, _ = m.handleAction(ActionNextFeed)
	if got := m.rows[m.selected].item.Title; got != "pull-request-one" {
		t.Fatalf("selected after switching tabs = %q, want pull-request-one", got)
	}

	m, _ = m.handleAction(ActionDown)
	m, _ = m.handleAction(ActionDown)
	if got := m.rows[m.selected].item.Title; got != "pull-request-three" {
		t.Fatalf("selected = %q, want pull-request-three", got)
	}

	m, _ = m.handleAction(ActionPrevFeed)
	if got := m.rows[m.selected].item.Title; got != "notification-two" {
		t.Fatalf("restored notification selection = %q, want notification-two", got)
	}

	m, _ = m.handleAction(ActionNextFeed)
	if got := m.rows[m.selected].item.Title; got != "pull-request-three" {
		t.Fatalf("restored pull request selection = %q, want pull-request-three", got)
	}
}

func testItem(title, repo string) model.Item {
	return model.Item{
		Host:            "github.com",
		Key:             title,
		RepositoryName:  repo,
		RepositoryOwner: "owner",
		Title:           title,
		Type:            model.ItemTypeIssue,
		UpdatedAt:       time.Now(),
	}
}
