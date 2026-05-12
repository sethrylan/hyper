package tui

type Action string

const (
	ActionNone       Action = ""
	ActionDown       Action = "down"
	ActionUp         Action = "up"
	ActionExpand     Action = "expand"
	ActionCollapse   Action = "collapse"
	ActionDone       Action = "done"
	ActionCopy       Action = "copy"
	ActionOpen       Action = "open"
	ActionRefresh    Action = "refresh"
	ActionRateLimits Action = "rate_limits"
	ActionHelp       Action = "help"
	ActionNextFeed   Action = "next_feed"
	ActionPrevFeed   Action = "prev_feed"
	ActionQuit       Action = "quit"
)

func ActionForKey(key string) Action {
	switch key {
	case "j", "down":
		return ActionDown
	case "k", "up":
		return ActionUp
	case "l", "right":
		return ActionExpand
	case "h", "left":
		return ActionCollapse
	case "E", "shift+e":
		return ActionDone
	case "y":
		return ActionCopy
	case "o", "enter":
		return ActionOpen
	case "r":
		return ActionRefresh
	case "R", "shift+r":
		return ActionRateLimits
	case "?":
		return ActionHelp
	case "tab":
		return ActionNextFeed
	case "shift+tab":
		return ActionPrevFeed
	case "q", "ctrl+c":
		return ActionQuit
	default:
		return ActionNone
	}
}
