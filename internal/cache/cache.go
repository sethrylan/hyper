//nolint:revive // Internal package exports are shared across command and tests.
package cache

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sethrylan/hyper/internal/jsonfile"
	"github.com/sethrylan/hyper/internal/model"
)

const schemaVersion = 2

type DoneState struct {
	DoneAt    time.Time `json:"done_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FeedData struct {
	Items       []model.Item `json:"items,omitempty"`
	RefreshedAt time.Time    `json:"refreshed_at,omitzero"`
}

type Data struct {
	Version int                     `json:"version"`
	Account string                  `json:"account,omitempty"`
	Done    map[string]DoneState    `json:"done,omitempty"`
	Feeds   map[model.Feed]FeedData `json:"feeds,omitempty"`
	Host    string                  `json:"host,omitempty"`
}

type Store struct {
	path string
	data Data
}

func OpenDefault() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home dir: %w", err)
	}
	return Open(filepath.Join(home, ".hyper", "cache.json"))
}

func Open(path string) (*Store, error) {
	var data Data
	if err := jsonfile.Read(path, &data); err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	if data.Version != schemaVersion {
		// Local Done markers survive feed schema changes.
		data = Data{Version: schemaVersion, Done: data.Done}
	}
	if data.Done == nil {
		data.Done = map[string]DoneState{}
	}
	if data.Feeds == nil {
		data.Feeds = map[model.Feed]FeedData{}
	}
	return &Store{path: path, data: data}, nil
}

func (s *Store) Data() Data {
	data := s.data
	data.Done = maps.Clone(data.Done)
	data.Feeds = make(map[model.Feed]FeedData, len(s.data.Feeds))
	for feed, cached := range s.data.Feeds {
		cached.Items = cloneItems(cached.Items)
		data.Feeds[feed] = cached
	}
	return data
}

// SetIdentity clears cached feeds before they can be reused for another account or host.
func (s *Store) SetIdentity(account, host string) error {
	if s.data.Account == account && s.data.Host == host {
		return nil
	}
	s.data.Account = account
	s.data.Host = host
	s.data.Feeds = map[model.Feed]FeedData{}
	return s.save()
}

// ReplaceFeeds replaces only the supplied feeds and advances their cursors.
func (s *Store) ReplaceFeeds(account, host string, feeds map[model.Feed][]model.Item, refreshedAt time.Time) error {
	if s.data.Account != account || s.data.Host != host {
		s.data.Feeds = map[model.Feed]FeedData{}
	}
	s.data.Account = account
	s.data.Host = host
	for feed, items := range feeds {
		items = cloneItems(items)
		if feed == model.FeedImportantNotifications {
			items = ReconcileDone(items, s.data.Done)
		} else {
			for i := range items {
				items[i].Done = false
				items[i].DoneAt = time.Time{}
			}
		}
		s.data.Feeds[feed] = FeedData{Items: items, RefreshedAt: refreshedAt}
	}
	return s.save()
}

func (s *Store) MarkDone(item model.Item, doneAt time.Time) error {
	item.Done = true
	item.DoneAt = doneAt
	s.replaceImportantItem(item)
	s.data.Done[item.Key] = DoneState{DoneAt: doneAt, UpdatedAt: item.UpdatedAt}
	return s.save()
}

func (s *Store) MarkUndone(item model.Item) error {
	delete(s.data.Done, item.Key)
	item.Done = false
	item.DoneAt = time.Time{}
	s.replaceImportantItem(item)
	return s.save()
}

func (s *Store) replaceImportantItem(item model.Item) {
	feed := s.data.Feeds[model.FeedImportantNotifications]
	for i := range feed.Items {
		if feed.Items[i].Key == item.Key {
			feed.Items[i] = cloneItem(item)
			break
		}
	}
	s.data.Feeds[model.FeedImportantNotifications] = feed
}

func (s *Store) save() error {
	if err := jsonfile.Write(s.path, s.data); err != nil {
		return fmt.Errorf("save cache: %w", err)
	}
	return nil
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
		}
	}
	return out
}

func cloneItems(items []model.Item) []model.Item {
	clone := make([]model.Item, len(items))
	for i, item := range items {
		clone[i] = cloneItem(item)
	}
	return clone
}

func cloneItem(item model.Item) model.Item {
	item.Assignees = slices.Clone(item.Assignees)
	item.Reviewers = slices.Clone(item.Reviewers)
	item.ReviewRequests = slices.Clone(item.ReviewRequests)
	item.SourceFeeds = slices.Clone(item.SourceFeeds)
	if item.Read != nil {
		item.Read = new(*item.Read)
	}
	if item.Saved != nil {
		item.Saved = new(*item.Saved)
	}
	return item
}
