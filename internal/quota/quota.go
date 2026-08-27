// Package quota tracks Hyper's share of GitHub API rate-limit windows.
//
//nolint:revive // Internal package exports are shared across application packages and tests.
package quota

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sethrylan/hyper/internal/jsonfile"
)

const budgetDivisor = 4

type Resource string

const (
	ResourceCore    Resource = "core"
	ResourceGraphQL Resource = "graphql"
	ResourceSearch  Resource = "search"
)

type Window struct {
	Limit   int       `json:"limit,omitempty"`
	ResetAt time.Time `json:"reset_at,omitzero"`
	Used    int       `json:"used,omitempty"`
}

type State struct {
	Account   string              `json:"account,omitempty"`
	Host      string              `json:"host,omitempty"`
	Resources map[Resource]Window `json:"resources,omitempty"`
}

type Reservation struct {
	Cost        int
	Resource    Resource
	windowReset time.Time
}

type ExhaustedError struct {
	Limit    int
	ResetAt  time.Time
	Resource Resource
	Used     int
}

func (e ExhaustedError) Error() string {
	if e.ResetAt.IsZero() {
		return fmt.Sprintf("Hyper %s API budget exhausted (%d/%d used)", e.Resource, e.Used, e.Limit)
	}
	//nolint:gosmopolitan // Reset times are most useful in the user's local time.
	return fmt.Sprintf("Hyper %s API budget exhausted (%d/%d used); resumes at %s", e.Resource, e.Used, e.Limit, e.ResetAt.Local().Format(time.Kitchen))
}

type Manager struct {
	mu    sync.Mutex
	path  string
	state State
}

func NewManager(host, account string) *Manager {
	return newManager("", State{}, host, account)
}

func OpenDefault(host, account string) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home dir: %w", err)
	}
	return Open(filepath.Join(home, ".hyper", "api-usage.json"), host, account)
}

func Open(path, host, account string) (*Manager, error) {
	var state State
	if _, err := jsonfile.Read(path, &state); err != nil {
		return nil, fmt.Errorf("read API usage: %w", err)
	}
	return newManager(path, state, host, account), nil
}

func newManager(path string, state State, host, account string) *Manager {
	ensureResources(&state)
	if state.Host != "" && state.Host != host || state.Account != "" && account != "" && state.Account != account {
		state = State{}
		ensureResources(&state)
	}
	state.Host = host
	if account != "" {
		state.Account = account
	}
	return &Manager{path: path, state: state}
}

func (m *Manager) SetIdentity(host, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Host == host && (account == "" || m.state.Account == account) {
		return nil
	}
	if m.state.Host != "" && m.state.Host != host || m.state.Account != "" && account != "" && m.state.Account != account {
		m.state = State{}
		ensureResources(&m.state)
	}
	m.state.Host = host
	if account != "" {
		m.state.Account = account
	}
	return m.saveLocked()
}

func (m *Manager) Configure(resource Resource, limit int, resetAt, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetExpiredLocked(now)
	window := m.state.Resources[resource]
	original := window
	if limit > 0 {
		window.Limit = limit
	}
	if !resetAt.IsZero() {
		window.ResetAt = resetAt
	}
	m.state.Resources[resource] = window
	if window == original {
		return nil
	}
	return m.saveLocked()
}

func (m *Manager) Reserve(resource Resource, cost int, now time.Time) (Reservation, error) {
	return m.ReserveKeeping(resource, cost, 0, now)
}

// ReserveKeeping admits a request only when minimumRemaining capacity remains
// available for higher-priority work in the same rate-limit window.
func (m *Manager) ReserveKeeping(resource Resource, cost, minimumRemaining int, now time.Time) (Reservation, error) {
	if cost <= 0 {
		return Reservation{}, errors.New("quota reservation cost must be positive")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetExpiredLocked(now)
	window := m.state.Resources[resource]
	if window.Limit <= 0 {
		window.Limit = defaultLimit(resource)
	}
	if window.ResetAt.IsZero() {
		window.ResetAt = now.Add(windowDuration(resource))
	}
	limit := BudgetLimit(window.Limit)
	if window.Used+cost+max(0, minimumRemaining) > limit {
		return Reservation{}, ExhaustedError{Limit: limit, ResetAt: window.ResetAt, Resource: resource, Used: window.Used}
	}
	window.Used += cost
	m.state.Resources[resource] = window
	if err := m.saveLocked(); err != nil {
		window.Used -= cost
		m.state.Resources[resource] = window
		return Reservation{}, fmt.Errorf("persist API budget reservation: %w", err)
	}
	return Reservation{Cost: cost, Resource: resource, windowReset: window.ResetAt}, nil
}

func (m *Manager) Reconcile(reservation Reservation, actual int) error {
	if reservation.Cost <= 0 || actual <= 0 || actual == reservation.Cost {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	window := m.state.Resources[reservation.Resource]
	if !window.ResetAt.Equal(reservation.windowReset) {
		return nil
	}
	window.Used += actual - reservation.Cost
	if window.Used < 0 {
		window.Used = 0
	}
	m.state.Resources[reservation.Resource] = window
	return m.saveLocked()
}

func (m *Manager) Status(now time.Time) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetExpiredLocked(now)
	return cloneState(m.state)
}

func BudgetLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	return (limit - 1) / budgetDivisor
}

func (m *Manager) resetExpiredLocked(now time.Time) {
	ensureResources(&m.state)
	for resource, window := range m.state.Resources {
		if window.ResetAt.IsZero() || now.Before(window.ResetAt) {
			continue
		}
		window.Used = 0
		window.ResetAt = now.Add(windowDuration(resource))
		m.state.Resources[resource] = window
	}
}

func (m *Manager) saveLocked() error {
	if m.path == "" {
		return nil
	}
	if err := jsonfile.Write(m.path, m.state); err != nil {
		return fmt.Errorf("save API usage: %w", err)
	}
	return nil
}

func ensureResources(state *State) {
	if state.Resources == nil {
		state.Resources = map[Resource]Window{}
	}
	for _, resource := range []Resource{ResourceCore, ResourceGraphQL, ResourceSearch} {
		window := state.Resources[resource]
		if window.Limit == 0 {
			window.Limit = defaultLimit(resource)
		}
		state.Resources[resource] = window
	}
}

func cloneState(state State) State {
	clone := state
	clone.Resources = make(map[Resource]Window, len(state.Resources))
	maps.Copy(clone.Resources, state.Resources)
	return clone
}

func defaultLimit(resource Resource) int {
	if resource == ResourceSearch {
		return 30
	}
	return 5000
}

func windowDuration(resource Resource) time.Duration {
	if resource == ResourceSearch {
		return time.Minute
	}
	return time.Hour
}
