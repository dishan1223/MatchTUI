package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestDebugWidths(t *testing.T) {
	app := newTestApp(t, 60, 20, tabLive)
	app.detail = true

	head := lipgloss.JoinVertical(lipgloss.Left, app.headerLine(), app.tabLine())
	t.Logf("header: %d", ansi.StringWidth(app.headerLine()))
	t.Logf("tabs:   %d", ansi.StringWidth(app.tabLine()))
	t.Logf("head:   %d", ansi.StringWidth(head))
	footer := app.footerView()
	t.Logf("footer: %d", ansi.StringWidth(footer))
	bodyH := app.height - lipgloss.Height(head) - lipgloss.Height(footer)
	t.Logf("bodyH:  %d", bodyH)
	empty := app.emptyList(app.width, bodyH)
	t.Logf("empty:  %d x %d", ansi.StringWidth(empty), lipgloss.Height(empty))
	box := app.theme.panelBox.Width(minInt(app.width, 64) - 4).Render("hi")
	t.Logf("box:    %d", ansi.StringWidth(box))
	placed := lipgloss.Place(app.width, bodyH, lipgloss.Center, lipgloss.Center, empty)
	t.Logf("placed: %d x %d", ansi.StringWidth(placed), lipgloss.Height(placed))
	t.Logf("view:   %d", ansi.StringWidth(app.View()))
}
