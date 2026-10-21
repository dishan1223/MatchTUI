package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// theme keeps every colour and reusable style in one place so views stay
// declarative and the palette stays consistent.
type theme struct {
	accent    lipgloss.TerminalColor
	accentDim lipgloss.TerminalColor
	amber     lipgloss.TerminalColor
	crimson   lipgloss.TerminalColor
	ink       lipgloss.TerminalColor
	muted     lipgloss.TerminalColor
	faint     lipgloss.TerminalColor
	line      lipgloss.TerminalColor
	panel     lipgloss.TerminalColor
	selBg     lipgloss.TerminalColor
	selInk    lipgloss.TerminalColor

	title      lipgloss.Style
	chip       lipgloss.Style
	chipWarn   lipgloss.Style
	chipErr    lipgloss.Style
	tabOn      lipgloss.Style
	tabOff     lipgloss.Style
	tabCount   lipgloss.Style
	group      lipgloss.Style
	team       lipgloss.Style
	teamWin    lipgloss.Style
	teamLose   lipgloss.Style
	teamSel    lipgloss.Style
	score      lipgloss.Style
	scoreSel   lipgloss.Style
	statusLive lipgloss.Style
	statusRest lipgloss.Style
	meta       lipgloss.Style
	metaSel    lipgloss.Style
	star       lipgloss.Style
	starSel    lipgloss.Style
	footer     lipgloss.Style
	footerKey  lipgloss.Style
	footerDim  lipgloss.Style
	err        lipgloss.Style
	ok         lipgloss.Style
	warn       lipgloss.Style
	faintText  lipgloss.Style
	panelBox   lipgloss.Style
	panelTitle lipgloss.Style
	listSel    lipgloss.Style
	statLabel  lipgloss.Style
	statValue  lipgloss.Style
	barFill    lipgloss.Style
	barEmpty   lipgloss.Style
	tlMinute   lipgloss.Style
	tlKind     lipgloss.Style
	tlGoal     lipgloss.Style
	tlCard     lipgloss.Style
	tlPlayer   lipgloss.Style
	tlScore    lipgloss.Style
	empty      lipgloss.Style
	dimmed     lipgloss.Style
}

// shade returns a colour for the configured scheme: forced light/dark when the
// user picked one, adaptive otherwise.
func shade(light, dark, scheme string) lipgloss.TerminalColor {
	switch scheme {
	case "light":
		return lipgloss.Color(light)
	case "dark":
		return lipgloss.Color(dark)
	default:
		return lipgloss.AdaptiveColor{Light: light, Dark: dark}
	}
}

// newTheme builds the palette. The colours lean on a pitch green accent with a
// warm amber for kickoff times and a crimson for anything live.
func newTheme(scheme string) theme {
	t := theme{
		accent:    shade("#0E7A46", "#35D68B", scheme),
		accentDim: shade("#3F8F68", "#1C7A52", scheme),
		amber:     shade("#8A5200", "#FFB454", scheme),
		crimson:   shade("#B3261E", "#FF6B6B", scheme),
		ink:       shade("#151A21", "#E7ECF3", scheme),
		muted:     shade("#59626F", "#8B95A7", scheme),
		faint:     shade("#8A94A3", "#5C6575", scheme),
		line:      shade("#D3D9E2", "#29313D", scheme),
		panel:     shade("#F1F4F8", "#12171F", scheme),
		selBg:     shade("#D6EEDF", "#16352A", scheme),
		selInk:    shade("#08542F", "#8CF2C0", scheme),
	}

	t.title = lipgloss.NewStyle().Bold(true).Foreground(t.accent)
	t.chip = lipgloss.NewStyle().
		Foreground(t.ink).Background(t.panel).
		Padding(0, 1).Bold(true)
	t.chipWarn = lipgloss.NewStyle().
		Foreground(shade("#7A4400", "#2A1D05", scheme)).
		Background(t.amber).Padding(0, 1)
	t.chipErr = lipgloss.NewStyle().
		Foreground(shade("#FFFFFF", "#2A0B09", scheme)).
		Background(t.crimson).Padding(0, 1)

	t.tabOn = lipgloss.NewStyle().
		Bold(true).Foreground(t.selInk).Background(t.selBg).Padding(0, 1)
	t.tabOff = lipgloss.NewStyle().
		Foreground(t.muted).Background(t.panel).Padding(0, 1)
	t.tabCount = lipgloss.NewStyle().Faint(true)

	t.group = lipgloss.NewStyle().Bold(true).Foreground(t.accent).Padding(0, 0, 0, 1)

	t.team = lipgloss.NewStyle().Foreground(t.ink)
	t.teamWin = lipgloss.NewStyle().Foreground(t.ink).Bold(true)
	t.teamLose = lipgloss.NewStyle().Foreground(t.muted)
	t.teamSel = lipgloss.NewStyle().Foreground(t.selInk).Bold(true)
	t.score = lipgloss.NewStyle().Foreground(t.ink).Bold(true)
	t.scoreSel = lipgloss.NewStyle().Foreground(t.selInk).Bold(true)
	t.statusLive = lipgloss.NewStyle().Foreground(t.crimson).Bold(true)
	t.statusRest = lipgloss.NewStyle().Foreground(t.amber)
	t.meta = lipgloss.NewStyle().Foreground(t.faint)
	t.metaSel = lipgloss.NewStyle().Foreground(t.accentDim)
	t.star = lipgloss.NewStyle().Foreground(t.amber).Bold(true)
	t.starSel = lipgloss.NewStyle().Foreground(t.selInk).Bold(true)

	t.listSel = lipgloss.NewStyle().Background(t.selBg)

	t.footer = lipgloss.NewStyle().Foreground(t.muted)
	t.footerKey = lipgloss.NewStyle().Foreground(t.accent).Bold(true)
	t.footerDim = lipgloss.NewStyle().Foreground(t.faint)

	t.err = lipgloss.NewStyle().Foreground(t.crimson)
	t.ok = lipgloss.NewStyle().Foreground(t.accent)
	t.warn = lipgloss.NewStyle().Foreground(t.amber)
	t.faintText = lipgloss.NewStyle().Foreground(t.faint)
	t.empty = lipgloss.NewStyle().Foreground(t.faint).Italic(true)
	t.dimmed = lipgloss.NewStyle().Foreground(t.faint)

	t.panelBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.line).
		Background(t.panel).
		Padding(0, 1)
	t.panelTitle = lipgloss.NewStyle().Bold(true).Foreground(t.accent)

	t.statLabel = lipgloss.NewStyle().Foreground(t.muted)
	t.statValue = lipgloss.NewStyle().Foreground(t.ink).Bold(true)
	t.barFill = lipgloss.NewStyle().Background(t.accent)
	t.barEmpty = lipgloss.NewStyle().Background(t.line)

	t.tlMinute = lipgloss.NewStyle().Foreground(t.amber)
	t.tlKind = lipgloss.NewStyle().Bold(true).Foreground(t.accent)
	t.tlGoal = lipgloss.NewStyle().Foreground(t.accent)
	t.tlCard = lipgloss.NewStyle().Foreground(t.amber)
	t.tlPlayer = lipgloss.NewStyle().Foreground(t.ink)
	t.tlScore = lipgloss.NewStyle().Foreground(t.muted)

	return t
}

// ---- text helpers ---------------------------------------------------------

// width returns the printable cell width of a string, ignoring ANSI escapes.
func width(s string) int { return ansi.StringWidth(s) }

// padRight appends spaces until s is w cells wide.
func padRight(s string, w int) string {
	if n := w - width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// padLeft prepends spaces until s is w cells wide.
func padLeft(s string, w int) string {
	if n := w - width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// trunc cuts s to at most w cells, keeping ANSI sequences intact.
func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "")
}

// fit truncates then pads, giving a fixed width column.
func fit(s string, w int) string { return padRight(trunc(s, w), w) }

// clamp keeps a value inside a range.
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// bar renders a proportional bar using coloured spaces, which keeps the
// rendering width identical in every terminal.
func (th theme) bar(ratio float64, w int) string {
	if w <= 0 {
		return ""
	}
	filled := int(ratio*float64(w) + 0.5)
	filled = clamp(filled, 0, w)
	return th.barFill.Width(filled).Render("") + th.barEmpty.Width(w-filled).Render("")
}

// joinNonEmpty joins parts with a separator, skipping empty ones.
func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
