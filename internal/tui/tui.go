package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sethrylan/hyper/internal/browser"
	"github.com/sethrylan/hyper/internal/cache"
	"github.com/sethrylan/hyper/internal/clipboard"
	"github.com/sethrylan/hyper/internal/github"
	"github.com/sethrylan/hyper/internal/model"
)

const (
	shortRefresh = time.Minute
	fullRefresh  = 5 * time.Minute
	progressTick = time.Second
)

type service interface {
	CurrentProgress() github.RefreshProgress
	RateLimits(context.Context) (github.RateLimits, error)
	Refresh(context.Context) (github.RefreshResult, error)
}

type Model struct {
	account           string
	activeFeed        int
	expanded          map[string]bool
	feeds             map[model.Feed][]model.Item
	height            int
	host              string
	loading           bool
	loadingAt         time.Time
	rateWarning       string
	rateLimits        github.RateLimits
	rateLimitsErr     string
	rateLimitsLoading bool
	refreshProgress   github.RefreshProgress
	rows              []row
	selected          int
	selectedByFeed    map[model.Feed]int
	service           service
	showHelp          bool
	showRateLimits    bool
	spinner           int
	status            string
	store             *cache.Store
	width             int
}

type rowKind int

const (
	rowRepo rowKind = iota
	rowItem
)

type row struct {
	item model.Item
	key  string
	kind rowKind
	repo string
}

type refreshMsg struct {
	result github.RefreshResult
	err    error
}

type tickMsg struct {
	full bool
}

type progressTickMsg struct{}

type rateLimitsMsg struct {
	limits github.RateLimits
	err    error
}

func New(service service, store *cache.Store, host string) Model {
	data := store.Data()
	m := Model{
		account:        data.Account,
		expanded:       map[string]bool{},
		feeds:          feedsFromCache(data),
		host:           host,
		loading:        true,
		loadingAt:      time.Now(),
		selectedByFeed: map[model.Feed]int{},
		service:        service,
		status:         cacheStatus(data),
		store:          store,
	}
	for _, feed := range model.Feeds {
		m.expanded["feed:"+string(feed)] = true
	}
	m.rebuildRows()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestWindowSize, tea.RequestBackgroundColor, m.startRefreshCmd(), tickCmd(false), tickCmd(true))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.BackgroundColorMsg:
		return m, nil
	case tea.KeyPressMsg:
		return m.handleAction(ActionForKey(msg.Keystroke()))
	case refreshMsg:
		m.loading = false
		m.refreshProgress = github.RefreshProgress{}
		if msg.err != nil {
			m.status = "refresh failed: " + msg.err.Error()
			return m, nil
		}
		if err := m.store.Replace(msg.result.Account, m.host, msg.result.Feeds, msg.result.RefreshedAt); err != nil {
			m.status = "cache save failed: " + err.Error()
			return m, nil
		}
		m.account = msg.result.Account
		m.feeds = feedsFromCache(m.store.Data())
		m.rateWarning = msg.result.RateWarning
		m.status = "refreshed " + msg.result.RefreshedAt.Format(time.Kitchen)
		m.rebuildRows()
	case tickMsg:
		cmds := []tea.Cmd{tickCmd(msg.full)}
		if !m.loading {
			m.loading = true
			m.loadingAt = time.Now()
			cmds = append(cmds, m.startRefreshCmd())
		}
		return m, tea.Batch(cmds...)
	case progressTickMsg:
		if m.loading {
			m.spinner++
			m.refreshProgress = m.service.CurrentProgress()
			return m, progressTickCmd()
		}
	case rateLimitsMsg:
		m.rateLimitsLoading = false
		if msg.err != nil {
			m.rateLimitsErr = msg.err.Error()
			return m, nil
		}
		m.rateLimits = msg.limits
		m.rateLimitsErr = ""
		if msg.limits.Account != "" {
			m.account = msg.limits.Account
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	content := m.renderWithBorder()
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "hyper"
	return view
}

func (m Model) handleAction(action Action) (Model, tea.Cmd) {
	if m.showHelp && action != ActionHelp && action != ActionQuit {
		m.showHelp = false
	}
	if m.showRateLimits && action != ActionRateLimits && action != ActionQuit {
		m.showRateLimits = false
	}
	switch action {
	case ActionDown:
		m.selectNext()
	case ActionUp:
		m.selectPrev()
	case ActionExpand:
		m.expandSelected()
	case ActionCollapse:
		m.collapseSelected()
	case ActionDone:
		return m.markSelectedDone()
	case ActionCopy:
		return m.copySelected()
	case ActionOpen:
		return m.openSelected()
	case ActionRefresh:
		if !m.loading {
			m.loading = true
			m.loadingAt = time.Now()
			return m, m.startRefreshCmd()
		}
	case ActionRateLimits:
		if m.showRateLimits {
			m.showRateLimits = false
			return m, nil
		}
		m.showHelp = false
		m.showRateLimits = true
		m.rateLimitsLoading = true
		m.rateLimitsErr = ""
		return m, m.rateLimitsCmd()
	case ActionHelp:
		m.showHelp = !m.showHelp
		if m.showHelp {
			m.showRateLimits = false
		}
	case ActionNextFeed:
		m.rememberSelection()
		m.activeFeed = (m.activeFeed + 1) % len(model.Feeds)
		m.restoreSelection()
		m.rebuildRows()
	case ActionPrevFeed:
		m.rememberSelection()
		m.activeFeed = (m.activeFeed - 1 + len(model.Feeds)) % len(model.Feeds)
		m.restoreSelection()
		m.rebuildRows()
	case ActionQuit:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) selectNext() {
	for i := m.selected + 1; i < len(m.rows); i++ {
		if m.selectable(i) {
			m.selected = i
			m.rememberSelection()
			return
		}
	}
}

func (m *Model) selectPrev() {
	for i := m.selected - 1; i >= 0; i-- {
		if m.selectable(i) {
			m.selected = i
			m.rememberSelection()
			return
		}
	}
}

func (m Model) selectable(index int) bool {
	if index < 0 || index >= len(m.rows) {
		return false
	}
	row := m.rows[index]
	return row.kind == rowItem || (row.kind == rowRepo && !m.expanded[row.key])
}

func (m *Model) expandSelected() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.selected]
	if selected.kind == rowRepo {
		m.expanded[selected.key] = true
		m.rebuildRows()
	}
}

func (m *Model) collapseSelected() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.selected]
	if selected.kind == rowRepo && m.expanded[selected.key] {
		m.expanded[selected.key] = false
		m.rebuildRows()
		return
	}
	if selected.kind == rowItem {
		for i := range m.rows {
			if m.rows[i].kind == rowRepo && m.rows[i].repo == selected.repo {
				m.selected = i
				m.rememberSelection()
				return
			}
		}
	}
}

func (m Model) markSelectedDone() (Model, tea.Cmd) {
	item, ok := m.selectedItem()
	if !ok {
		m.status = "select an item to mark done"
		return m, nil
	}
	if !m.localDoneEnabled() {
		m.status = "local done is only available in Important Notifications"
		return m, nil
	}
	if item.Done {
		if err := m.store.MarkUndone(item); err != nil {
			m.status = "mark undone failed: " + err.Error()
			return m, nil
		}
		item.Done = false
		item.DoneAt = time.Time{}
		m.replaceItem(item)
		m.status = "marked undone"
		m.rebuildRows()
		return m, nil
	}
	if err := m.store.MarkDone(item, time.Now()); err != nil {
		m.status = "mark done failed: " + err.Error()
		return m, nil
	}
	item.Done = true
	m.replaceItem(item)
	m.status = "marked done"
	m.rebuildRows()
	return m, nil
}

func (m Model) localDoneEnabled() bool {
	return model.Feeds[m.activeFeed] == model.FeedImportantNotifications
}

func (m Model) copySelected() (Model, tea.Cmd) {
	item, ok := m.selectedItem()
	if !ok {
		m.status = "select an item to copy"
		return m, nil
	}
	if err := clipboard.CopyLink(item.URL, item.Title); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = "copied link"
	return m, nil
}

func (m Model) openSelected() (Model, tea.Cmd) {
	item, ok := m.selectedItem()
	if !ok {
		m.status = "select an item to open"
		return m, nil
	}
	if err := browser.Open(item.URL); err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = "opened URL"
	return m, nil
}

func (m Model) selectedItem() (model.Item, bool) {
	if m.selected < 0 || m.selected >= len(m.rows) {
		return model.Item{}, false
	}
	row := m.rows[m.selected]
	return row.item, row.kind == rowItem
}

func (m *Model) replaceItem(item model.Item) {
	feed := model.Feeds[m.activeFeed]
	for i := range m.feeds[feed] {
		if m.feeds[feed][i].Key == item.Key {
			m.feeds[feed][i] = item
		}
	}
}

func (m Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		result, err := m.service.Refresh(ctx)
		return refreshMsg{result: result, err: err}
	}
}

func (m Model) rateLimitsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		limits, err := m.service.RateLimits(ctx)
		return rateLimitsMsg{limits: limits, err: err}
	}
}

func (m Model) startRefreshCmd() tea.Cmd {
	return tea.Batch(m.refreshCmd(), progressTickCmd())
}

func tickCmd(full bool) tea.Cmd {
	duration := shortRefresh
	if full {
		duration = fullRefresh
	}
	return tea.Tick(duration, func(time.Time) tea.Msg { return tickMsg{full: full} })
}

func progressTickCmd() tea.Cmd {
	return tea.Tick(progressTick, func(time.Time) tea.Msg { return progressTickMsg{} })
}

func (m *Model) rebuildRows() {
	feed := model.Feeds[m.activeFeed]
	items := append([]model.Item(nil), m.feeds[feed]...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Repository() == items[j].Repository() {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Repository() < items[j].Repository()
	})

	var rows []row
	currentRepo := ""
	for _, item := range items {
		repo := item.Repository()
		if repo == "" {
			repo = "(unknown repo)"
		}
		if repo != currentRepo {
			currentRepo = repo
			key := "repo:" + repo
			if _, ok := m.expanded[key]; !ok {
				m.expanded[key] = true
			}
			rows = append(rows, row{kind: rowRepo, key: key, repo: repo})
		}
		if !m.expanded["repo:"+repo] {
			continue
		}
		rows = append(rows, row{kind: rowItem, key: item.Key, repo: repo, item: item})
	}
	m.rows = rows
	m.normalizeSelection()
}

func (m *Model) normalizeSelection() {
	if len(m.rows) == 0 {
		m.selected = 0
		m.rememberSelection()
		return
	}
	if m.selected >= len(m.rows) {
		m.selected = len(m.rows) - 1
	}
	if m.selectable(m.selected) {
		m.rememberSelection()
		return
	}
	for i := m.selected + 1; i < len(m.rows); i++ {
		if m.selectable(i) {
			m.selected = i
			m.rememberSelection()
			return
		}
	}
	for i := m.selected - 1; i >= 0; i-- {
		if m.selectable(i) {
			m.selected = i
			m.rememberSelection()
			return
		}
	}
	m.selected = 0
	m.rememberSelection()
}

func (m *Model) rememberSelection() {
	if m.selectedByFeed == nil {
		m.selectedByFeed = map[model.Feed]int{}
	}
	m.selectedByFeed[model.Feeds[m.activeFeed]] = m.selected
}

func (m *Model) restoreSelection() {
	m.selected = 0
	if m.selectedByFeed == nil {
		return
	}
	m.selected = m.selectedByFeed[model.Feeds[m.activeFeed]]
}

func (m Model) render() string {
	if m.showHelp {
		return m.renderHelp()
	}
	if m.showRateLimits {
		return m.renderRateLimits()
	}
	var b strings.Builder
	b.WriteString(m.renderFeeds())
	b.WriteString("\n")
	b.WriteString(m.renderRows())
	b.WriteString("\n")
	b.WriteString(m.renderSeparator())
	b.WriteString("\n")
	b.WriteString(m.renderStatus())
	b.WriteString("\n")
	b.WriteString(helpStyle().Render(m.renderFooterHelp()))
	return b.String()
}

func (m Model) renderFooterHelp() string {
	parts := []string{
		"tab/shift+tab feed",
		"j/k move",
		"h/l collapse/expand",
	}
	if m.localDoneEnabled() {
		parts = append(parts, "E done")
	}
	parts = append(parts, "y copy", "o/enter open", "r refresh", "? help", "q quit")
	return strings.Join(parts, "  ")
}

func (m Model) renderWithBorder() string {
	if m.width <= 2 || m.height <= 2 {
		return m.render()
	}

	innerWidth := m.width - 2
	innerHeight := m.height - 2
	lines := strings.Split(m.render(), "\n")
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}

	bordered := make([]string, 0, innerHeight+2)
	borderLine := strings.Repeat(" ", m.width)
	bordered = append(bordered, borderLine)
	for _, line := range lines {
		bordered = append(bordered, " "+padRight(line, innerWidth)+" ")
	}
	for len(bordered) < innerHeight+1 {
		bordered = append(bordered, borderLine)
	}
	bordered = append(bordered, borderLine)
	return strings.Join(bordered, "\n")
}

func (m Model) renderFeeds() string {
	parts := make([]string, 0, len(model.Feeds))
	for i, feed := range model.Feeds {
		label := fmt.Sprintf("%s (%d)", feed.Title(), len(m.feeds[feed]))
		if i == m.activeFeed {
			parts = append(parts, selectedStyle().Render(label))
		} else {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderSeparator() string {
	width := m.contentWidth()
	if width <= 0 {
		return ""
	}
	return dimStyle().Render(strings.Repeat("─", width))
}

func (m Model) renderRows() string {
	if len(m.rows) == 0 {
		return dimStyle().Render("No items. Press r to refresh.")
	}
	limit := m.height - 6
	if limit <= 0 {
		limit = len(m.rows)
	}
	start := 0
	if m.selected >= limit {
		start = m.selected - limit + 1
	}
	end := min(len(m.rows), start+limit)
	rowWidth := max(0, m.contentWidth()-2)
	layout := itemRowLayoutForRows(m.rows[start:end], rowWidth)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := m.renderRow(m.rows[i], rowWidth, layout)
		if i == m.selected {
			line = selectedStyle().Render("> " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) contentWidth() int {
	if m.width > 2 {
		return m.width - 2
	}
	return m.width
}

func itemRowLayoutForRows(rows []row, width int) itemRowLayout {
	items := make([]model.Item, 0, len(rows))
	for _, row := range rows {
		if row.kind == rowItem {
			items = append(items, row.item)
		}
	}
	return itemRowLayoutForItems(items, width)
}

func (m Model) renderRow(r row, width int, layout itemRowLayout) string {
	switch r.kind {
	case rowRepo:
		marker := "▾"
		if !m.expanded[r.key] {
			marker = "▸"
		}
		return repoStyle().Render(marker + " " + r.repo)
	default:
		item := r.item
		style := lipgloss.NewStyle()
		if item.Done {
			style = dimStyle()
		}
		return style.Render(itemRowWithLayout(item, width, layout))
	}
}

func (m Model) renderStatus() string {
	account := m.account
	if account == "" {
		account = "unknown"
	}
	status := m.status
	if m.loading {
		progress := m.refreshProgress.String()
		if progress == "" {
			progress = "from GitHub"
		}
		status = fmt.Sprintf("%s refreshing %s (%s)", spinnerFrame(m.spinner), progress, formatDuration(time.Since(m.loadingAt)))
	}
	if m.rateWarning != "" {
		status = status + " | " + m.rateWarning
	}
	return statusStyle().Render(fmt.Sprintf("%s@%s | %s", account, m.host, status))
}

func (m Model) renderHelp() string {
	return strings.Join([]string{
		titleStyle().Render("hyper help"),
		"",
		"j/down        select next visible item",
		"k/up          select previous visible item",
		"l/right       expand selected group",
		"h/left        collapse selected group or move to parent",
		"E             toggle local done in Important Notifications",
		"y             copy selected item URL",
		"o/enter       open selected item URL",
		"r             refresh",
		"shift+r       show rate limits",
		"tab           next feed",
		"shift+tab     previous feed",
		"?             close help",
		"q/Ctrl+C      quit",
	}, "\n")
}

func (m Model) renderRateLimits() string {
	lines := []string{
		titleStyle().Render("GitHub rate limits"),
		"",
		fmt.Sprintf("account: %s", displayAccount(m.account, m.rateLimits.Account)),
		"",
	}
	switch {
	case m.rateLimitsLoading:
		lines = append(lines, "loading rate limits...")
	case m.rateLimitsErr != "":
		lines = append(lines, "failed to load rate limits: "+m.rateLimitsErr)
	default:
		lines = append(lines,
			renderRateLimitLine("REST/core", m.rateLimits.Core),
			renderRateLimitLine("GraphQL", m.rateLimits.GraphQL),
			renderRateLimitLine("Search", m.rateLimits.Search),
		)
	}
	lines = append(lines, "", "shift+r      close", "q/Ctrl+C     quit")
	return strings.Join(lines, "\n")
}

func displayAccount(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func renderRateLimitLine(label string, resource github.RateLimitResource) string {
	reset := "unknown"
	if !resource.ResetAt.IsZero() {
		reset = resource.ResetAt.Local().Format(time.Kitchen)
	}
	return fmt.Sprintf("%-10s %5d/%-5d remaining, %5d used, resets %s", label, resource.Remaining, resource.Limit, resource.Used, reset)
}

func feedsFromCache(data cache.Data) map[model.Feed][]model.Item {
	feeds := map[model.Feed][]model.Item{}
	for _, feed := range model.Feeds {
		for _, key := range data.FeedItemIDs[feed] {
			item, ok := data.Items[key]
			if !ok {
				continue
			}
			if feed != model.FeedImportantNotifications {
				item.Done = false
				item.DoneAt = time.Time{}
			}
			feeds[feed] = append(feeds[feed], item)
		}
	}
	return feeds
}

func spinnerFrame(frame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[frame%len(frames)]
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	if minutes == 0 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func cacheStatus(data cache.Data) string {
	count := 0
	for _, keys := range data.FeedItemIDs {
		count += len(keys)
	}
	if count == 0 {
		return "cache empty"
	}
	return fmt.Sprintf("cache ready (%d items)", count)
}

func typeIcon(item model.Item) string {
	style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(iconColor(item)))

	switch item.Type {
	case model.ItemTypePullRequest:
		return style.Render("⎇")
	case model.ItemTypeIssue:
		if strings.EqualFold(item.State, "closed") && isNotPlanned(item.StateReason) {
			return style.Render("∅")
		}
		return style.Render("◉")
	case model.ItemTypeDiscussion:
		return style.Render("⇄")
	default:
		return "NOT"
	}
}

func iconColor(item model.Item) string {
	if item.Type == model.ItemTypePullRequest && strings.EqualFold(item.State, "closed") && !item.Merged {
		return "9"
	}
	if item.Type == model.ItemTypePullRequest && item.Draft {
		return "8"
	}
	if item.Type == model.ItemTypeIssue && strings.EqualFold(item.State, "closed") && isNotPlanned(item.StateReason) {
		return "8"
	}
	if item.Merged || strings.EqualFold(item.State, "closed") || strings.EqualFold(item.State, "merged") {
		return "5"
	}
	if item.State == "" || strings.EqualFold(item.State, "open") {
		return "10"
	}
	return "8"
}

func isNotPlanned(reason string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(reason, "_", " "))
	return normalized == "not planned"
}

func age(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func selectedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
}

func repoStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func dimStyle() lipgloss.Style {
	return lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("8"))
}

func helpStyle() lipgloss.Style {
	return lipgloss.NewStyle().Faint(true)
}

func statusStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
}

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
}
