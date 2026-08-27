//nolint:testpackage // These tests exercise package internals.
package quota

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBudgetLimitIsStrictlyBelowTwentyFivePercent(t *testing.T) {
	if got := BudgetLimit(5000); got != 1249 {
		t.Fatalf("BudgetLimit(5000) = %d, want 1249", got)
	}
	if got := BudgetLimit(30); got != 7 {
		t.Fatalf("BudgetLimit(30) = %d, want 7", got)
	}
}

func TestOpenDefaultUsesSeparateUsageFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	manager, err := OpenDefault("github.com", "me")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if want := filepath.Join(home, ".hyper", "api-usage.json"); manager.path != want {
		t.Fatalf("usage path = %q, want %q", manager.path, want)
	}
}

func TestReservationsPersistAndStopAtLimit(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "api-usage.json")
	manager, openErr := Open(path, "github.com", "me")
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() { _ = manager.Close() })
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
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(path, "github.com", "me")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })
	if got := reloaded.Status(now).Resources[ResourceGraphQL].Used; got != 4 {
		t.Fatalf("persisted usage = %d, want 4", got)
	}
}

func TestReconcileReleasesConservativeReservation(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	manager := NewManager("github.com", "me")
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

func TestReconcileRejectsCostAboveReservedUpperBound(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	manager := NewManager("github.com", "me")
	reservation, err := manager.Reserve(ResourceGraphQL, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(reservation, 6); err == nil {
		t.Fatal("expected actual cost above reserved upper bound to fail")
	}
	if got := manager.Status(now).Resources[ResourceGraphQL].Used; got != 5 {
		t.Fatalf("usage after invalid reconciliation = %d, want conservative reservation retained", got)
	}
}

func TestOpenRejectsConcurrentLedgerOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-usage.json")
	first, openErr := Open(path, "github.com", "me")
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, secondOpenErr := Open(path, "github.com", "me"); secondOpenErr == nil {
		t.Fatal("expected second manager to be rejected while the ledger is owned")
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	second, secondOpenErr := Open(path, "github.com", "me")
	if secondOpenErr != nil {
		t.Fatalf("open after releasing ledger = %v", secondOpenErr)
	}
	t.Cleanup(func() { _ = second.Close() })
}

func TestReserveKeepingProtectsHigherPriorityCapacity(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	manager := NewManager("github.com", "me")
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
	path := filepath.Join(t.TempDir(), "api-usage.json")
	manager, openErr := Open(path, "github.com", "me")
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() { _ = manager.Close() })
	manager.state = State{
		Account: "me",
		Host:    "github.com",
		Resources: map[Resource]Window{
			ResourceGraphQL: {Limit: 5000, ResetAt: now.Add(-time.Second), Used: 1249},
		},
	}
	if _, err := manager.Reserve(ResourceGraphQL, 1, now); err != nil {
		t.Fatal(err)
	}
	if got := manager.Status(now).Resources[ResourceGraphQL].Used; got != 1 {
		t.Fatalf("usage after reset = %d, want 1", got)
	}
}
