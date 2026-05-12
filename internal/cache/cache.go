package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sethrylan/hyper/internal/model"
)

type DoneState struct {
	DoneAt    time.Time `json:"done_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Data struct {
	Account     string                  `json:"account,omitempty"`
	Done        map[string]DoneState    `json:"done,omitempty"`
	FeedItemIDs map[model.Feed][]string `json:"feed_item_ids,omitempty"`
	Host        string                  `json:"host,omitempty"`
	Items       map[string]model.Item   `json:"items,omitempty"`
	LastRefresh time.Time               `json:"last_refresh,omitempty"`
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
	if err := store.Load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Load() error {
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
	ensureMaps(&s.data)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	content, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	if err := os.WriteFile(s.path, content, 0o600); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

func (s *Store) Data() Data {
	ensureMaps(&s.data)
	return s.data
}

func (s *Store) Replace(account, host string, feeds map[model.Feed][]model.Item, refreshedAt time.Time) error {
	next := emptyData()
	next.Account = account
	next.Host = host
	next.Done = s.data.Done
	next.LastRefresh = refreshedAt
	for feed, items := range feeds {
		if feed == model.FeedImportantNotifications {
			items = ReconcileDone(items, next.Done)
		}
		for _, item := range items {
			if feed != model.FeedImportantNotifications {
				item.Done = false
				item.DoneAt = time.Time{}
			}
			next.Items[item.Key] = item
			next.FeedItemIDs[feed] = append(next.FeedItemIDs[feed], item.Key)
		}
	}
	s.data = next
	return s.Save()
}

func (s *Store) MarkDone(item model.Item, doneAt time.Time) error {
	ensureMaps(&s.data)
	item.Done = true
	item.DoneAt = doneAt
	s.data.Items[item.Key] = item
	s.data.Done[item.Key] = DoneState{DoneAt: doneAt, UpdatedAt: item.UpdatedAt}
	return s.Save()
}

func (s *Store) MarkUndone(item model.Item) error {
	ensureMaps(&s.data)
	delete(s.data.Done, item.Key)
	item.Done = false
	item.DoneAt = time.Time{}
	s.data.Items[item.Key] = item
	return s.Save()
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
}
