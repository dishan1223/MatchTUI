package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"matchtui/internal/config"
	"matchtui/internal/model"
	"matchtui/internal/provider"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Tab identifiers, also used as the 1-4 shortcuts.
const (
	tabLive = iota
	tabUpcoming
	tabResults
	tabStarred
	tabCount
)

// Menu identifiers for the overlay views.
const (
	menuNone = iota
	menuProvider
	menuLeagues
	menuHelp
	menuKey
)

// windowTTL is how long a fetched window is treated as fresh before the app
// reloads it when the user switches tabs.
const windowTTL = 5 * time.Minute

// detailTimeout bounds the on-demand team sheet request so a slow feed cannot
// leave the match page hanging.
const detailTimeout = 25 * time.Second

// matchesMsg carries the result of one provider fetch.
type matchesMsg struct {
	seq        int
	providerID string
	days       []time.Time
	matches    []model.Match
	err        error
	at         time.Time
}

// lineupsMsg carries the team sheets for one opened match.
type lineupsMsg struct {
	key        string
	providerID string
	lineups    []model.Lineup
	err        error
}

// leaguesMsg carries a provider's competition catalogue.
type leaguesMsg struct {
	providerID string
	leagues    []provider.League
	err        error
}

// tickMsg drives the one second clock used for refresh countdowns and live
// match minutes.
type tickMsg time.Time

// App is the root Bubble Tea model.
type App struct {
	cfg       *config.Config
	cfgPath   string
	providers []provider.Provider
	active    provider.Provider
	theme     theme

	// tabs and list state
	tab     int
	matches []model.Match
	list    []model.Match
	stars   map[string]bool
	cursor  int
	shift   int

	// detail page
	detail        bool
	detailScroll  int
	detailKey     string // match key whose team sheets are being or have been loaded
	detailLoading bool
	lineups       map[string][]model.Lineup // team sheets by match key
	detailErr     error
	detailTotal   int // content lines of the open match page, measured while rendering
	detailFrom    int // first content line on screen, 1 based, for the footer
	detailTo      int // last content line on screen, inclusive

	// pendingG tracks a half-typed "gg" so the vim chord can jump to the top.
	pendingG bool

	// filter input
	filter     string
	filterMode bool
	ti         textinput.Model

	// API key prompt
	keyInput textinput.Model
	keyFor   string

	// overlays
	menu       int
	menuCursor int
	menuOffset int
	leagues    []provider.League
	pending    map[string]bool

	// fetching
	spin      spinner.Model
	loading   bool
	loadSeq   int
	fetched   map[string]time.Time
	lastFetch time.Time
	nextFetch time.Time

	// status
	err     error
	status  string
	statusA time.Time

	width, height int
	now           time.Time
	quitting      bool
}

// New builds the application from the persisted configuration.
func New(cfg *config.Config, cfgPath string) *App {
	providers := provider.Builtin(cfg)
	active, ok := provider.ByID(providers, cfg.Provider)
	if !ok {
		active = providers[0]
	}

	a := &App{
		cfg:       cfg,
		cfgPath:   cfgPath,
		providers: providers,
		active:    active,
		theme:     newTheme(cfg.ColorScheme),
		stars:     make(map[string]bool, len(cfg.Starred)),
		fetched:   map[string]time.Time{},
		lineups:   map[string][]model.Lineup{},
	}
	for _, k := range cfg.Starred {
		a.stars[k] = true
	}

	a.ti = textinput.New()
	a.ti.Prompt = " filter "
	a.ti.PromptStyle = a.theme.footerKey
	a.ti.CharLimit = 64
	a.ti.Width = 32

	a.keyInput = textinput.New()
	a.keyInput.Prompt = " key "
	a.keyInput.PromptStyle = a.theme.footerKey
	a.keyInput.EchoMode = textinput.EchoPassword
	a.keyInput.EchoCharacter = '•'
	a.keyInput.CharLimit = 128
	a.keyInput.Width = 52

	a.spin = spinner.New(spinner.WithSpinner(spinner.Dot))
	a.spin.Style = a.theme.faintText

	// The first fetch runs from Init, before any tick has set the clock.
	a.now = time.Now()
	a.nextFetch = a.now.Add(a.refreshInterval())

	return a
}

// Init starts the clock and the first fetch.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.tickCmd(), a.spin.Tick, a.fetchLiveCmd())
}

// tickCmd schedules the next one second tick.
func (a *App) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---- update ---------------------------------------------------------------

// Update is the Bubble Tea message handler.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil

	case tickMsg:
		a.now = time.Now()
		return a, tea.Batch(a.tickCmd(), a.autoRefreshCmd())

	case spinner.TickMsg:
		var cmd tea.Cmd
		a.spin, cmd = a.spin.Update(msg)
		return a, cmd

	case matchesMsg:
		return a, a.applyMatches(msg)

	case lineupsMsg:
		a.applyLineups(msg)
		return a, nil

	case leaguesMsg:
		a.applyLeagues(msg)
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)
	}
	return a, nil
}

// handleKey dispatches by the active mode, then by the global shortcuts.
func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.filterMode {
		return a.handleFilterKey(msg)
	}
	switch a.menu {
	case menuProvider, menuLeagues, menuHelp:
		return a.handleMenuKey(msg)
	case menuKey:
		return a.handleKeyPrompt(msg)
	}

	key := msg.String()
	switch key {
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit

	case "q":
		if a.detail {
			a.closeDetail()
			return a, nil
		}
		a.quitting = true
		return a, tea.Quit

	// The match page is a scrollable document: it owns the movement keys while
	// it is open, so the list underneath cannot be nudged by accident.
	case "enter", "i", " ", "esc":
		if a.detail {
			a.closeDetail()
			return a, nil
		}
		if key != "esc" {
			return a, a.openDetail()
		}
		if a.filter != "" {
			a.filter = ""
			a.ti.SetValue("")
			a.rebuild()
		}

	case "up", "k":
		if a.detail {
			a.scrollDetail(-1)
			return a, nil
		}
		a.moveCursor(-1)
	case "down", "j":
		if a.detail {
			a.scrollDetail(1)
			return a, nil
		}
		a.moveCursor(1)
	case "pgup", "ctrl+b":
		if a.detail {
			a.scrollDetail(-a.detailPageSize())
			return a, nil
		}
		a.moveCursor(-a.pageSize())
	case "pgdown", "ctrl+f":
		if a.detail {
			a.scrollDetail(a.detailPageSize())
			return a, nil
		}
		a.moveCursor(a.pageSize())
	case "ctrl+u", "ctrl+d":
		// Half page, in whichever window owns the keys right now.
		half := a.pageSize() / 2
		if a.detail {
			half = a.detailPageSize() / 2
		}
		if key == "ctrl+u" {
			half = -half
		}
		if a.detail {
			a.scrollDetail(half)
			return a, nil
		}
		a.moveCursor(half)
	case "g":
		// Vim's gg: a lone g does nothing but arm the chord.
		if a.pendingG {
			a.pendingG = false
			if a.detail {
				a.detailScroll = 0
			} else {
				a.cursor = 0
			}
			return a, nil
		}
		a.pendingG = true
		return a, nil
	case "G":
		a.pendingG = false
		if a.detail {
			// Overshoot on purpose: the renderer clamps to the real end.
			a.detailScroll = a.detailTotal
			return a, nil
		}
		a.cursor = maxInt(0, len(a.list)-1)
	case "home":
		a.cursor = 0
	case "end":
		a.cursor = maxInt(0, len(a.list)-1)

	case "left", "h", "shift+tab":
		a.setTab(a.tab - 1)
		return a, a.ensureWindow()
	case "right", "l", "tab":
		a.setTab(a.tab + 1)
		return a, a.ensureWindow()
	case "1":
		a.setTab(tabLive)
		return a, a.ensureWindow()
	case "2":
		a.setTab(tabUpcoming)
		return a, a.ensureWindow()
	case "3":
		a.setTab(tabResults)
		return a, a.ensureWindow()
	case "4":
		a.setTab(tabStarred)
		return a, a.ensureWindow()

	case "[", "shift+left":
		a.shiftWindow(-1)
		return a, a.ensureWindow()
	case "]", "shift+right":
		a.shiftWindow(1)
		return a, a.ensureWindow()

	case "/":
		a.filterMode = true
		a.ti.SetValue(a.filter)
		a.ti.Focus()
		return a, textinput.Blink

	case "r":
		return a, a.fetchCurrentCmd(true)

	case "s":
		a.toggleStar()

	case "p":
		a.openMenu(menuProvider)
		return a, a.loadLeaguesCmd()
	case "L":
		a.openMenu(menuLeagues)
		return a, a.loadLeaguesCmd()
	case "?":
		a.openMenu(menuHelp)

	case "K":
		// Shift+K sets the API key for the active provider.
		if a.active.NeedsKey() {
			a.openKeyPrompt(a.active.ID())
		} else {
			a.flash("%s does not need an API key", a.active.Name())
		}
	}
	a.pendingG = false
	return a, nil
}

// openDetail switches to the full-screen match page and asks the provider for
// the team sheets the moment they are not already cached.
func (a *App) openDetail() tea.Cmd {
	if a.cursor < 0 || a.cursor >= len(a.list) {
		return nil
	}
	a.detail = true
	a.detailScroll = 0
	a.pendingG = false
	a.detailKey = a.list[a.cursor].Key() // pin the page to this fixture
	return a.fetchLineupsCmd(a.list[a.cursor])
}

// closeDetail returns to the list.
func (a *App) closeDetail() {
	a.detail = false
	a.detailScroll = 0
	a.pendingG = false
}

// detailPageSize is how many lines one page of the match panel scrolls.
func (a *App) detailPageSize() int { return maxInt(3, a.height-12) }

// detailPanelWidth is the width of the match panel. On a roomy terminal it
// takes about two thirds of the screen so the list stays readable beside it;
// on a phone sized terminal (Termux in portrait) it takes the whole width.
func (a *App) detailPanelWidth() int {
	if a.width < 88 {
		return a.width
	}
	return clamp(a.width*2/3, 44, a.width-32)
}

// compact reports whether the terminal is too narrow for side by side columns,
// which is the Termux portrait case.
func (a *App) compact() bool { return a.width < 76 }

// cramped reports whether the terminal is too short for a two line footer.
func (a *App) cramped() bool { return a.height < 16 }

// atWidth runs a render pass at a different width, so the list column beside
// the match panel can be drawn with its own layout maths.
func (a *App) atWidth(w int, fn func() string) string {
	saved := a.width
	a.width = w
	out := fn()
	a.width = saved
	return out
}

// scrollDetail moves the match page viewport; the renderer clamps it against
// the real content height on the next frame.
func (a *App) scrollDetail(delta int) {
	a.pendingG = false
	a.detailScroll = maxInt(0, a.detailScroll+delta)
}

// fetchLineupsCmd loads team sheets for one fixture on demand, so opening a
// match costs at most one extra request and only for the match actually read.
func (a *App) fetchLineupsCmd(m model.Match) tea.Cmd {
	d, ok := provider.AsDetailer(a.active)
	if !ok {
		return nil
	}
	key := m.Key()
	if _, have := a.lineups[key]; have {
		return nil // already loaded
	}
	if a.detailKey == key && a.detailLoading {
		return nil // already in flight
	}
	a.detailKey = key
	a.detailLoading = true
	a.detailErr = nil

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), detailTimeout)
		defer cancel()
		enriched, err := d.FetchDetail(ctx, m)
		return lineupsMsg{
			key:        key,
			providerID: a.active.ID(),
			lineups:    enriched.Lineups,
			err:        err,
		}
	}
}

// applyLineups stores the team sheets that arrived for an opened match.
func (a *App) applyLineups(msg lineupsMsg) {
	if msg.providerID != a.active.ID() || msg.key != a.detailKey {
		return // stale: the user moved on or switched feed
	}
	a.detailLoading = false
	if msg.err != nil {
		a.detailErr = msg.err
		return
	}
	if len(msg.lineups) > 0 {
		a.lineups[msg.key] = msg.lineups
	} else {
		// Remember that this fixture has none, so a retry does not loop.
		a.lineups[msg.key] = []model.Lineup{}
	}
}

// handleFilterKey drives the filter input.
func (a *App) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		if msg.String() == "ctrl+c" {
			a.quitting = true
			a.filterMode = false
			a.ti.Blur()
			return a, tea.Quit
		}
		a.filterMode = false
		a.ti.Blur()
		a.filter = ""
		a.ti.SetValue("")
		a.rebuild()
		return a, nil
	case "enter":
		a.filterMode = false
		a.filter = strings.TrimSpace(a.ti.Value())
		a.ti.Blur()
		a.rebuild()
		return a, nil
	}
	var cmd tea.Cmd
	a.ti, cmd = a.ti.Update(msg)
	next := strings.TrimSpace(a.ti.Value())
	if next != a.filter {
		a.filter = next
		a.rebuild()
	}
	return a, cmd
}

// handleMenuKey drives the provider, league and help overlays.
func (a *App) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if a.menu == menuHelp {
		switch key {
		case "esc", "enter", "?", "q", "ctrl+c":
			if key == "ctrl+c" {
				a.quitting = true
				return a, tea.Quit
			}
			a.menu = menuNone
		}
		return a, nil
	}

	switch key {
	case "esc":
		a.menu = menuNone
		a.pending = nil
		return a, nil
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	case "up", "k":
		a.moveMenuCursor(-1)
		return a, nil
	case "down", "j":
		a.moveMenuCursor(1)
		return a, nil
	case "pgup":
		a.moveMenuCursor(-a.menuPageSize())
		return a, nil
	case "pgdown":
		a.moveMenuCursor(a.menuPageSize())
		return a, nil
	}

	if a.menu == menuProvider {
		switch key {
		case "enter", " ":
			if a.menuCursor >= 0 && a.menuCursor < len(a.providers) {
				p := a.providers[a.menuCursor]
				a.menu = menuNone
				return a, a.switchProvider(p)
			}
		case "k":
			// Set the key for the highlighted feed before switching to it.
			if a.menuCursor >= 0 && a.menuCursor < len(a.providers) {
				p := a.providers[a.menuCursor]
				if p.NeedsKey() {
					a.openKeyPrompt(p.ID())
				}
			}
		}
		return a, nil
	}

	// League menu.
	switch key {
	case "enter":
		a.menu = menuNone
		return a, a.commitLeagues()
	case " ", "space":
		if a.menuCursor >= 0 && a.menuCursor < len(a.leagues) {
			id := a.leagues[a.menuCursor].ID
			a.pending[id] = !a.pending[id]
		}
	case "a":
		for _, l := range a.leagues {
			a.pending[l.ID] = true
		}
	case "n":
		for _, l := range a.leagues {
			a.pending[l.ID] = false
		}
	}
	return a, nil
}

// handleKeyPrompt drives the API key entry overlay.
func (a *App) handleKeyPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.menu = menuNone
		a.keyFor = ""
		return a, nil
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	case "enter":
		key := strings.TrimSpace(a.keyInput.Value())
		id := a.keyFor
		a.menu = menuNone
		a.keyFor = ""
		if key == "" {
			return a, nil
		}
		a.cfg.SetKey(id, key)
		_ = a.cfg.Save()
		a.flash("API key saved")
		return a, tea.Batch(a.loadLeaguesCmd(), a.fetchCurrentCmd(true))
	}
	var cmd tea.Cmd
	a.keyInput, cmd = a.keyInput.Update(msg)
	return a, cmd
}

// ---- cursor and tab helpers ----------------------------------------------

func (a *App) moveCursor(delta int) {
	if len(a.list) == 0 {
		a.cursor = 0
		return
	}
	a.cursor = clamp(a.cursor+delta, 0, len(a.list)-1)
}

func (a *App) moveMenuCursor(delta int) {
	n := len(a.leagues)
	if a.menu == menuProvider {
		n = len(a.providers)
	}
	if n == 0 {
		a.menuCursor = 0
		return
	}
	a.menuCursor = clamp(a.menuCursor+delta, 0, n-1)
	a.ensureMenuVisible(n)
}

// ensureMenuVisible scrolls the overlay list so the cursor stays on screen.
func (a *App) ensureMenuVisible(total int) {
	page := a.menuPageSize()
	if a.menuCursor < a.menuOffset {
		a.menuOffset = a.menuCursor
	}
	if a.menuCursor >= a.menuOffset+page {
		a.menuOffset = a.menuCursor - page + 1
	}
	a.menuOffset = clamp(a.menuOffset, 0, maxInt(0, total-page))
}

func (a *App) menuPageSize() int {
	rows := a.height - 10
	if a.menu == menuLeagues {
		rows = a.height - 9
	}
	return maxInt(3, rows)
}

func (a *App) pageSize() int { return maxInt(1, a.height-6) }

func (a *App) setTab(tab int) {
	if tab < 0 {
		tab = tabCount - 1
	}
	if tab >= tabCount {
		tab = 0
	}
	if tab == a.tab {
		return
	}
	a.tab = tab
	a.cursor = 0
	a.detailScroll = 0
	a.shift = 0
	a.rebuildKeep("") // fresh tab: don't carry the old cursor position over
}

// shiftWindow moves the upcoming/results window by one chunk of days.
func (a *App) shiftWindow(delta int) {
	if a.tab != tabUpcoming && a.tab != tabResults {
		return
	}
	a.shift += delta
	a.cursor = 0
}

func (a *App) openMenu(kind int) {
	a.menu = kind
	a.menuCursor = 0
	a.menuOffset = 0
	a.pending = nil
	if kind == menuLeagues {
		a.pending = map[string]bool{}
		selected := a.cfg.SelectedLeagues(a.active.ID())
		if len(selected) == 0 {
			for _, l := range a.leagues {
				if l.Default {
					a.pending[l.ID] = true
				}
			}
		}
		for _, id := range selected {
			a.pending[id] = true
		}
	}
}

func (a *App) openKeyPrompt(providerID string) {
	a.menu = menuKey
	a.keyFor = providerID
	a.keyInput.SetValue(a.cfg.GetKey(providerID))
	a.keyInput.Focus()
}

// flash shows a transient status message for a few seconds.
func (a *App) flash(format string, args ...any) {
	a.status = fmt.Sprintf(format, args...)
	a.statusA = time.Now()
}

// toggleStar adds or removes the followed match from the watchlist. While the
// match page is open it acts on the fixture being read, which is pinned by key
// and may differ from the row under the cursor.
func (a *App) toggleStar() {
	var m model.Match
	switch {
	case a.detail:
		var ok bool
		if m, ok = a.openMatch(); !ok {
			return
		}
	case a.cursor >= 0 && a.cursor < len(a.list):
		m = a.list[a.cursor]
	default:
		return
	}

	key := m.Key()
	if a.cfg.ToggleStar(key) {
		a.stars[key] = true
		a.flash("Starred %s v %s", m.Home.Name, m.Away.Name)
	} else {
		delete(a.stars, key)
		a.flash("Unstarred %s v %s", m.Home.Name, m.Away.Name)
	}
	_ = a.cfg.Save()
	if a.tab == tabStarred {
		a.rebuild()
	}
}

// ---- fetching -------------------------------------------------------------

// leagueIDs returns the competitions configured for the active provider.
func (a *App) leagueIDs() []string { return a.cfg.SelectedLeagues(a.active.ID()) }

// refreshInterval is the auto refresh cadence, never faster than the provider
// can sustain so free quotas stay intact.
func (a *App) refreshInterval() time.Duration {
	d := time.Duration(a.cfg.RefreshSeconds) * time.Second
	if hint := a.active.RateHint(); hint > d {
		d = hint
	}
	return d
}

// requestFor builds the feed request backing a tab.
func (a *App) requestFor(tab int) (provider.Request, string) {
	today := startOfDay(a.now)
	ids := a.leagueIDs()
	switch tab {
	case tabUpcoming:
		from := today.AddDate(0, 0, a.shift)
		to := from.AddDate(0, 0, a.cfg.UpcomingDays-1)
		return provider.Request{Leagues: ids, From: from, To: to, Window: "upcoming"},
			"up:" + strconv.Itoa(a.shift)
	case tabResults:
		to := today.AddDate(0, 0, a.shift)
		from := to.AddDate(0, 0, -(a.cfg.ResultsDays - 1))
		return provider.Request{Leagues: ids, From: from, To: to, Window: "results"},
			"res:" + strconv.Itoa(a.shift)
	default:
		return provider.Request{Leagues: ids, From: today, To: today, Window: "live"}, "live"
	}
}

// fetchLiveCmd refreshes today's matches, which keeps live scores current on
// every tab without spending the requests a multi-day window would cost.
func (a *App) fetchLiveCmd() tea.Cmd {
	if a.loading {
		return nil
	}
	if a.active.NeedsKey() && !a.active.KeyPresent() {
		return nil
	}
	req, _ := a.requestFor(tabLive)
	return a.fetchCmd(req, "live")
}

// fetchCurrentCmd reloads the window behind the selected tab.
func (a *App) fetchCurrentCmd(force bool) tea.Cmd {
	if a.tab == tabLive || a.tab == tabStarred {
		if !force {
			return nil
		}
		a.fetched = map[string]time.Time{}
		a.loading = false
		return a.fetchLiveCmd()
	}
	if !force {
		if cmd := a.ensureWindow(); cmd != nil {
			return cmd
		}
	}
	a.fetched = map[string]time.Time{}
	a.loading = false
	return a.ensureWindow()
}

// ensureWindow fetches an upcoming or results window when it is missing or
// stale, and returns nil when the data is already current.
func (a *App) ensureWindow() tea.Cmd {
	if a.tab != tabUpcoming && a.tab != tabResults {
		return nil
	}
	if a.loading {
		return nil
	}
	if a.active.NeedsKey() && !a.active.KeyPresent() {
		return nil
	}
	req, key := a.requestFor(a.tab)
	if at, ok := a.fetched[key]; ok && time.Since(at) < windowTTL {
		return nil
	}
	return a.fetchCmd(req, key)
}

// fetchCmd runs a provider request off the UI thread.
func (a *App) fetchCmd(req provider.Request, windowKey string) tea.Cmd {
	a.loadSeq++
	seq := a.loadSeq
	p := a.active
	prefix := p.ID() + "|" + windowKey
	days := req.Days()

	a.loading = true
	a.fetched[prefix] = time.Now()

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		ms, err := p.Fetch(ctx, req)
		return matchesMsg{
			seq:        seq,
			providerID: p.ID(),
			days:       days,
			matches:    ms,
			err:        err,
			at:         time.Now(),
		}
	}
}

// autoRefreshCmd triggers the periodic live refresh.
func (a *App) autoRefreshCmd() tea.Cmd {
	if a.loading {
		return nil
	}
	if a.now.Before(a.nextFetch) {
		return nil
	}
	if a.active.NeedsKey() && !a.active.KeyPresent() {
		return nil
	}
	return a.fetchLiveCmd()
}

// applyMatches folds a fetch result into the loaded set.
func (a *App) applyMatches(msg matchesMsg) tea.Cmd {
	if msg.seq != a.loadSeq || msg.providerID != a.active.ID() {
		return nil // a stale response from a previous provider or request
	}
	a.loading = false
	if msg.err != nil {
		a.err = msg.err
	} else {
		a.err = nil
		a.lastFetch = msg.at
		a.nextFetch = msg.at.Add(a.refreshInterval())
	}

	// Replace everything the request covered, keeping other windows intact.
	days := make(map[string]bool, len(msg.days))
	for _, d := range msg.days {
		days[d.Format("2006-01-02")] = true
	}
	kept := make([]model.Match, 0, len(a.matches)+len(msg.matches))
	for _, m := range a.matches {
		if m.Provider == msg.providerID && days[dayKey(m)] {
			continue
		}
		kept = append(kept, m)
	}
	kept = append(kept, msg.matches...)
	model.ByKickoff(kept)
	a.matches = kept
	a.rebuild()

	if msg.err == nil {
		return nil
	}
	return nil
}

// dayKey formats a match's local day for window bookkeeping.
func dayKey(m model.Match) string {
	if m.Kickoff.IsZero() {
		return ""
	}
	return m.Kickoff.Format("2006-01-02")
}

// loadLeaguesCmd fetches the active provider's competition catalogue.
func (a *App) loadLeaguesCmd() tea.Cmd {
	p := a.active
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ls, err := p.Leagues(ctx)
		return leaguesMsg{providerID: p.ID(), leagues: ls, err: err}
	}
}

// applyLeagues caches a catalogue that arrives from the provider.
func (a *App) applyLeagues(msg leaguesMsg) {
	if msg.providerID != a.active.ID() {
		return
	}
	a.leagues = msg.leagues
	if msg.err != nil {
		a.err = msg.err
	}
}

// switchProvider changes the active feed and reloads everything behind it.
func (a *App) switchProvider(p provider.Provider) tea.Cmd {
	if p.ID() == a.active.ID() {
		return nil
	}
	a.active = p
	a.cfg.Provider = p.ID()
	_ = a.cfg.Save()

	// Drop the previous feed's fixtures and any window bookkeeping.
	a.matches = nil
	a.list = nil
	a.cursor = 0
	a.shift = 0
	a.leagues = nil
	a.fetched = map[string]time.Time{}
	a.lineups = map[string][]model.Lineup{}
	a.detailKey = ""
	a.detailLoading = false
	a.detailErr = nil
	a.err = nil
	a.lastFetch = time.Time{}
	a.rebuild()

	if p.NeedsKey() && !p.KeyPresent() {
		a.openKeyPrompt(p.ID())
		a.flash("%s needs a free API key", p.Name())
		return a.loadLeaguesCmd()
	}
	a.flash("Switched to %s", p.Name())
	return tea.Batch(a.loadLeaguesCmd(), a.fetchLiveCmd())
}

// commitLeagues writes the league selection and reloads behind it.
func (a *App) commitLeagues() tea.Cmd {
	if a.pending == nil {
		return nil
	}
	var ids []string
	for _, l := range a.leagues {
		if a.pending[l.ID] {
			ids = append(ids, l.ID)
		}
	}
	a.cfg.SetSelectedLeagues(a.active.ID(), ids)
	_ = a.cfg.Save()

	a.matches = nil
	a.list = nil
	a.cursor = 0
	a.fetched = map[string]time.Time{}
	a.rebuild()
	a.flash("Loaded %d competition(s)", len(ids))
	return a.fetchLiveCmd()
}

// ---- list construction ----------------------------------------------------

// rebuild recomputes the visible list for the current tab and filter, keeping
// the cursor on the same fixture when possible.
// rebuild refilters the list, trying to keep the cursor on the same match.
func (a *App) rebuild() {
	var keep string
	if a.cursor >= 0 && a.cursor < len(a.list) {
		keep = a.list[a.cursor].Key()
	}
	a.rebuildKeep(keep)
}

// rebuildKeep refilters the list; keep, when non-empty, pins the cursor to the
// match with that key instead of the current index.
func (a *App) rebuildKeep(keep string) {
	q := strings.ToLower(strings.TrimSpace(a.filter))
	out := make([]model.Match, 0, len(a.matches))
	for _, m := range a.matches {
		if !a.tabOK(m) {
			continue
		}
		if q != "" && !matchesFilter(m, q) {
			continue
		}
		out = append(out, m)
	}

	switch a.tab {
	case tabResults:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Kickoff.After(out[j].Kickoff)
		})
	case tabLive:
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].League != out[j].League {
				return out[i].League < out[j].League
			}
			return out[i].Kickoff.Before(out[j].Kickoff)
		})
	default:
		model.ByKickoff(out)
	}

	a.list = out
	if keep != "" {
		for i, m := range out {
			if m.Key() == keep {
				a.cursor = i
				return
			}
		}
	}
	a.cursor = clamp(a.cursor, 0, maxInt(0, len(out)-1))
}

// tabOK reports whether a match belongs on the current tab.
func (a *App) tabOK(m model.Match) bool {
	switch a.tab {
	case tabLive:
		return m.State.Live()
	case tabUpcoming:
		return m.State.Upcoming()
	case tabResults:
		return m.State.Finished()
	case tabStarred:
		return a.stars[m.Key()]
	}
	return true
}

// matchesFilter applies the free text filter.
func matchesFilter(m model.Match, q string) bool {
	hay := strings.ToLower(strings.Join([]string{
		m.Home.Name, m.Home.Abbr, m.Away.Name, m.Away.Abbr,
		m.League, m.LeagueTag, m.Venue, m.City,
	}, " "))
	return strings.Contains(hay, q)
}

// counts returns the number of matches on each tab for the badges.
func (a *App) counts() [tabCount]int {
	var out [tabCount]int
	for _, m := range a.matches {
		switch {
		case m.State.Live():
			out[tabLive]++
		case m.State.Upcoming():
			out[tabUpcoming]++
		case m.State.Finished():
			out[tabResults]++
		}
		if a.stars[m.Key()] {
			out[tabStarred]++
		}
	}
	return out
}

// helpers ------------------------------------------------------------------

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
