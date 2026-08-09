//nolint:testpackage // These tests exercise package internals.
package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sethrylan/hyper/internal/cache"
)

func TestNewCachedDisablesGitHubActions(t *testing.T) {
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	refreshedAt := time.Date(2000, time.January, 1, 15, 4, 0, 0, time.UTC)
	if err := store.Replace("mona", "github.com", nil, refreshedAt); err != nil {
		t.Fatal(err)
	}

	m := NewCached(store, "github.com")
	if m.service != nil || m.loading {
		t.Fatal("cached model should not have a GitHub service or active refresh")
	}
	if m.status != "refreshed 3:04PM" {
		t.Fatalf("status = %q, want cached refresh time", m.status)
	}
	for _, action := range []Action{ActionRefresh, ActionRateLimits} {
		updated, cmd := m.handleAction(action)
		if cmd != nil || updated.loading || updated.showRateLimits {
			t.Fatalf("action %q started a GitHub operation", action)
		}
	}
	help := m.renderHelp() + m.renderFooterHelp()
	if strings.Contains(help, "refresh") || strings.Contains(help, "rate limits") {
		t.Fatalf("cached help advertises GitHub actions: %q", help)
	}
}
