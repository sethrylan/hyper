//nolint:revive // Internal package exports are shared across command and tests.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sethrylan/hyper/internal/model"
	"github.com/sethrylan/hyper/internal/quota"
)

type DoneState struct {
	DoneAt    time.Time `json:"done_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Data struct {
	APIUsage          quota.State              `json:"api_usage,omitzero"`
	Account           string                   `json:"account,omitempty"`
	Done              map[string]DoneState     `json:"done,omitempty"`
	FeedItemIDs       map[model.Feed][]string  `json:"feed_item_ids,omitempty"`
	Host              string                   `json:"host,omitempty"`
	Items             map[string]model.Item    `json:"items,omitempty"`
	LastRefresh       time.Time                `json:"last_refresh,omitzero"`
	LastRefreshByFeed map[model.Feed]time.Time `json:"last_refresh_by_feed,omitempty"`
}

type Store struct {
	path string
	data Data
	mu   sync.Mutex
}

func OpenDefault() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home dir: %w", err)
	}
	return Open(filepath.Join(home, ".hyper", "cache.json"))
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, data: emptyData()}
	if err := store.Load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() error {
	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.data = emptyData()
			return nil
		}
		return fmt.Errorf("read cache: %w", err)
	}
	if len(content) == 0 {
		s.data = emptyData()
		return nil
	}
	if err := json.Unmarshal(content, &s.data); err != nil {
		return fmt.Errorf("decode cache: %w", err)
	}
	ensureMaps(&s.data)
	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	ensureMaps(&s.data)
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	content, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".cache-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary cache: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary cache permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary cache: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	return nil
}

func (s *Store) Data() Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureMaps(&s.data)
	return cloneData(s.data)
}

func (s *Store) Replace(account, host string, feeds map[model.Feed][]model.Item, refreshedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := emptyData()
	next.APIUsage = s.data.APIUsage
	next.Account = account
	next.Host = host
	next.Done = s.data.Done
	next.LastRefresh = refreshedAt
	for feed, items := range feeds {
		next.LastRefreshByFeed[feed] = refreshedAt
		if feed == model.FeedImportantNotifications {
			items = ReconcileDone(items, next.Done)
		}
		for _, item := range items {
			if feed != model.FeedImportantNotifications {
				item.Done = false
				item.DoneAt = time.Time{}
			}
			next.Items[item.Key] = mergeCachedItems(item, next.Items[item.Key])
			next.FeedItemIDs[feed] = append(next.FeedItemIDs[feed], item.Key)
		}
	}
	s.data = next
	return s.saveLocked()
}

func (s *Store) ReplaceFeed(account, host string, feed model.Feed, items []model.Item, refreshedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureMaps(&s.data)
	feeds := s.feedsLocked()
	feeds[feed] = append([]model.Item(nil), items...)
	timestamps := mapsClone(s.data.LastRefreshByFeed)
	timestamps[feed] = refreshedAt
	s.replaceFeedsLocked(account, host, feeds, timestamps, maxTime(s.data.LastRefresh, refreshedAt))
	return s.saveLocked()
}

// UpdateFeeds writes item changes without advancing any feed's polling cursor.
func (s *Store) UpdateFeeds(account, host string, feeds map[model.Feed][]model.Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureMaps(&s.data)
	s.replaceFeedsLocked(account, host, feeds, mapsClone(s.data.LastRefreshByFeed), s.data.LastRefresh)
	return s.saveLocked()
}

func (s *Store) MarkDone(item model.Item, doneAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureMaps(&s.data)
	item.Done = true
	item.DoneAt = doneAt
	s.data.Items[item.Key] = item
	s.data.Done[item.Key] = DoneState{DoneAt: doneAt, UpdatedAt: item.UpdatedAt}
	return s.saveLocked()
}

func (s *Store) MarkUndone(item model.Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ensureMaps(&s.data)
	delete(s.data.Done, item.Key)
	item.Done = false
	item.DoneAt = time.Time{}
	s.data.Items[item.Key] = item
	return s.saveLocked()
}

func (s *Store) QuotaState() quota.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneQuotaState(s.data.APIUsage)
}

func (s *Store) SaveQuotaState(state quota.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.APIUsage = cloneQuotaState(state)
	return s.saveLocked()
}

func ReconcileDone(items []model.Item, done map[string]DoneState) []model.Item {
	out := make([]model.Item, 0, len(items))
	for _, item := range items {
		state, ok := done[item.Key]
		if !ok {
			out = append(out, item)
			continue
		}
		if item.UpdatedAt.After(state.DoneAt) || item.UpdatedAt.After(state.UpdatedAt) {
			delete(done, item.Key)
			item.Done = false
			item.DoneAt = time.Time{}
			out = append(out, item)
			continue
		}
	}
	return out
}

func emptyData() Data {
	data := Data{}
	ensureMaps(&data)
	return data
}

func ensureMaps(data *Data) {
	if data.Done == nil {
		data.Done = map[string]DoneState{}
	}
	if data.FeedItemIDs == nil {
		data.FeedItemIDs = map[model.Feed][]string{}
	}
	if data.Items == nil {
		data.Items = map[string]model.Item{}
	}
	if data.LastRefreshByFeed == nil {
		data.LastRefreshByFeed = map[model.Feed]time.Time{}
	}
	if !data.LastRefresh.IsZero() {
		for feed := range data.FeedItemIDs {
			if data.LastRefreshByFeed[feed].IsZero() {
				data.LastRefreshByFeed[feed] = data.LastRefresh
			}
		}
	}
}

func (s *Store) feedsLocked() map[model.Feed][]model.Item {
	feeds := make(map[model.Feed][]model.Item, len(s.data.FeedItemIDs))
	for feed, keys := range s.data.FeedItemIDs {
		for _, key := range keys {
			if item, ok := s.data.Items[key]; ok {
				feeds[feed] = append(feeds[feed], cloneItem(item))
			}
		}
	}
	return feeds
}

func (s *Store) replaceFeedsLocked(account, host string, feeds map[model.Feed][]model.Item, timestamps map[model.Feed]time.Time, lastRefresh time.Time) {
	next := emptyData()
	next.APIUsage = s.data.APIUsage
	next.Account = account
	next.Host = host
	next.Done = s.data.Done
	next.LastRefresh = lastRefresh
	next.LastRefreshByFeed = timestamps
	for feed, items := range feeds {
		if feed == model.FeedImportantNotifications {
			items = ReconcileDone(items, next.Done)
		}
		for _, item := range items {
			if feed != model.FeedImportantNotifications {
				item.Done = false
				item.DoneAt = time.Time{}
			}
			next.Items[item.Key] = mergeCachedItems(cloneItem(item), next.Items[item.Key])
			next.FeedItemIDs[feed] = append(next.FeedItemIDs[feed], item.Key)
		}
	}
	s.data = next
}

func cloneData(data Data) Data {
	clone := data
	clone.APIUsage = cloneQuotaState(data.APIUsage)
	clone.Done = make(map[string]DoneState, len(data.Done))
	maps.Copy(clone.Done, data.Done)
	clone.FeedItemIDs = make(map[model.Feed][]string, len(data.FeedItemIDs))
	for feed, keys := range data.FeedItemIDs {
		clone.FeedItemIDs[feed] = append([]string(nil), keys...)
	}
	clone.Items = make(map[string]model.Item, len(data.Items))
	for key, item := range data.Items {
		clone.Items[key] = cloneItem(item)
	}
	clone.LastRefreshByFeed = mapsClone(data.LastRefreshByFeed)
	return clone
}

func cloneItem(item model.Item) model.Item {
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
	return item
}

func mergeCachedItems(fresh, existing model.Item) model.Item {
	if fresh.Key == "" {
		return existing
	}
	if existing.Key == "" {
		return fresh
	}
	if existing.UpdatedAt.After(fresh.UpdatedAt) {
		fresh, existing = existing, fresh
	}
	if fresh.AuthorLogin == "" {
		fresh.AuthorLogin = existing.AuthorLogin
	}
	if fresh.CreatedAt.IsZero() {
		fresh.CreatedAt = existing.CreatedAt
	}
	if fresh.NodeID == "" {
		fresh.NodeID = existing.NodeID
	}
	if fresh.NotificationReason == "" {
		fresh.NotificationReason = existing.NotificationReason
	}
	if fresh.NotificationThreadID == "" {
		fresh.NotificationThreadID = existing.NotificationThreadID
	}
	if fresh.Read == nil {
		fresh.Read = existing.Read
	}
	if fresh.RepositoryName == "" {
		fresh.RepositoryName = existing.RepositoryName
	}
	if fresh.RepositoryOwner == "" {
		fresh.RepositoryOwner = existing.RepositoryOwner
	}
	if fresh.Saved == nil {
		fresh.Saved = existing.Saved
	}
	if fresh.StateReason == "" {
		fresh.StateReason = existing.StateReason
	}
	if fresh.Title == "" {
		fresh.Title = existing.Title
	}
	if fresh.Type == "" || fresh.Type == model.ItemTypeUnknown {
		fresh.Type = existing.Type
	}
	if fresh.URL == "" {
		fresh.URL = existing.URL
	}
	fresh.Assignees = mergeStrings(fresh.Assignees, existing.Assignees)
	fresh.Reviewers = mergeStrings(fresh.Reviewers, existing.Reviewers)
	fresh.ReviewRequests = mergeStrings(fresh.ReviewRequests, existing.ReviewRequests)
	fresh.SourceFeeds = mergeFeeds(fresh.SourceFeeds, existing.SourceFeeds)
	return fresh
}

func mergeStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	merged := make([]string, 0, len(a)+len(b))
	for _, value := range append(append([]string(nil), a...), b...) {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	return merged
}

func mergeFeeds(a, b []model.Feed) []model.Feed {
	seen := make(map[model.Feed]struct{}, len(a)+len(b))
	merged := make([]model.Feed, 0, len(a)+len(b))
	for _, feed := range append(append([]model.Feed(nil), a...), b...) {
		if _, ok := seen[feed]; ok {
			continue
		}
		seen[feed] = struct{}{}
		merged = append(merged, feed)
	}
	return merged
}

func cloneQuotaState(state quota.State) quota.State {
	clone := state
	clone.Resources = make(map[quota.Resource]quota.Window, len(state.Resources))
	maps.Copy(clone.Resources, state.Resources)
	return clone
}

func mapsClone(values map[model.Feed]time.Time) map[model.Feed]time.Time {
	return maps.Clone(values)
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
