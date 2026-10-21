package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"matchtui/internal/config"
	"matchtui/internal/model"
	"matchtui/internal/provider"

	"github.com/charmbracelet/x/ansi"
)

// TestVimKeybindings exercises the vim flavoured navigation on the list.
func TestVimKeybindings(t *testing.T) {
	app := newInteractApp(t)
	padMatchList(app, 40) // enough rows to page through
	if len(app.list) < 3 {
		t.Skip("need several fixtures")
	}

	// g jumps to the top, G to the bottom. A lone g does nothing but arm the
	// chord, exactly as vim does.
	send(app, "G")
	if app.cursor != len(app.list)-1 {
		t.Errorf("G moved cursor to %d, want %d", app.cursor, len(app.list)-1)
	}
	send(app, "g")
	if app.cursor != len(app.list)-1 {
		t.Errorf("a lone g moved cursor to %d, want it unchanged at %d", app.cursor, len(app.list)-1)
	}
	send(app, "g")
	if app.cursor != 0 {
		t.Errorf("gg moved cursor to %d, want 0", app.cursor)
	}

	send(app, "down", "down", "down")
	if app.cursor != 3 {
		t.Fatalf("cursor = %d after three downs, want 3", app.cursor)
	}
	send(app, "g")
	send(app, "g")
	if app.cursor != 0 {
		t.Errorf("gg moved cursor to %d, want 0", app.cursor)
	}

	// Any other key cancels a pending chord.
	send(app, "down", "down", "g", "j")
	if app.cursor != 3 {
		t.Errorf("cursor = %d, want 3 (g then j should not fire gg)", app.cursor)
	}

	// ctrl+d and ctrl+u move by half a page, and never leave the list.
	page := app.pageSize() / 2
	send(app, "g", "g", "ctrl+d")
	if app.cursor != page {
		t.Errorf("ctrl+d moved cursor to %d, want %d", app.cursor, page)
	}
	send(app, "ctrl+u")
	if app.cursor != 0 {
		t.Errorf("ctrl+u moved cursor to %d, want 0", app.cursor)
	}
	send(app, "ctrl+u")
	if app.cursor != 0 {
		t.Errorf("cursor = %d after ctrl+u at the top, want 0", app.cursor)
	}

	// h and l switch tabs, the vim mirror of left and right.
	app.tab = tabLive
	send(app, "l")
	if app.tab != tabUpcoming {
		t.Errorf("l selected tab %d, want upcoming", app.tab)
	}
	send(app, "h")
	if app.tab != tabLive {
		t.Errorf("h selected tab %d, want live", app.tab)
	}
	send(app, "h")
	if app.tab != tabStarred {
		t.Errorf("h should wrap to starred, got %d", app.tab)
	}
}

// TestMatchPageIsFullScreen checks that enter opens the full screen match page
// with the scoreline and team names pinned at the top.
func TestMatchPageIsFullScreen(t *testing.T) {
	app := newInteractApp(t)
	m := app.list[app.cursor]

	send(app, "enter")
	if !app.detail {
		t.Fatal("enter did not open the match page")
	}

	out := app.View()
	lines := strings.Split(out, "\n")
	if len(lines) != app.height {
		t.Errorf("match page rendered %d lines, want %d", len(lines), app.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > app.width {
			t.Fatalf("line %d is %d cells wide, want <= %d", i, w, app.width)
		}
	}

	// Team names sit on the same row, one either side of the score.
	joined := ansi.Strip(out)
	if !strings.Contains(joined, m.Home.Label(true)) {
		t.Errorf("match page is missing the home team %q", m.Home.Label(true))
	}
	if !strings.Contains(joined, m.Away.Label(true)) {
		t.Errorf("match page is missing the away team %q", m.Away.Label(true))
	}
	scoreRow := -1
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), m.Home.Label(true)) &&
			strings.Contains(ansi.Strip(line), m.Away.Label(true)) {
			scoreRow = i
			break
		}
	}
	if scoreRow < 0 {
		t.Fatal("no row carries both team names")
	}
	if scoreRow > 6 {
		t.Errorf("score row is at line %d, want it near the top", scoreRow)
	}

	// Esc and q both return to the list.
	send(app, "esc")
	if app.detail {
		t.Error("esc did not close the match page")
	}
	send(app, "enter", "q")
	if app.detail {
		t.Error("q did not close the match page")
	}
	if app.quitting {
		t.Error("q on the match page quit the app instead of closing the page")
	}
}

// TestMatchPageScrolls checks the page scrolls its own content, clamped at both
// ends, without moving the list cursor underneath.
func TestMatchPageScrolls(t *testing.T) {
	app := newInteractApp(t)
	cursor := app.cursor

	// Give the page more content than one screen: real team sheets plus a long
	// synthetic timeline, so scrolling is genuinely needed at any terminal size.
	target := app.list[cursor]
	app.lineups[target.Key()] = mustLineups(t, target)
	for i := 0; i < 60; i++ {
		target.Events = append(target.Events, model.Event{
			Kind: model.EventGoal, Minute: strconv.Itoa(i+1) + "'",
			Player: "Player " + strconv.Itoa(i), Score: "1-0",
		})
	}
	for i, m := range app.matches {
		if m.Key() == target.Key() {
			app.matches[i] = target
		}
	}
	app.rebuildKeep(target.Key())

	send(app, "enter")
	if !app.detail {
		t.Fatal("enter did not open the match page")
	}
	app.View() // a render clamps the scroll against the real content height
	if app.detailTotal <= app.detailPageSize() {
		t.Fatalf("page has %d lines, which fits in %d; cannot test scrolling",
			app.detailTotal, app.detailPageSize())
	}

	send(app, "j", "j")
	if app.detailScroll != 2 {
		t.Errorf("detail scroll = %d after two j, want 2", app.detailScroll)
	}
	send(app, "k")
	if app.detailScroll != 1 {
		t.Errorf("detail scroll = %d after a k, want 1", app.detailScroll)
	}
	send(app, "k", "k", "k")
	if app.detailScroll != 0 {
		t.Errorf("detail scroll = %d at the top, want 0", app.detailScroll)
	}

	send(app, "G")
	if app.detailScroll == 0 {
		t.Error("G did not scroll the match page")
	}
	// G overshoots on purpose; the next render clamps it to the real end.
	app.View()
	if app.detailScroll > app.detailTotal-app.detailPageSize() {
		t.Errorf("detail scroll = %d past the end (total %d)", app.detailScroll, app.detailTotal)
	}
	atEnd := app.detailScroll
	send(app, "G")
	app.View()
	if app.detailScroll != atEnd {
		t.Errorf("detail scroll = %d after a second G, want %d", app.detailScroll, atEnd)
	}
	if app.detailTo != app.detailTotal {
		t.Errorf("footer reports the window ending at %d, want the last line %d",
			app.detailTo, app.detailTotal)
	}

	if app.cursor != cursor {
		t.Errorf("scrolling moved the list cursor from %d to %d", cursor, app.cursor)
	}
}

// TestTeamSheetsLoadOnOpen checks that opening a match asks the provider for
// the team sheets, and that they reach the rendered page.
func TestTeamSheetsLoadOnOpen(t *testing.T) {
	isolateConfig(t)
	cfg := config.Default()
	cfg.Provider = "demo"
	app := New(cfg, "")
	app.width, app.height = 120, 40

	p, _ := provider.ByID(app.providers, "demo")
	d, ok := provider.AsDetailer(p)
	if !ok {
		t.Fatal("demo provider should expose team sheets")
	}
	req, _ := app.requestFor(tabResults)
	matches, err := p.Fetch(t.Context(), req)
	if err != nil {
		t.Fatalf("demo fetch: %v", err)
	}
	app.Update(matchesMsg{
		seq: app.loadSeq, providerID: p.ID(), days: req.Days(),
		matches: matches, at: app.now,
	})
	app.tab = tabResults
	app.rebuild()
	if len(app.list) == 0 {
		t.Skip("no fixtures")
	}
	target := app.list[0]

	// Opening the page issues the on-demand request...
	cmd := app.openDetail()
	if cmd == nil {
		t.Fatal("opening a match returned no team sheet command")
	}
	if !app.detailLoading {
		t.Error("detail is not marked loading")
	}

	// ...and a second open does not repeat it.
	if again := app.openDetail(); again != nil {
		t.Error("opening again while loading returned another request")
	}

	enriched, err := d.FetchDetail(t.Context(), target)
	if err != nil {
		t.Fatalf("demo detail: %v", err)
	}
	if len(enriched.Lineups) != 2 {
		t.Fatalf("demo returned %d team sheets, want 2", len(enriched.Lineups))
	}

	app.Update(lineupsMsg{
		key: target.Key(), providerID: p.ID(), lineups: enriched.Lineups,
	})
	if app.detailLoading {
		t.Error("detail still marked loading after the sheets arrived")
	}
	if len(app.lineups[target.Key()]) != 2 {
		t.Fatal("team sheets were not cached")
	}

	// Both sheets are complete elevens, with the sheets shown on the page.
	for _, l := range enriched.Lineups {
		if len(l.Starters) != 11 {
			t.Errorf("%s has %d starters, want 11", l.TeamName, len(l.Starters))
		}
		if l.Formation == "" {
			t.Errorf("%s has no formation", l.TeamName)
		}
	}
	joined := ansi.Strip(app.View())
	if !strings.Contains(joined, enriched.Lineups[0].Starters[0].Name) {
		t.Error("the rendered match page is missing a starter's name")
	}

	// Cached sheets mean the next open costs no request.
	send(app, "esc")
	if cmd := app.openDetail(); cmd != nil {
		t.Error("reopening a cached match returned another request")
	}
}

// TestMatchPageFitsEverySize renders the match page across the terminal sizes
// the app has to survive, with team sheets present.
func TestMatchPageFitsEverySize(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{160, 48}, {120, 40}, {100, 30}, {80, 24}, {60, 20}, {48, 14},
	} {
		app := newTestApp(t, size.w, size.h, tabResults)
		if len(app.list) == 0 {
			t.Fatal("no demo fixtures")
		}
		m := app.list[0]
		app.lineups[m.Key()] = mustLineups(t, m)
		app.detail = true

		out := app.View()
		lines := strings.Split(out, "\n")
		if len(lines) != size.h {
			t.Errorf("%dx%d: match page is %d lines, want %d", size.w, size.h, len(lines), size.h)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > size.w {
				t.Errorf("%dx%d: line %d is %d cells wide, want <= %d\n%q",
					size.w, size.h, i, w, size.w, ansi.Strip(line))
			}
		}
	}
}

// TestMatchPageForUnstartedFixture checks a fixture with no timeline yet, which
// must still render a usable page.
func TestMatchPageForUnstartedFixture(t *testing.T) {
	app := newInteractApp(t)
	app.tab = tabUpcoming
	app.rebuild()
	if len(app.list) == 0 {
		t.Skip("no upcoming fixtures")
	}

	send(app, "enter")
	if !app.detail {
		t.Fatal("enter did not open the match page")
	}
	joined := ansi.Strip(app.View())
	if !strings.Contains(joined, "Team sheets") {
		t.Error("unstarted fixture page is missing the team sheets section")
	}
	if strings.Contains(joined, "Timeline") {
		t.Error("unstarted fixture should not show a timeline section")
	}
}

// TestMatchPageShowsTeamSheetFallback checks the messaging when a feed cannot
// provide team sheets at all.
func TestMatchPageShowsTeamSheetFallback(t *testing.T) {
	app := newInteractApp(t)
	app.detail = true
	app.detailErr = errFake("boom")
	joined := ansi.Strip(app.View())
	if !strings.Contains(joined, "Could not load") {
		t.Errorf("match page does not surface the team sheet error:\n%s", joined)
	}
}

// TestStarFromMatchPage checks that starring works while the page is open.
func TestStarFromMatchPage(t *testing.T) {
	isolateConfig(t)
	cfg := config.Default()
	cfg.Provider = "demo"
	app := New(cfg, "")
	app.width, app.height = 120, 40
	app.tab = tabResults

	p, _ := provider.ByID(app.providers, "demo")
	req, _ := app.requestFor(tabResults)
	matches, _ := p.Fetch(t.Context(), req)
	app.Update(matchesMsg{
		seq: app.loadSeq, providerID: p.ID(), days: req.Days(),
		matches: matches, at: app.now,
	})
	app.tab = tabResults
	app.rebuild()
	if len(app.list) == 0 {
		t.Skip("no fixtures")
	}
	target := app.list[0]

	send(app, "enter", "s")
	if !cfg.IsStarred(target.Key()) {
		t.Error("s on the match page did not star the fixture")
	}
	send(app, "esc", "4")
	found := false
	for _, m := range app.list {
		if m.Key() == target.Key() {
			found = true
		}
	}
	if !found {
		t.Error("starred match is missing from the starred tab")
	}
}

// TestMatchPageSurvivesRefresh checks that a background refresh or a filter
// change cannot swap the open page onto a different match, even when the match
// it is showing drops out of the visible list.
func TestMatchPageSurvivesRefresh(t *testing.T) {
	app := newInteractApp(t)
	if len(app.list) < 2 {
		t.Skip("need several fixtures")
	}
	open := app.list[0]
	other := app.list[1]

	send(app, "enter")
	if app.detailKey != open.Key() {
		t.Fatalf("page pinned to %q, want %q", app.detailKey, open.Key())
	}

	// A filter that matches nothing but the other fixtures empties the list
	// under the page. The page must keep showing the fixture being read.
	app.filter = other.Home.Label(true)
	app.rebuild()
	if len(app.list) == 0 || app.list[0].Key() == open.Key() {
		t.Skip("filter did not remove the open fixture from the list")
	}

	joined := ansi.Strip(app.View())
	if !strings.Contains(joined, open.Home.Label(true)) {
		t.Errorf("the page swapped off the fixture being read; now showing %q",
			other.Home.Label(true))
	}
}

// TestStarActsOnOpenMatch checks the star follows the page, not the cursor.
func TestStarActsOnOpenMatch(t *testing.T) {
	isolateConfig(t)
	cfg := config.Default()
	cfg.Provider = "demo"
	app := New(cfg, "")
	app.width, app.height = 120, 40

	p, _ := provider.ByID(app.providers, "demo")
	req, _ := app.requestFor(tabResults)
	matches, _ := p.Fetch(t.Context(), req)
	app.Update(matchesMsg{
		seq: app.loadSeq, providerID: p.ID(), days: req.Days(),
		matches: matches, at: app.now,
	})
	app.tab = tabResults
	app.rebuild()
	if len(app.list) < 2 {
		t.Skip("need several fixtures")
	}

	open := app.list[0]
	send(app, "enter")

	// Move the cursor underneath the page; the star must still hit `open`.
	app.cursor = 1
	send(app, "s")
	if !cfg.IsStarred(open.Key()) {
		t.Errorf("s starred %q, want the open fixture %q",
			app.list[1].Key(), open.Key())
	}
}

// errFake is a stand-in error for the fallback rendering test.
type errFake string

func (e errFake) Error() string { return string(e) }

// padMatchList repeats the loaded fixtures until the current tab has at least
// n rows, so paging behaviour can be tested regardless of what the demo feed
// happens to generate for today.
func padMatchList(a *App, n int) {
	if len(a.matches) == 0 || len(a.list) >= n {
		return
	}
	seed := a.list
	for len(a.list) < n {
		for _, m := range seed {
			if len(a.list) >= n {
				break
			}
			c := m
			c.ID = m.ID + "-pad" + strconv.Itoa(len(a.list))
			c.Kickoff = m.Kickoff.Add(time.Duration(len(a.list)) * time.Minute)
			a.matches = append(a.matches, c)
		}
	}
	a.rebuild()
}

// mustLineups builds a team sheet pair for a fixture, mirroring what a
// Detailer would return.
func mustLineups(t *testing.T, m model.Match) []model.Lineup {
	t.Helper()
	p, _ := provider.ByID(provider.Builtin(nil), "demo")
	d, ok := provider.AsDetailer(p)
	if !ok {
		t.Fatal("demo provider should expose team sheets")
	}
	enriched, err := d.FetchDetail(t.Context(), m)
	if err != nil {
		t.Fatalf("demo detail: %v", err)
	}
	return enriched.Lineups
}
