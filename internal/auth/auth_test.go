//nolint:testpackage // These tests exercise package internals.
package auth

import (
	"testing"

	ghconfig "github.com/cli/go-gh/v2/pkg/config"
)

func TestActiveAccountReadsGitHubCLISelection(t *testing.T) {
	config := ghconfig.ReadFromString(`
hosts:
  github.com:
    user: selected-account
`)

	account, err := activeAccount(config, "github.com")
	if err != nil {
		t.Fatal(err)
	}
	if account != "selected-account" {
		t.Fatalf("account = %q, want selected-account", account)
	}
}

func TestActiveAccountRequiresGitHubCLISelection(t *testing.T) {
	if _, err := activeAccount(ghconfig.ReadFromString(""), "github.com"); err == nil {
		t.Fatal("expected missing active account to fail")
	}
}
