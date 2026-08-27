package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sethrylan/hyper/internal/auth"
	"github.com/sethrylan/hyper/internal/autoupdate"
	"github.com/sethrylan/hyper/internal/buildinfo"
	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/demo"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/quota"
	"github.com/sethrylan/hyper/internal/tui"
)

const usage = `Usage: hyper [options]

Options:
  -h, --help     Show this help
      --version  Show version
`

func main() {
	if handleArgs(os.Args[1:], os.Stdout) {
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hyper: %v\n", err)
		os.Exit(1)
	}
}

func handleArgs(args []string, output io.Writer) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "-h", "--help":
		_, _ = fmt.Fprint(output, usage)
	case "--version":
		_, _ = fmt.Fprintln(output, buildinfo.String())
	default:
		return false
	}
	return true
}

func run() error {
	if os.Getenv("HYPER_DEMO") == "1" {
		return runDemo()
	}

	authContext, err := auth.Resolve("github.com")
	if err != nil {
		return err
	}

	store, err := cache.OpenDefault()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}

	budget, err := quota.OpenDefault(authContext.Host, authContext.Account)
	if err != nil {
		return fmt.Errorf("open API usage ledger: %w", err)
	}
	defer func() { _ = budget.Close() }()
	client := github.NewBudgetedClient(authContext.Host, authContext.Token, budget)
	verifyContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	verifyErr := client.VerifyAccount(verifyContext, authContext.Account)
	cancel()
	if verifyErr != nil {
		return fmt.Errorf("verify GitHub CLI active account: %w", verifyErr)
	}
	if updater := releaseUpdater(authContext.Token, budget); updater != nil {
		return runProgram(tui.NewWithUpdater(client, store, authContext.Host, updater))
	}
	return runProgram(tui.New(client, store, authContext.Host))
}

func releaseUpdater(token string, budget *quota.Manager) *autoupdate.Service {
	if !buildinfo.IsRelease() || os.Getenv("HYPER_NO_UPDATE") == "1" {
		return nil
	}
	updater, err := autoupdate.NewBudgeted(token, buildinfo.Version(), budget)
	if err != nil {
		return nil
	}
	return updater
}

func runDemo() error {
	tempDir, err := os.MkdirTemp("", "hyper-demo-")
	if err != nil {
		return fmt.Errorf("create demo cache: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	cachePath := filepath.Join(tempDir, "cache.json")
	if writeErr := os.WriteFile(cachePath, demo.CacheJSON(), 0o600); writeErr != nil {
		return fmt.Errorf("seed demo cache: %w", writeErr)
	}
	store, err := cache.Open(cachePath)
	if err != nil {
		return fmt.Errorf("open demo cache: %w", err)
	}

	return runProgram(tui.NewCached(store, "github.com"))
}

func runProgram(model tui.Model) error {
	_, err := tea.NewProgram(model).Run()
	return err
}
