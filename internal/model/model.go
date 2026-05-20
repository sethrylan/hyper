//nolint:revive // Internal package exports are shared across command and tests.
package model

import (
	"slices"
	"time"
)

type Feed string

const (
	FeedImportantNotifications Feed = "important_notifications"
	FeedMyPullRequests         Feed = "my_pull_requests"
	FeedMyIssues               Feed = "my_issues"
)

var Feeds = []Feed{FeedImportantNotifications, FeedMyPullRequests, FeedMyIssues}

func (f Feed) Title() string {
	switch f {
	case FeedImportantNotifications:
		return "Important Notifications"
	case FeedMyPullRequests:
		return "My Pull Requests"
	case FeedMyIssues:
		return "My Issues"
	default:
		return string(f)
	}
}

type ItemType string

const (
	ItemTypeNotification ItemType = "notification"
	ItemTypePullRequest  ItemType = "pull_request"
	ItemTypeIssue        ItemType = "issue"
	ItemTypeDiscussion   ItemType = "discussion"
	ItemTypeUnknown      ItemType = "unknown"
)

type Item struct {
	Assignees            []string  `json:"assignees,omitempty"`
	AuthorLogin          string    `json:"author_login,omitempty"`
	CreatedAt            time.Time `json:"created_at,omitzero"`
	Done                 bool      `json:"done,omitempty"`
	DoneAt               time.Time `json:"done_at,omitzero"`
	Draft                bool      `json:"draft,omitempty"`
	Host                 string    `json:"host"`
	Key                  string    `json:"key"`
	Merged               bool      `json:"merged,omitempty"`
	NodeID               string    `json:"node_id,omitempty"`
	NotificationReason   string    `json:"notification_reason,omitempty"`
	NotificationThreadID string    `json:"notification_thread_id,omitempty"`
	Read                 *bool     `json:"read,omitempty"`
	RepositoryName       string    `json:"repository_name"`
	RepositoryOwner      string    `json:"repository_owner"`
	Reviewers            []string  `json:"reviewers,omitempty"`
	ReviewRequests       []string  `json:"review_requests,omitempty"`
	Saved                *bool     `json:"saved,omitempty"`
	SourceFeeds          []Feed    `json:"source_feeds,omitempty"`
	State                string    `json:"state,omitempty"`
	StateReason          string    `json:"state_reason,omitempty"`
	Title                string    `json:"title"`
	Type                 ItemType  `json:"type"`
	UpdatedAt            time.Time `json:"updated_at"`
	URL                  string    `json:"url,omitempty"`
}

func (i Item) Repository() string {
	if i.RepositoryOwner == "" {
		return i.RepositoryName
	}
	return i.RepositoryOwner + "/" + i.RepositoryName
}

func (i Item) WithFeed(feed Feed) Item {
	if slices.Contains(i.SourceFeeds, feed) {
		return i
	}
	i.SourceFeeds = append(i.SourceFeeds, feed)
	return i
}

func StableKey(host, nodeID, url, fallback string) string {
	switch {
	case nodeID != "":
		return host + "|" + nodeID
	case url != "":
		return host + "|" + url
	default:
		return host + "|" + fallback
	}
}
