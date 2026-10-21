package ui

import (
	"testing"

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
