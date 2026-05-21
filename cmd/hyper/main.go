package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/sethrylan/hyper/internal/auth"
	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/tui"
)

func main() {
	ctx, err := auth.Resolve("github.com")
	if err != nil {
		fmt.Fprintf(os.Stderr, "hyper: %v\n", err)
		os.Exit(1)
	}

	store, err := cache.OpenDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "hyper: open cache: %v\n", err)
		os.Exit(1)
	}

	client := github.NewClient(ctx.Host, ctx.Token)
	model := tui.New(client, store, ctx.Host, ctx.Account)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "hyper: %v\n", err)
		os.Exit(1)
	}
}
