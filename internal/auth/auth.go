//nolint:revive // Internal package exports are shared across command and tests.
package auth

import (
	"errors"
	"fmt"
	"os/exec"

	ghauth "github.com/cli/go-gh/v2/pkg/auth"
	ghconfig "github.com/cli/go-gh/v2/pkg/config"
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
	config, err := ghconfig.Read(nil)
	if err != nil {
		return Context{}, fmt.Errorf("read GitHub CLI configuration: %w", err)
	}
	account, err := activeAccount(config, host)
	if err != nil {
		return Context{}, err
	}
	return Context{Account: account, Host: host, Source: source, Token: token}, nil
}

func activeAccount(config *ghconfig.Config, host string) (string, error) {
	account, err := config.Get([]string{"hosts", ghauth.NormalizeHostname(host), "user"})
	if err != nil || account == "" {
		return "", errors.New("GitHub CLI active account is missing; run `gh auth login --hostname github.com`")
	}
	return account, nil
}
