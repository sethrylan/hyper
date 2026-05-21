//nolint:testpackage // These tests exercise package internals.
package auth

import (
	"testing"

	"github.com/cli/go-gh/v2/pkg/config"
)

func TestAccountFromConfig(t *testing.T) {
	cfg := config.ReadFromString(`
hosts:
  github.com:
    user: me
`)

	if got := accountFromConfig(cfg, "github.com"); got != "me" {
		t.Fatalf("accountFromConfig() = %q, want me", got)
	}
}

func TestAccountFromConfigMissingUser(t *testing.T) {
	cfg := config.ReadFromString(`
hosts:
  github.com:
    oauth_token: token
`)

	if got := accountFromConfig(cfg, "github.com"); got != "" {
		t.Fatalf("accountFromConfig() = %q, want empty account", got)
	}
}
