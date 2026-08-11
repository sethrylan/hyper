package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/sethrylan/hyper/internal/auth"
	"github.com/sethrylan/hyper/internal/autoupdate"
	"github.com/sethrylan/hyper/internal/buildinfo"
	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/demo"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/tui"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = fmt.Fprintln(os.Stdout, buildinfo.String())
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hyper: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("HYPER_DEMO") == "1" {
		return runDemo()
	}

	ctx, err := auth.Resolve("github.com")
	if err != nil {
		return err
	}

	store, err := cache.OpenDefault()
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}

	client := github.NewClient(ctx.Host, ctx.Token)
	if updater := releaseUpdater(ctx.Token); updater != nil {
		return runProgram(tui.NewWithUpdater(client, store, ctx.Host, updater))
	}
	return runProgram(tui.New(client, store, ctx.Host))
}

func releaseUpdater(token string) *autoupdate.Service {
	if !buildinfo.IsRelease() || os.Getenv("HYPER_NO_UPDATE") == "1" {
		return nil
	}
	updater, err := autoupdate.New(token, buildinfo.Version())
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
