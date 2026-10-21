package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"matchtui/internal/config"
	"matchtui/internal/provider"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// newTestApp builds an app sized to a terminal and populated with the demo
// feed's fixtures, which exercises every rendering path without a network.
func newTestApp(t *testing.T, w, h int, tab int) *App {
	t.Helper()
	isolateConfig(t)

	cfg := config.Default()
	cfg.Provider = "demo"
	a := New(cfg, "")

	model, _ := a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	app := model.(*App)

	p, ok := provider.ByID(app.providers, "demo")
	if !ok {
		t.Fatal("demo provider missing")
	}
	req, _ := app.requestFor(tab)
	ms, err := p.Fetch(context.Background(), req)
	if err != nil {
		t.Fatalf("demo fetch: %v", err)
	}
	app.Update(matchesMsg{
		seq:        app.loadSeq,
		providerID: p.ID(),
		days:       req.Days(),
		matches:    ms,
		at:         time.Now(),
	})
	app.tab = tab
	app.rebuild()
	return app
}

// TestViewFitsTerminal guards the layout against overflow at the widths and
// heights the app has to survive.
func TestViewFitsTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{
		{140, 42}, {120, 34}, {100, 30}, {80, 24}, {60, 20}, {48, 16},
	}
	tabs := []int{tabLive, tabUpcoming, tabResults, tabStarred}

	for _, size := range sizes {
		for _, tab := range tabs {
			app := newTestApp(t, size.w, size.h, tab)
			app.detail = true
			out := app.View()

			lines := strings.Split(out, "\n")
			if got := len(lines); got != size.h {
				t.Errorf("%dx%d tab %d: view is %d lines, want %d",
					size.w, size.h, tab, got, size.h)
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > size.w {
					t.Errorf("%dx%d tab %d: line %d is %d cells wide, want <= %d\n%q",
						size.w, size.h, tab, i, w, size.w, ansi.Strip(line))
				}
			}
		}
	}
}

// TestOverlaysFitTerminal checks the menus render inside the terminal too.
func TestOverlaysFitTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 40}, {80, 24}, {60, 20}} {
		app := newTestApp(t, size.w, size.h, tabLive)
		app.leagues, _ = app.active.Leagues(context.Background())

		for _, menu := range []int{menuProvider, menuLeagues, menuHelp, menuKey} {
			app.openMenu(menu)
			out := app.View()
			lines := strings.Split(out, "\n")
			if got := len(lines); got != size.h {
				t.Errorf("%dx%d menu %d: view is %d lines, want %d", size.w, size.h, menu, got, size.h)
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > size.w {
					t.Errorf("%dx%d menu %d: line %d is %d cells wide, want <= %d",
						size.w, size.h, menu, i, w, size.w)
				}
			}
		}
	}
}

// TestRealFeedRender drives real ESPN data through the layout, which is the
// only way to catch overflow from long club names like "Paris Saint-Germain".
func TestRealFeedRender(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := config.Default()
	cfg.Provider = "espn"
	cfg.SetSelectedLeagues("espn", []string{"eng.1", "uefa.champions", "esp.1"})
	app := New(cfg, "")

	p, ok := provider.ByID(app.providers, "espn")
	if !ok {
		t.Fatal("espn provider missing")
	}
	req, _ := app.requestFor(tabResults)
	matches, err := p.Fetch(ctx, req)
	if err != nil {
		t.Fatalf("espn fetch: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no real matches in this window")
	}

	for _, size := range []struct{ w, h int }{{120, 40}, {100, 30}, {80, 24}, {60, 20}} {
		app.width, app.height = size.w, size.h
		app.tab = tabResults
		app.matches = nil
		app.Update(matchesMsg{
			seq:        app.loadSeq,
			providerID: "espn",
			days:       req.Days(),
			matches:    matches,
			at:         time.Now(),
		})
		app.detail = true
		out := app.View()
		for i, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > size.w {
				t.Errorf("%dx%d line %d is %d cells wide\n%q",
					size.w, size.h, i, w, ansi.Strip(line))
			}
		}
		if size.w == 120 {
			t.Log("\n===== real ESPN data =====\n" + ansi.Strip(out))
		}
	}
}

// TestPreview logs the rendered frames so `go test -v` can be used to eyeball
// the layout without a terminal.
func TestPreview(t *testing.T) {
	if testing.Short() {
		t.Skip("preview only")
	}
	base := tabLive
	app := newTestApp(t, 120, 40, tabLive)
	if len(app.list) == 0 {
		// Nothing is in play at this moment; results still exercise the layout.
		base = tabResults
		app = newTestApp(t, 120, 40, tabResults)
	}
	if len(app.list) == 0 {
		t.Skip("no demo matches to preview")
	}

	dump := func(name string, mutate func(*App)) {
		mutate(app)
		t.Log("\n===== " + name + " =====\n" + ansi.Strip(app.View()))
	}

	dump("live list", func(a *App) {})
	dump("detail open", func(a *App) { a.detail = true })
	dump("provider menu", func(a *App) {
		a.detail = false
		a.leagues, _ = a.active.Leagues(context.Background())
		a.openMenu(menuProvider)
	})
	dump("league menu", func(a *App) { a.openMenu(menuLeagues) })
	dump("help", func(a *App) { a.openMenu(menuHelp) })
	dump("api key prompt", func(a *App) { a.openKeyPrompt("apifootball") })
	dump("filter active", func(a *App) {
		a.menu = menuNone
		a.filter = "a"
		a.rebuild()
	})
	dump("upcoming", func(a *App) {
		a.filter = ""
		a.tab = tabUpcoming
		a.rebuild()
	})
	dump("results", func(a *App) {
		a.tab = tabResults
		a.rebuild()
	})
	dump("narrow terminal", func(a *App) {
		a.tab = base
		a.rebuild()
		a.width, a.height = 70, 24
		a.detail = true
	})
}
