package ui

import (
	"strings"
	"testing"
	"time"

	"matchtui/internal/config"
	"matchtui/internal/provider"

	tea "github.com/charmbracelet/bubbletea"
)

// key builds a key message the way Bubble Tea would deliver it.
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// send applies a sequence of keys to the model.
func send(a *App, keys ...string) {
	for _, k := range keys {
		model, _ := a.Update(key(k))
		next, ok := model.(*App)
		if !ok {
			panic("unexpected model type")
		}
		*a = *next
	}
}

// isolateConfig points the config directory at a temp folder so tests never
// read or write the user's real settings.
func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)         // Windows
	t.Setenv("LocalAppData", dir)    // Windows, some fallbacks
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux
	t.Setenv("HOME", dir)            // macOS
}

// newInteractApp builds a demo-backed app with fixtures loaded.
func newInteractApp(t *testing.T) *App {
	t.Helper()
	isolateConfig(t)
	app := newTestApp(t, 120, 40, tabResults)
	if len(app.list) == 0 {
		t.Skip("no demo fixtures available")
	}
	return app
}

func TestTabSwitching(t *testing.T) {
	app := newInteractApp(t)

	if app.tab != tabResults {
		t.Fatalf("starting tab = %d, want results", app.tab)
	}
	send(app, "1")
	if app.tab != tabLive {
		t.Errorf("key 1 selected tab %d, want live", app.tab)
	}
	send(app, "right")
	if app.tab != tabUpcoming {
		t.Errorf("right selected tab %d, want upcoming", app.tab)
	}
	send(app, "tab")
	if app.tab != tabResults {
		t.Errorf("tab selected tab %d, want results", app.tab)
	}
	// Two more presses carry results -> starred -> live.
	send(app, "tab", "tab")
	if app.tab != tabLive {
		t.Errorf("tab should wrap around to live, got %d", app.tab)
	}
	send(app, "shift+tab")
	if app.tab != tabStarred {
		t.Errorf("shift+tab selected tab %d, want starred", app.tab)
	}

	// The upcoming and results tabs must actually contain fixtures.
	send(app, "2")
	if len(app.list) == 0 {
		t.Error("upcoming tab has no fixtures")
	}
	for _, m := range app.list {
		if !m.State.Upcoming() {
			t.Errorf("upcoming tab shows %s in state %v", m.ID, m.State)
			break
		}
	}
}

func TestCursorMovementAndSelection(t *testing.T) {
	app := newInteractApp(t)
	if len(app.list) < 2 {
		t.Skip("need several fixtures")
	}

	if app.cursor != 0 {
		t.Fatalf("cursor starts at %d", app.cursor)
	}
	send(app, "down", "down")
	if app.cursor != 2 {
		t.Errorf("cursor = %d after two downs, want 2", app.cursor)
	}
	send(app, "up")
	if app.cursor != 1 {
		t.Errorf("cursor = %d after an up, want 1", app.cursor)
	}

	// The cursor must clamp at both ends.
	send(app, "up", "up", "up")
	if app.cursor != 0 {
		t.Errorf("cursor = %d at the top, want 0", app.cursor)
	}
	send(app, "end")
	if app.cursor != len(app.list)-1 {
		t.Errorf("cursor = %d after end, want %d", app.cursor, len(app.list)-1)
	}
	send(app, "pgdown")
	if app.cursor != len(app.list)-1 {
		t.Errorf("cursor = %d after pgdown past the end, want %d", app.cursor, len(app.list)-1)
	}

	// Selection drives the detail pane.
	send(app, "enter")
	if !app.detail {
		t.Error("enter did not open the detail")
	}
	send(app, "esc")
	if app.detail {
		t.Error("esc did not close the detail")
	}
}

func TestFiltering(t *testing.T) {
	app := newInteractApp(t)

	send(app, "/")
	if !app.filterMode {
		t.Fatal("slash did not start the filter")
	}
	send(app, "r", "o", "m", "a")
	if app.filter != "roma" {
		t.Errorf("filter = %q, want roma", app.filter)
	}
	for _, m := range app.list {
		hay := strings.ToLower(m.Home.Name + " " + m.Away.Name + " " + m.League)
		if !strings.Contains(hay, "roma") {
			t.Errorf("filter leaked %s (%s v %s)", m.ID, m.Home.Name, m.Away.Name)
			break
		}
	}
	send(app, "enter")
	if app.filterMode {
		t.Error("enter did not leave filter mode")
	}

	send(app, "esc")
	if app.filter != "" {
		t.Errorf("esc left filter %q", app.filter)
	}
	if len(app.list) == 0 {
		t.Error("clearing the filter emptied the list")
	}
}

func TestStarringPersists(t *testing.T) {
	isolateConfig(t)
	cfg := config.Default()
	cfg.Provider = "demo"
	app := New(cfg, "")
	app.width, app.height = 120, 40
	app.tab = tabResults

	p, _ := provider.ByID(app.providers, "demo")
	req, _ := app.requestFor(tabResults)
	matches, err := p.Fetch(t.Context(), req)
	if err != nil {
		t.Fatalf("demo fetch: %v", err)
	}
	app.Update(matchesMsg{seq: app.loadSeq, providerID: p.ID(), days: req.Days(), matches: matches, at: time.Now()})
	app.tab = tabResults
	app.rebuild()
	if len(app.list) == 0 {
		t.Skip("no fixtures")
	}

	target := app.list[0]
	send(app, "s")
	if !cfg.IsStarred(target.Key()) {
		t.Fatal("s did not star the match")
	}
	if len(cfg.Starred) != 1 {
		t.Fatalf("starred list = %v", cfg.Starred)
	}

	// A starred match must appear on the starred tab.
	send(app, "4")
	if app.tab != tabStarred {
		t.Fatalf("tab = %d after key 4", app.tab)
	}
	found := false
	for _, m := range app.list {
		if m.Key() == target.Key() {
			found = true
		}
	}
	if !found {
		t.Error("starred match is missing from the starred tab")
	}

	// Starring again removes it.
	send(app, "3", "s")
	if cfg.IsStarred(target.Key()) {
		t.Error("second s did not unstar the match")
	}
}

func TestProviderMenu(t *testing.T) {
	isolateConfig(t)
	cfg := config.Default()
	cfg.Provider = "demo"
	app := New(cfg, "")
	app.width, app.height = 120, 40

	send(app, "p")
	if app.menu != menuProvider {
		t.Fatalf("menu = %d after p", app.menu)
	}
	send(app, "down")
	if app.menuCursor != 1 {
		t.Errorf("menu cursor = %d, want 1", app.menuCursor)
	}
	send(app, "esc")
	if app.menu != menuNone {
		t.Error("esc did not close the provider menu")
	}

	// Switching to a key-free provider changes the active feed immediately.
	send(app, "p", "down", "down", "down", "enter")
	if app.active.ID() != "demo" {
		t.Errorf("active provider = %s, want demo", app.active.ID())
	}

	// Switching to the key-backed provider asks for a key.
	send(app, "p", "down", "down", "enter")
	if app.active.ID() != "apifootball" {
		t.Fatalf("active provider = %s, want apifootball", app.active.ID())
	}
	if app.menu != menuKey {
		t.Errorf("menu = %d, expected the API key prompt", app.menu)
	}
	send(app, "esc")
	if app.menu != menuNone {
		t.Error("esc did not cancel the key prompt")
	}
}

func TestLeagueMenuAppliesSelection(t *testing.T) {
	isolateConfig(t)
	cfg := config.Default()
	cfg.Provider = "demo"
	app := New(cfg, "")
	app.width, app.height = 120, 40
	app.leagues, _ = app.active.Leagues(t.Context())
	total := len(app.leagues)
	if total < 2 {
		t.Skip("need several competitions")
	}

	send(app, "L")
	if app.menu != menuLeagues {
		t.Fatalf("menu = %d after L", app.menu)
	}

	// Every competition starts selected; toggle the first one off, then apply.
	send(app, " ", "enter")
	if app.menu != menuNone {
		t.Error("enter did not close the league menu")
	}
	after := app.cfg.SelectedLeagues("demo")
	if len(after) != total-1 {
		t.Errorf("selected %d competitions, want %d", len(after), total-1)
	}
}

func TestHelpOverlay(t *testing.T) {
	app := newInteractApp(t)
	send(app, "?")
	if app.menu != menuHelp {
		t.Fatalf("menu = %d after ?", app.menu)
	}
	send(app, "esc")
	if app.menu != menuNone {
		t.Error("esc did not close help")
	}
	send(app, "?", "?")
	if app.menu != menuNone {
		t.Error("? did not toggle help closed")
	}
}

func TestWindowShift(t *testing.T) {
	app := newInteractApp(t)
	send(app, "2")
	if app.shift != 0 {
		t.Fatalf("shift starts at %d", app.shift)
	}
	send(app, "]")
	if app.shift != 1 {
		t.Errorf("shift = %d after ], want 1", app.shift)
	}
	send(app, "[", "[")
	if app.shift != -1 {
		t.Errorf("shift = %d after [[, want -1", app.shift)
	}

	// Shifting must not apply to the live tab.
	send(app, "1", "]")
	if app.shift != 0 {
		t.Errorf("shifted the live tab to %d", app.shift)
	}
}

func TestQuit(t *testing.T) {
	app := newInteractApp(t)
	model, cmd := app.Update(key("q"))
	next := model.(*App)
	if !next.quitting {
		t.Error("q did not mark the app as quitting")
	}
	if cmd == nil {
		t.Fatal("q returned no quit command")
	}
	if cmd() == nil {
		t.Error("quit command produced no message")
	}
}

func TestWindowResizeKeepsLayoutValid(t *testing.T) {
	app := newInteractApp(t)
	for _, size := range []struct{ w, h int }{{40, 12}, {300, 80}, {80, 24}} {
		send(app, "enter") // open detail so both panes are exercised
		model, _ := app.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		next := model.(*App)
		*app = *next
		out := app.View()
		if lines := strings.Count(out, "\n") + 1; lines != size.h {
			t.Errorf("%dx%d rendered %d lines, want %d", size.w, size.h, lines, size.h)
		}
		send(app, "esc")
	}
}

func TestStateFiltersStayConsistent(t *testing.T) {
	app := newInteractApp(t)
	for _, tab := range []int{tabLive, tabUpcoming, tabResults} {
		app.tab = tab
		app.rebuild()
		for _, m := range app.list {
			var ok bool
			switch tab {
			case tabLive:
				ok = m.State.Live()
			case tabUpcoming:
				ok = m.State.Upcoming()
			case tabResults:
				ok = m.State.Finished()
			}
			if !ok {
				t.Fatalf("tab %d shows fixture %s in state %v", tab, m.ID, m.State)
			}
		}
	}
}
