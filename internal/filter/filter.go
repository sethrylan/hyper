package filter

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

const defaultRelativeDays = 30

func Query(feed model.Feed, now time.Time) (string, error) {
	cutoff := now.AddDate(0, 0, -defaultRelativeDays).Format("2006-01-02")
	switch feed {
	case model.FeedMyPullRequests:
		return fmt.Sprintf("is:open is:pr author:@me archived:false created:>%s", cutoff), nil
	case model.FeedMyIssues:
		return fmt.Sprintf("is:open is:issue author:@me archived:false created:>%s", cutoff), nil
	default:
		return "", fmt.Errorf("feed %s has no search query", feed)
	}
}

func Important(item model.Item, me string) bool {
	me = strings.ToLower(me)
	if strings.EqualFold(item.AuthorLogin, me) {
		return true
	}
	if item.Saved != nil && *item.Saved {
		return true
	}
	if strings.EqualFold(item.NotificationReason, "MENTION") {
		return true
	}
	return containsFold(item.Assignees, me) ||
		containsFold(item.Reviewers, me) ||
		containsFold(item.ReviewRequests, me)
}

func containsFold(values []string, needle string) bool {
	return slices.ContainsFunc(values, func(value string) bool {
		return strings.EqualFold(value, needle)
	})
}
