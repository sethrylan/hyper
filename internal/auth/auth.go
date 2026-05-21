//nolint:revive // Internal package exports are shared across command and tests.
package auth

import (
	"errors"
	"fmt"
	"os/exec"

	ghauth "github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/config"
)

type Context struct {
	Account string
	Host    string
	Source  string
	Token   string
}

func Resolve(host string) (Context, error) {
	if host != "github.com" {
		return Context{}, fmt.Errorf("unsupported GitHub host %q; hyper v1 supports github.com only", host)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Context{}, errors.New("GitHub CLI is required for authentication; install gh and run `gh auth login --hostname github.com`")
		}
		return Context{}, fmt.Errorf("find gh: %w", err)
	}
	token, source := ghauth.TokenForHost(host)
	if token == "" {
		return Context{}, errors.New("GitHub authentication is missing or expired; run `gh auth login --hostname github.com`")
	}
	return Context{Account: accountForHost(host), Host: host, Source: source, Token: token}, nil
}

func accountForHost(host string) string {
	cfg, err := config.Read(nil)
	if err != nil {
		return ""
	}
	return accountFromConfig(cfg, host)
}

func accountFromConfig(cfg *config.Config, host string) string {
	account, err := cfg.Get([]string{"hosts", host, "user"})
	if err != nil {
		return ""
	}
	return account
}
