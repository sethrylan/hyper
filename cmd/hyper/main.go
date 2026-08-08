package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sethrylan/hyper/internal/auth"
	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/demo"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/tui"
)

func main() {
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
	return runProgram(tui.New(client, store, ctx.Host))
}

func runDemo() error {
	tempDir, err := os.MkdirTemp("", "hyper-demo-")
	if err != nil {
		return fmt.Errorf("create demo cache: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	store, err := cache.Open(filepath.Join(tempDir, "cache.json"))
	if err != nil {
		return fmt.Errorf("open demo cache: %w", err)
	}

	return runProgram(tui.New(demo.New(time.Now()), store, "github.com"))
}

func runProgram(model tui.Model) error {
	_, err := tea.NewProgram(model).Run()
	return err
}
