//nolint:testpackage // These tests exercise package internals.
package quota

import (
	"errors"
	"testing"
	"time"
)

type stateStore struct{ state State }

func (s *stateStore) QuotaState() State { return s.state }
func (s *stateStore) SaveQuotaState(state State) error {
	s.state = state
	return nil
}

func TestBudgetLimitIsStrictlyBelowTwentyFivePercent(t *testing.T) {
	if got := BudgetLimit(5000); got != 1249 {
		t.Fatalf("BudgetLimit(5000) = %d, want 1249", got)
	}
	if got := BudgetLimit(30); got != 7 {
		t.Fatalf("BudgetLimit(30) = %d, want 7", got)
	}
}

func TestReservationsPersistAndStopAtLimit(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	store := &stateStore{}
	manager := NewManager(store, "github.com", "me")
	if err := manager.Configure(ResourceGraphQL, 20, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := manager.Reserve(ResourceGraphQL, 1, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.Reserve(ResourceGraphQL, 1, now); err == nil {
		t.Fatal("expected strict 4/20 budget to be exhausted")
	} else {
		var exhausted ExhaustedError
		if !errors.As(err, &exhausted) {
			t.Fatalf("error = %T, want ExhaustedError", err)
		}
	}
	reloaded := NewManager(store, "github.com", "me")
	if got := reloaded.Status(now).Resources[ResourceGraphQL].Used; got != 4 {
		t.Fatalf("persisted usage = %d, want 4", got)
	}
}

func TestReconcileReleasesConservativeReservation(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	manager := NewManager(nil, "github.com", "me")
	reservation, err := manager.Reserve(ResourceGraphQL, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(reservation, 1); err != nil {
		t.Fatal(err)
	}
	if got := manager.Status(now).Resources[ResourceGraphQL].Used; got != 1 {
		t.Fatalf("reconciled usage = %d, want 1", got)
	}
}

func TestReserveKeepingProtectsHigherPriorityCapacity(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	manager := NewManager(nil, "github.com", "me")
	if err := manager.Configure(ResourceGraphQL, 20, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveKeeping(ResourceGraphQL, 1, 3, now); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReserveKeeping(ResourceGraphQL, 1, 3, now); err == nil {
		t.Fatal("expected reservation to preserve three points for higher-priority work")
	}
}

func TestExpiredWindowResetsUsage(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	store := &stateStore{state: State{
		Account: "me",
		Host:    "github.com",
		Resources: map[Resource]Window{
			ResourceGraphQL: {Limit: 5000, ResetAt: now.Add(-time.Second), Used: 1249},
		},
	}}
	manager := NewManager(store, "github.com", "me")
	if _, err := manager.Reserve(ResourceGraphQL, 1, now); err != nil {
		t.Fatal(err)
	}
	if got := manager.Status(now).Resources[ResourceGraphQL].Used; got != 1 {
		t.Fatalf("usage after reset = %d, want 1", got)
	}
}
