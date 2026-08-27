//nolint:revive // Internal package exports are shared across command and tests.
package cache

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
	store := &Store{path: path, data: emptyData()}
	var loaded Data
	found, err := jsonfile.Read(path, &loaded)
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	if !found {
		return store, nil
	}
	if loaded.Version != schemaVersion {
		// Feed data is disposable, but local Done markers are user state.
		store.data.Done = loaded.Done
		ensureMaps(&store.data)
		return store, nil
	}
	ensureMaps(&loaded)
	store.data = loaded
	return store, nil
}

func (s *Store) Data() Data {
	ensureMaps(&s.data)
	return cloneData(s.data)
}

// SetIdentity clears cached feeds before they can be reused for another account or host.
func (s *Store) SetIdentity(account, host string) error {
	ensureMaps(&s.data)
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
	ensureMaps(&s.data)
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
	ensureMaps(&s.data)
	item.Done = true
	item.DoneAt = doneAt
	s.replaceImportantItem(item)
	s.data.Done[item.Key] = DoneState{DoneAt: doneAt, UpdatedAt: item.UpdatedAt}
	return s.save()
}

func (s *Store) MarkUndone(item model.Item) error {
	ensureMaps(&s.data)
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

func emptyData() Data {
	data := Data{Version: schemaVersion}
	ensureMaps(&data)
	return data
}

func ensureMaps(data *Data) {
	data.Version = schemaVersion
	if data.Done == nil {
		data.Done = map[string]DoneState{}
	}
	if data.Feeds == nil {
		data.Feeds = map[model.Feed]FeedData{}
	}
}

func cloneData(data Data) Data {
	clone := data
	clone.Done = make(map[string]DoneState, len(data.Done))
	maps.Copy(clone.Done, data.Done)
	clone.Feeds = make(map[model.Feed]FeedData, len(data.Feeds))
	for feed, cached := range data.Feeds {
		cached.Items = cloneItems(cached.Items)
		clone.Feeds[feed] = cached
	}
	return clone
}

func cloneItems(items []model.Item) []model.Item {
	clone := make([]model.Item, len(items))
	for i, item := range items {
		clone[i] = cloneItem(item)
	}
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
