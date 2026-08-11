package main

import (
	"fmt"
	"io"
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
	if handleInformationalArgs(os.Args[1:], os.Stdout) {
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hyper: %v\n", err)
		os.Exit(1)
	}
}

func handleInformationalArgs(args []string, output io.Writer) bool {
	if len(args) != 1 || args[0] != "--version" {
		return false
	}
	_, _ = fmt.Fprintln(output, buildinfo.String())
	return true
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
	updater := releaseUpdater(ctx.Token)
	return runProgram(tui.NewWithUpdater(client, store, ctx.Host, updater))
}

func releaseUpdater(token string) *autoupdate.Service {
	if !autoUpdateEnabled(buildinfo.IsRelease(), os.Getenv("HYPER_NO_UPDATE")) {
		return nil
	}
	updater, err := autoupdate.New(token, buildinfo.Version())
	if err != nil {
		return nil
	}
	return updater
}

func autoUpdateEnabled(release bool, optOut string) bool {
	return release && optOut != "1"
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
