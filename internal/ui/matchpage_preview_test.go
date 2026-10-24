package ui

import (
	"testing"

	"matchtui/internal/model"

	"github.com/charmbracelet/x/ansi"
)

// TestMatchPagePreview logs the rendered match page so `go test -v` can be used
// to eyeball the layout without a terminal.
func TestMatchPagePreview(t *testing.T) {
	if testing.Short() {
		t.Skip("preview only")
	}
	for _, size := range []struct{ w, h int }{{120, 40}, {80, 24}} {
		app := newTestApp(t, size.w, size.h, tabResults)
		if len(app.list) == 0 {
			t.Skip("no demo fixtures")
		}
		// Load team sheets so the full document is exercised.
		m := app.list[app.cursor]
		app.lineups[m.Key()] = mustLineups(t, m)
		app.detail = true

		t.Logf("\n===== match page %dx%d (top) =====\n%s",
			size.w, size.h, ansi.Strip(app.View()))

		// And again scrolled to the bottom.
		app.detailScroll = app.detailTotal
		t.Logf("\n===== match page %dx%d (bottom) =====\n%s",
			size.w, size.h, ansi.Strip(app.View()))
	}
}

// TestLiveMatchPreview logs a live fixture with a full timeline, so the two
// team columns can be eyeballed with `go test -v`.
func TestLiveMatchPreview(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 40}, {80, 24}} {
		app := newTestApp(t, size.w, size.h, tabLive)
		app.detail = true

		// Nothing may be in play right now, so drive the panel with a
		// synthetic live fixture carrying both teams' events.
		m := model.Match{
			ID: "live-preview", Provider: "demo", League: "Premier League",
			LeagueTag: "EPL", Round: "Matchweek 9",
			Home:  model.Team{ID: "h", Name: "Manchester City", Abbr: "MCI", Score: 2, ScoreHT: 1},
			Away:  model.Team{ID: "a", Name: "Newcastle United", Abbr: "NEW", Score: 1, ScoreHT: 1},
			State: model.StateLive, StatusText: "67'", Minute: 67,
			Venue: "Etihad Stadium", City: "Manchester",
			Events: []model.Event{
				{Kind: model.EventGoal, Minute: "12'", Player: "Silva", TeamID: "h", Score: "1-0"},
				{Kind: model.EventYellow, Minute: "19'", Player: "Rossi", TeamID: "a"},
				{Kind: model.EventGoal, Minute: "23'", Player: "Dubois", TeamID: "a", Score: "1-1"},
				{Kind: model.EventGoal, Minute: "41'", Player: "Kovacic", TeamID: "h", Score: "2-1", Penalty: true},
				{Kind: model.EventRed, Minute: "58'", Player: "Costa", TeamID: "a"},
				{Kind: model.EventGoal, Minute: "66'", Player: "Larsen", TeamID: "h", Score: "3-1"},
			},
			Stats: []model.Stat{
				{Label: "Possession", Home: "58%", Away: "42%", HomeVal: 58, AwayVal: 42},
				{Label: "Shots", Home: "14", Away: "7", HomeVal: 14, AwayVal: 7},
				{Label: "Expected goals", Home: "2.41", Away: "1.05", HomeVal: 2.41, AwayVal: 1.05},
			},
		}
		app.matches = []model.Match{m}
		app.rebuildKeep(m.Key())
		app.detailKey = m.Key()
		app.lineups[m.Key()] = mustLineups(t, m)

		t.Logf("\n===== live match %dx%d =====\n%s", size.w, size.h, ansi.Strip(app.View()))
	}
}
