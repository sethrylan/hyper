# hyper TUI Specification

## Overview

`hyper` is a Go terminal UI for GitHub work queues. It shows a small, high-signal set of GitHub items for engineers working across multiple repositories and organizations:

- Important Notifications
- My Pull Requests
- My Issues

The app is intentionally read-mostly for v1. It may mutate local state, but it must not write to the GitHub API.

## Goals

- Provide a fast grouped-outline TUI for the user’s GitHub work.
- Use GitHub CLI authentication, then call GitHub APIs directly from Go.
- Keep the first version zero-config.
- Use Bubble Tea v2, Lip Gloss v2, and Bubbles v2.

## Non-goals

- Custom filters.
- GHES support.
- Multiple GitHub accounts.
- AI summaries.
- Commenting, reviewing, approving, merging, closing, reopening, assigning, subscribing, or any other write action against GitHub.
- System notifications, menu bar behavior, or native macOS integrations beyond opening URLs and clipboard support.

## Platform and command

- Binary name: `hyper`.
- GitHub host: `github.com` only.
- Primary target: engineers working across multiple GitHub orgs and repos.

## Authentication

`hyper` must use the GitHub CLI for authentication only.

- On startup, verify `gh` is installed and authenticated for `github.com`.
- Resolve the locally selected GitHub CLI account before opening its API usage ledger, then verify the token owner with a budgeted request before starting refresh or update commands.
- If authentication is missing or expired, fail with clear setup instructions, for example `gh auth login`.
- After auth validation, use the token to call GitHub APIs directly from Go rather than shelling out to `gh api`.
- Prefer `github.com/cli/go-gh` for resolving the authenticated host/account/token context, combined with direct `net/http` calls for GraphQL where needed. Use `github.com/google/go-github` only where it materially reduces REST boilerplate.

## GitHub API strategy

Use a hybrid API strategy:

- REST for notification threads and initial notification subject enrichment.
- GraphQL for batched subject-node enrichment and search/list details.
- REST for supporting metadata where it materially reduces boilerplate.

Do not use GraphQL `viewer.notificationThreads`. Hyperlist Mac has access to that field in its generated/internal schema, but it is not available through the normal GitHub CLI GraphQL context that `hyper` uses. Important Notifications should use REST notifications plus best-effort REST subject enrichment, then batch-enrich REST subject `node_id` values with public GraphQL `nodes(ids: [...])` where possible.

## Built-in feeds

### Important Notifications

Important Notifications predicate:

```text
author ==[c] "$me"
OR ANY assignees CONTAINS[c] "$me"
OR saved ==[c] 1
OR reason ==[c] "MENTION"
OR ANY reviewers CONTAINS[c] "$me"
OR ANY reviewRequests CONTAINS[c] "$me"
```

In Go, implement this as explicit predicate logic over a normalized item model, not as an NSPredicate clone.

Important Notifications should be based on the default REST notifications feed, including read and unread notification threads, capped at 500 fetched notifications. REST notification subjects should be enriched with public GraphQL node data to recover fields needed by the Important predicate, including PR reviewers, PR review requests, discussion metadata, assignees, author, repository, state, draft/merged state, and issue state reason where available.

Because REST notifications do not expose all fields available to Hyperlist Mac's internal GraphQL notification-thread API, saved notification state cannot be fully recovered. To get closer to Hyperlist Mac's Important list, v1 should also union in de-duplicated public search results for recent open work involving the viewer, including authored, assigned, and review-requested items, and then apply the same normalized Important predicate.

### My Pull Requests

```text
is:open is:pr author:@me archived:false created:>@RELATIVE_DATE
```

`@RELATIVE_DATE` defaults to 30 days ago.

### My Issues

```text
is:open is:issue author:@me archived:false created:>@RELATIVE_DATE
```

`@RELATIVE_DATE` defaults to 30 days ago.

## Fetching, polling, and rate limits

- Fetch caps:
  - Notifications: 500.
  - Search results for My Pull Requests and My Issues: 500 each.
- Poll periodically.
- Cadence:
  - Refresh the newest page of My Pull Requests approximately every 5 seconds with one lightweight GraphQL request, merging those changes into the cached feed.
  - Refresh REST notifications incrementally approximately every 15 seconds, adding or updating Important items without removing cached items.
  - Reconcile all three feeds authoritatively approximately every 10 minutes. This refresh also updates rich Important-item metadata and removes stale items.
- Run two independent refresh lanes and apply each successful result immediately. A slow background refresh must not delay My Pull Requests; the ten-minute authoritative refresh replaces all three feeds so stale pull requests are eventually removed.
- Keep a local cache so the app can render recent results quickly on startup.
- Store each feed and its successful-refresh timestamp independently so a fast pull request refresh cannot advance the incremental notification cursor or overwrite richer Important-item data.
- Keep Hyper's own API usage strictly below 25% of every GitHub primary rate-limit window. With 5,000-point core and GraphQL limits, Hyper may use at most 1,249 requests or points per window; with a 30-request search limit, it may use at most 7.
- Allow only one running Hyper process to own the shared API usage ledger at a time.
- Persist API usage reservations before sending requests, reconcile GraphQL reservations against `rateLimit.cost`, and retain enough GraphQL capacity for the five-second pull request cadence before admitting lower-priority work.
- On rate-limit pressure:
  - Show a warning in the status bar.
  - Reduce query depth before failing the whole app.
  - Defer lower-priority refreshes before slowing My Pull Requests.
  - Do not retry a primary rate-limit failure until a later scheduled refresh.
  - Prefer preserving the current cached view over clearing the screen.
- Read account-wide core, GraphQL, and search quota status from the REST rate-limit endpoint so the rate-limit screen still works after GraphQL is exhausted.
- `r` refreshes the pull request lane from My Pull Requests and runs the authoritative background lane from Important Notifications or My Issues.

## Cache and local state

Use simple local storage under the user cache/config directory. Prefer XDG-compatible locations via Go conventions; exact path can be implementation-defined.

Cache:

- Independent item payloads for each of the three feeds.
- A successful-refresh timestamp for each feed.
- Local done state.
- Current authenticated account and host metadata.

Persist Hyper's API usage ledger separately from the feed cache so each API reservation does not rewrite cached items.

Local state identity must include host or full URL, not only GitHub node ID, so future host/account support does not collide with existing cache data.

## Local done behavior

`hyper` must not mark GitHub notifications done remotely.

When the user marks an item done:

- Update local state only.
- Show the item in place with a checkmark and dim/gray styling.
- On the next successful refresh, remove the item from display if it has not changed since being marked done.
- If the item was updated after the local done timestamp, re-add it to the feed and clear or ignore the stale local done state for display.

For v1, local done applies to notification-backed items. If a PR or issue has no notification thread, either disable mark done for that row or record local done only for that feed; the UI must make the behavior consistent and visible.

## Data model

Normalize notifications, issues, and PRs into a single internal item model.

Required fields:

- Stable key including host.
- GitHub node ID when available.
- Notification thread ID when available.
- Type: notification, pull request, issue, discussion, or unknown.
- Repository owner/name.
- Title.
- Author login.
- URL for opening/copying.
- Updated timestamp.
- Created timestamp when available.
- Notification reason when available.
- Read/unread when available.
- Saved when available.
- Local done state and local done timestamp.
- Assignees.
- Reviewers.
- Review requests.
- Source feeds containing the item.

## Layout and UX

Use a grouped-outline layout

Required layout:

- Top or side feed selector for:
  - Important Notifications
  - My Pull Requests
  - My Issues
- Main outline grouped by repository, then by date.
- Rows sorted by repository, then updated date.
- Status/footer area.
- Discoverable shortcut footer.
- Fullscreen help opened with `?`.

Rows should include enough information for fast triage:

- Done/check state.
- Type marker.
- Repository.
- Title.
- Author.
- Notification reason when available.
- Updated age/date.

Omit labels, review state, unread count, and CI status in v1.

## Selection and actions

Use single selection only for v1.

Required actions:

| Action | Required key | Behavior |
| --- | --- | --- |
| Move down | `j` or down arrow | Select next visible item |
| Move up | `k` or up arrow | Select previous visible item |
| Expand group | `l` or right arrow | Expand selected group |
| Collapse group | `h` or left arrow | Collapse selected group or move to parent |
| Mark done | `E` | Apply local done behavior |
| Copy URL | `y` | Copy selected item URL |
| Open in browser | `o` | Open selected item’s underlying URL |
| Refresh | `r` | Trigger manual refresh |
| Help | `?` | Show fullscreen keybinding/help view |
| Quit | `q` or Ctrl+C | Exit |

Shift+E for Mark Done and Shift+Command+C for Copy URL. Terminals generally cannot rely on Command-key input, so v1 uses discoverable terminal-native fallbacks: `E` for mark done and `y` for copy URL.

## Clipboard behavior

Use terminal clipboard integration.

Recommended order:

1. OSC52 when supported.
2. Platform clipboard helper when obvious and available, such as `pbcopy` on macOS.
3. If copying fails, print the URL in the status line.

Do not treat clipboard failure as fatal.

## Opening URLs

Enter and `o` should open the selected item’s underlying GitHub URL in the browser.

- For PRs and issues, use the subject URL.
- For notifications, the underlying subject URL is acceptable.
- Notification unread anchors are not required in v1.

## Status bar

Keep the status bar simple. It should show:

- Current authenticated account.
- Loading/refresh indicator.
- Rate-limit warning or reduced-depth warning.
- Copy/open/done action feedback.
- Authentication or API errors.

## Bubble Tea v2 requirements

Use the v2 module paths:

```go
import tea "charm.land/bubbletea/v2"
import "charm.land/lipgloss/v2"
```

Use Bubbles v2 imports when components are needed:

```go
import "charm.land/bubbles/v2/..."
```

Bubble Tea v2 implementation constraints:

- `View()` returns `tea.View`, not `string`.
- Use declarative `tea.View` fields for alternate screen, mouse mode, window title, cursor, and keyboard enhancements.
- Use `tea.KeyPressMsg` for key handling.
- Match keys with v2 semantics; for example, space is `"space"` if needed.
- Use `tea.RequestWindowSize`, not `tea.WindowSize()`.
- Use `tea.Sequence`, not `tea.Sequentially`.

Lip Gloss v2 implementation constraints:

- Use `charm.land/lipgloss/v2`.
- `lipgloss.Color("...")` returns `color.Color`.
- Do not use removed renderer APIs.
- In Bubble Tea, let Bubble Tea handle output color downsampling.
- For adaptive colors, request background color in `Init` with `tea.RequestBackgroundColor` and build styles after receiving `tea.BackgroundColorMsg`.

Bubbles v2 implementation constraints:

- Use `charm.land/bubbles/v2`.
- Prefer v2 getter/setter APIs for width and height.
- Use `DefaultKeyMap()` functions where applicable.
- Pass explicit dark/light style state where required.

## Suggested package shape

The final implementation can vary, but the spec expects these responsibilities to stay separated:

- `cmd/hyper`: command entrypoint.
- `internal/auth`: `gh` auth detection and token/context resolution.
- `internal/github`: GraphQL/REST clients, queries, pagination, rate-limit handling.
- `internal/model`: normalized item/feed/cache models.
- `internal/cache`: local cache and local done state.
- `internal/filter`: built-in feed query construction and Important predicate evaluation.
- `internal/tui`: Bubble Tea model, update logic, views, keybindings, styles.
- `internal/browser`: browser opening.
- `internal/clipboard`: clipboard/OSC52/status fallback.

## Testing expectations

Keep tests minimal but cover the behavior most likely to regress:

- Built-in query construction, including 30-day `@RELATIVE_DATE`.
- Important predicate evaluation.
- Local done reconciliation when items are unchanged vs updated.
- Keybinding mapping for required actions.
- Basic cache round-trip for normalized items/local done state.

Integration tests requiring live GitHub auth are optional and should be skipped by default.
