package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"matchtui/internal/model"
	"matchtui/internal/provider"

	"github.com/charmbracelet/lipgloss"
)

// Version is reported in the header and --version output.
const Version = "1.0.0"

// View renders the whole frame.
func (a *App) View() string {
	if a.quitting {
		return ""
	}
	if a.width <= 0 || a.height <= 0 {
		return a.theme.faintText.Render("starting MatchTUI…")
	}

	head := lipgloss.JoinVertical(lipgloss.Left, a.headerLine(), a.tabLine())
	if line := a.filterLine(); line != "" {
		head = lipgloss.JoinVertical(lipgloss.Left, head, line)
	}
	if line := a.bannerLine(); line != "" {
		head = lipgloss.JoinVertical(lipgloss.Left, head, line)
	}

	// The match page owns the whole terminal: the score stays pinned at the
	// top and everything else scrolls beneath it.
	if a.detail && a.menu == menuNone {
		return a.fitFrame(a.matchPageView())
	}

	footer := a.footerView()
	bodyH := a.height - lipgloss.Height(head) - lipgloss.Height(footer)
	if bodyH < 3 {
		bodyH = 3
	}

	var body string
	switch a.menu {
	case menuProvider:
		body = a.centered(a.providerMenuView(), bodyH)
	case menuLeagues:
		body = a.centered(a.leagueMenuView(), bodyH)
	case menuHelp:
		body = a.centered(a.helpView(), bodyH)
	case menuKey:
		body = a.centered(a.keyPromptView(), bodyH)
	default:
		body = a.mainView(bodyH)
	}

	return a.fitFrame(lipgloss.JoinVertical(lipgloss.Left, head, body, footer))
}

// fitFrame clips the rendered frame to the terminal so a long label can never
// wrap the screen and leave artefacts behind, and pads it to a full screen so
// the renderer always repaints every row.
func (a *App) fitFrame(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > a.height {
		lines = lines[:a.height]
	}
	for i, line := range lines {
		lines[i] = trunc(line, a.width)
	}
	for len(lines) < a.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// centered places an overlay box in the middle of the body area.
func (a *App) centered(content string, h int) string {
	return lipgloss.Place(a.width, h, lipgloss.Center, lipgloss.Center, content)
}

// panelWidth is the width used by overlay panels.
func (a *App) panelWidth() int { return minInt(a.width-2, 76) }

// ---- header ---------------------------------------------------------------

func (a *App) headerLine() string {
	left := "⚽ " + a.theme.title.Render("MatchTUI") +
		"  " + a.theme.footerDim.Render("v"+Version)
	right := a.providerChip()
	return fit(left, maxInt(0, a.width-width(right))) + right
}

// providerChip summarises the active feed and anything it needs from the user.
func (a *App) providerChip() string {
	if a.active.NeedsKey() && !a.active.KeyPresent() {
		return a.theme.chipWarn.Render("no key · press p")
	}
	label := a.active.Name()
	if hint := a.active.RateHint(); hint > 0 {
		label += " · max " + humanDuration(hint)
	}
	return a.theme.chip.Render(label)
}

// tabLine draws the tab bar with live counts, and the clock on the right.
func (a *App) tabLine() string {
	counts := a.counts()
	names := []string{"Live", "Upcoming", "Results", "Starred"}

	parts := make([]string, 0, len(names))
	for i, name := range names {
		label := name
		if counts[i] > 0 {
			label += " " + a.theme.tabCount.Render(strconv.Itoa(counts[i]))
		}
		if i == a.tab {
			parts = append(parts, a.theme.tabOn.Render(label))
		} else {
			parts = append(parts, a.theme.tabOff.Render(label))
		}
	}
	left := strings.Join(parts, " ")
	right := a.clockLine()
	return fit(left, maxInt(0, a.width-width(right))) + right
}

// clockLine reports the local time plus either the refresh countdown or the
// window being browsed.
func (a *App) clockLine() string {
	if a.loading {
		return a.theme.faintText.Render(a.spin.View() + " updating")
	}
	parts := []string{a.now.Format("Mon 2 Jan"), a.now.Format("15:04")}
	switch a.tab {
	case tabUpcoming, tabResults:
		parts = append(parts, a.windowLabel())
	default:
		parts = append(parts, "refresh in "+strconv.Itoa(a.secondsToRefresh())+"s")
	}
	return a.theme.footerDim.Render(strings.Join(parts, " · "))
}

// secondsToRefresh is the countdown to the next automatic refresh.
func (a *App) secondsToRefresh() int {
	if a.nextFetch.IsZero() {
		return int(a.refreshInterval().Seconds())
	}
	left := int(a.nextFetch.Sub(a.now).Seconds())
	if left < 0 {
		return 0
	}
	return left
}

// windowLabel describes the date range behind the upcoming/results tabs.
func (a *App) windowLabel() string {
	req, _ := a.requestFor(a.tab)
	days := req.Days()
	if len(days) == 0 {
		return ""
	}
	from, to := days[0], days[len(days)-1]
	var label string
	if from.Format("2006-01-02") == to.Format("2006-01-02") {
		label = from.Format("2 Jan")
	} else if from.Month() == to.Month() {
		label = fmt.Sprintf("%d-%d %s", from.Day(), to.Day(), from.Format("Jan"))
	} else {
		label = from.Format("2 Jan") + " - " + to.Format("2 Jan")
	}
	if a.shift != 0 {
		label += fmt.Sprintf(" (shift %+d)", a.shift)
	}
	return label
}

// filterLine shows the filter input, or the active filter as a chip.
func (a *App) filterLine() string {
	if a.filterMode {
		return a.ti.View()
	}
	if a.filter == "" {
		return ""
	}
	return a.theme.footer.Render("filter ") +
		a.theme.chip.Render(a.filter) +
		a.theme.footerDim.Render("  "+strconv.Itoa(len(a.list))+" match(es) · esc clears")
}

// bannerLine surfaces fetch errors and transient status messages.
func (a *App) bannerLine() string {
	if a.err != nil {
		return a.theme.err.Render("! " + trunc(a.err.Error(), a.width-4))
	}
	if a.status != "" && time.Since(a.statusA) < 4*time.Second {
		return a.theme.ok.Render("+ " + trunc(a.status, a.width-4))
	}
	return ""
}

// ---- body -----------------------------------------------------------------

// mainView renders the match list.
func (a *App) mainView(h int) string {
	return a.renderList(a.width, h)
}

// teamColumnWidth sizes the team columns from the terminal width.
func (a *App) teamColumnWidth(listW int) int {
	return clamp((listW-40)/2, 8, 22)
}

// renderList draws the visible slice of matches with group headers.
func (a *App) renderList(w, h int) string {
	if len(a.list) == 0 {
		return a.emptyList(w, h)
	}
	teamW := a.teamColumnWidth(w)

	cost := func(i int) int {
		if a.headerFor(i) != "" {
			return 2
		}
		return 1
	}
	start := a.cursor
	used := cost(start)
	for start > 0 {
		n := cost(start - 1)
		if used+n > h {
			break
		}
		used += n
		start--
	}

	lines := make([]string, 0, h)
	for i := start; i < len(a.list) && len(lines) < h; i++ {
		if hdr := a.headerFor(i); hdr != "" {
			lines = append(lines, a.theme.group.Render(trunc(hdr, w)))
		}
		if len(lines) >= h {
			break
		}
		lines = append(lines, a.rowFor(a.list[i], teamW, w, i == a.cursor))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// headerFor returns the group heading shown above a row, or "" for none.
func (a *App) headerFor(i int) string {
	if i < 0 || i >= len(a.list) {
		return ""
	}
	// The first row always opens a group so the tab has a heading even when
	// every match shares one.
	if i == 0 {
		switch a.tab {
		case tabLive:
			return a.list[0].League
		case tabUpcoming, tabResults:
			return a.dayLabel(a.list[0].Kickoff)
		case tabStarred:
			return stateGroup(a.list[0])
		}
		return ""
	}

	cur, prev := a.list[i], a.list[i-1]
	switch a.tab {
	case tabLive:
		if cur.League != prev.League {
			return cur.League
		}
	case tabUpcoming, tabResults:
		if dayKey(cur) != dayKey(prev) {
			return a.dayLabel(cur.Kickoff)
		}
	case tabStarred:
		if stateGroup(cur) != stateGroup(prev) {
			return stateGroup(cur)
		}
	}
	return ""
}

// dayLabel renders "Today", "Tomorrow" or a full date.
func (a *App) dayLabel(t time.Time) string {
	day := startOfDay(t)
	switch day.Sub(startOfDay(a.now)).Hours() {
	case 0:
		return "Today · " + t.Format("Monday 2 January")
	case 24:
		return "Tomorrow · " + t.Format("Monday 2 January")
	case -24:
		return "Yesterday · " + t.Format("Monday 2 January")
	}
	return t.Format("Monday 2 January")
}

// stateGroup groups the starred tab by lifecycle stage.
func stateGroup(m model.Match) string {
	switch {
	case m.State.Live():
		return "Live now"
	case m.State.Upcoming():
		return "Upcoming"
	default:
		return "Results"
	}
}

// rowFor renders one match as a fixed set of columns inside a list of the
// given width.
func (a *App) rowFor(m model.Match, teamW, listW int, selected bool) string {
	const stW, scoreW = 7, 9
	t := a.theme

	star := " "
	if a.stars[m.Key()] {
		star = "*"
	}
	starStyle := t.star
	if selected {
		starStyle = t.starSel
	}

	// Status column: minute for live, kickoff for fixtures, FT otherwise.
	status := m.StatusText
	var statusStyle lipgloss.Style
	switch {
	case m.State.Live():
		statusStyle = t.statusLive
	case m.State.Upcoming():
		statusStyle = t.statusRest
	default:
		statusStyle = t.faintText
	}

	// Score column.
	mid := m.ScoreLine()
	if m.State == model.StateScheduled || m.State == model.StatePostponed {
		mid = "vs"
	}

	meta := joinNonEmpty(" · ", m.LeagueTag, m.Venue)
	if m.State.Upcoming() && m.Round != "" {
		meta = joinNonEmpty(" · ", meta, m.Round)
	}

	teamStyle := t.team
	scoreStyle := t.score
	metaStyle := t.meta
	if selected {
		teamStyle, scoreStyle, metaStyle = t.teamSel, t.scoreSel, t.metaSel
	} else if m.State.Finished() && !m.Home.Winner && !m.Away.Winner {
		teamStyle = t.teamLose
	} else if m.State.Finished() && (m.Home.Winner || m.Away.Winner) {
		// The winning side keeps full contrast, the loser dims.
		teamStyle = t.teamWin
	}

	homeStyle, awayStyle := teamStyle, teamStyle
	if !selected && m.State.Finished() && !m.Home.Winner && m.Away.Winner {
		homeStyle = t.teamLose
	}
	if !selected && m.State.Finished() && m.Home.Winner && !m.Away.Winner {
		awayStyle = t.teamLose
	}

	row := starStyle.Render(star) + " " +
		statusStyle.Render(fit(trunc(status, stW), stW)) + " " +
		padLeft(homeStyle.Render(trunc(m.Home.Label(a.width >= 130), teamW)), teamW) + " " +
		scoreStyle.Render(fit(trunc(mid, scoreW-1), scoreW-1)) + " " +
		awayStyle.Render(fit(trunc(m.Away.Label(a.width >= 130), teamW), teamW)) + " " +
		metaStyle.Render(trunc(meta, a.rowMetaWidth(teamW, listW)))

	if selected {
		return t.listSel.Render(row)
	}
	return row
}

// rowMetaWidth is the space left for the league and venue column. The fixed
// columns are star(1)+gap+status(7)+gap+home(teamW)+gap+score(8)+gap+
// away(teamW)+gap, which is 22 + 2*teamW cells.
func (a *App) rowMetaWidth(teamW, listW int) int {
	return maxInt(8, listW-(22+2*teamW))
}

// emptyList explains why there is nothing to show and how to change that.
func (a *App) emptyList(w, h int) string {
	var title, hint string
	switch a.tab {
	case tabLive:
		title, hint = "No live matches", "Kick-off times are shown on the Upcoming tab"
	case tabUpcoming:
		title, hint = "No upcoming fixtures in this window", "Press ] to look further ahead, [ to come back"
	case tabResults:
		title, hint = "No results in this window", "Press [ to look further back, ] to come forward"
	case tabStarred:
		title, hint = "Nothing starred yet", "Press s on a match to follow it here"
	}
	if a.filter != "" {
		title = "No matches match \"" + a.filter + "\""
		hint = "Press esc to clear the filter"
	}
	if a.loading {
		hint = "Waiting for the first update from " + a.active.Name() + "…"
	}

	body := a.theme.empty.Render(title) + "\n\n" + a.theme.faintText.Render(hint)
	box := a.theme.panelBox.Width(minInt(w, 64) - 4).Render(body)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

// ---- match page -----------------------------------------------------------

// matchPageView renders the full-screen fixture view: a scoreboard pinned to
// the top, with the timeline, statistics and team sheets scrolling under it.
func (a *App) matchPageView() string {
	t := a.theme
	m, ok := a.openMatch()
	if !ok {
		return t.empty.Render("Select a match to see details")
	}

	head := a.scoreHeader(m)
	foot := a.matchPageFooter()
	// The footer is always two lines; the body takes everything else.
	avail := a.height - lipgloss.Height(head) - 2
	if avail < 3 {
		avail = 3
	}

	lines := a.matchBody(m)
	a.detailTotal = len(lines)
	if a.detailScroll > len(lines)-avail {
		a.detailScroll = maxInt(0, len(lines)-avail)
	}
	if a.detailScroll < 0 {
		a.detailScroll = 0
	}

	end := minInt(a.detailScroll+avail, len(lines))
	visible := make([]string, 0, avail)
	for i := a.detailScroll; i < end; i++ {
		visible = append(visible, lines[i])
	}
	// Record what is actually on screen so the footer can report it.
	a.detailFrom, a.detailTo = 0, 0
	if end > a.detailScroll {
		a.detailFrom, a.detailTo = a.detailScroll+1, end
	}
	for len(visible) < avail {
		visible = append(visible, "")
	}

	// A thin scrollbar on the right edge when there is more below the fold.
	body := strings.Join(visible, "\n")
	if len(lines) > avail {
		body = a.attachScrollbar(body, len(lines), avail)
	}
	// The footer is rendered after the body so it can report the real window.
	foot = a.matchPageFooter()
	return lipgloss.JoinVertical(lipgloss.Left, head, body, foot)
}

// attachScrollbar draws a one cell column on the right showing how far down the
// visible window sits in the document.
func (a *App) attachScrollbar(body string, total, avail int) string {
	rows := strings.Split(body, "\n")
	thumbH := maxInt(1, avail*avail/total)
	top := 0
	if total-avail > 0 {
		top = a.detailScroll * (avail - thumbH) / (total - avail)
	}
	bar := make([]string, len(rows))
	for i := range bar {
		bar[i] = " "
	}
	for i := 0; i < thumbH && top+i < len(bar); i++ {
		bar[top+i] = a.theme.barFill.Render(" ")
	}
	for i, row := range rows {
		rows[i] = trunc(row, a.width-1) + bar[i]
	}
	return strings.Join(rows, "\n")
}

// scoreHeader is the pinned scoreboard: team names on the outer edges, the
// scoreline in the middle, and the competition plus state above and below.
func (a *App) scoreHeader(m model.Match) string {
	t := a.theme
	w := a.width
	sideW := maxInt(8, (w-24)/2)
	scoreW := 24

	homeStyle, awayStyle := t.team, t.team
	if m.State.Finished() {
		// The winning side keeps full contrast, the loser dims.
		switch {
		case m.Home.Winner && !m.Away.Winner:
			homeStyle = t.teamWin
		case m.Away.Winner && !m.Home.Winner:
			awayStyle = t.teamWin
		default:
			homeStyle, awayStyle = t.teamLose, t.teamLose
		}
	} else if m.State.Upcoming() {
		homeStyle, awayStyle = t.team, t.team
	}

	score := m.ScoreLine()
	switch m.State {
	case model.StateScheduled, model.StatePostponed:
		score = m.KickoffLabel("15:04")
	}

	// Left half is right-aligned toward the score, right half is left-aligned.
	left := padLeft(homeStyle.Render(trunc(m.Home.Label(true), sideW)), sideW)
	right := padRight(awayStyle.Render(trunc(m.Away.Label(true), sideW)), sideW)
	mid := t.score.Width(scoreW).Align(lipgloss.Center).Render(score)

	lines := []string{
		t.panelTitle.Align(lipgloss.Center).Width(w).
			Render(trunc(joinNonEmpty(" · ", m.League, m.Round), w)),
		lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right),
		a.statusFor(m).Align(lipgloss.Center).Width(w).Render(trunc(a.detailStatusLine(m), w)),
	}

	if m.State.Finished() && (m.Home.ScoreHT != 0 || m.Away.ScoreHT != 0) {
		lines = append(lines, t.faintText.Align(lipgloss.Center).Width(w).
			Render(fmt.Sprintf("half time %d-%d", m.Home.ScoreHT, m.Away.ScoreHT)))
	}
	if venue := joinNonEmpty(" · ", m.Venue, m.City); venue != "" {
		lines = append(lines, t.faintText.Align(lipgloss.Center).Width(w).Render(trunc(venue, w)))
	}
	if len(m.Broadcast) > 0 {
		lines = append(lines, t.faintText.Align(lipgloss.Center).Width(w).
			Render(trunc(strings.Join(m.Broadcast, " · "), w)))
	}
	if starred := a.stars[m.Key()]; starred {
		lines = append(lines, t.star.Align(lipgloss.Center).Width(w).Render("* starred"))
	}
	lines = append(lines, t.dimmed.Align(lipgloss.Center).Width(w).Render(strings.Repeat("─", maxInt(0, w))))
	return strings.Join(lines, "\n")
}

// statusFor picks the lifecycle style for the match page header.
func (a *App) statusFor(m model.Match) lipgloss.Style {
	switch {
	case m.State.Live():
		return a.theme.statusLive
	case m.State.Upcoming():
		return a.theme.statusRest
	default:
		return a.theme.faintText
	}
}

// matchBody builds the scrollable document: timeline, statistics, team sheets.
func (a *App) matchBody(m model.Match) []string {
	t := a.theme
	w := a.width
	var lines []string

	// Fixtures that have not kicked off have no timeline or statistics yet;
	// lead with what there is rather than an empty block.
	if m.State != model.StateScheduled {
		lines = append(lines, t.group.Render("Timeline"))
		if len(m.Events) == 0 {
			lines = append(lines, t.faintText.Render("  No goals or cards reported yet."))
		}
		for _, e := range m.Events {
			lines = append(lines, a.timelineLine(e, m, w))
		}
		lines = append(lines, "")

		lines = append(lines, t.group.Render("Statistics"))
		if len(m.Stats) == 0 {
			lines = append(lines, t.faintText.Render("  No statistics for this fixture."))
		}
		for _, s := range m.Stats {
			lines = append(lines, a.statLine(s, w))
		}
		lines = append(lines, "")
	}

	lines = append(lines, t.group.Render("Team sheets"))
	lines = append(lines, a.lineupLines(m, w)...)
	return lines
}

// lineupLines renders both team sheets, side by side on wide terminals and
// stacked on narrow ones.
func (a *App) lineupLines(m model.Match, w int) []string {
	t := a.theme

	home, homeOK := a.lineupFor(m, m.Home)
	away, awayOK := a.lineupFor(m, m.Away)

	switch {
	case !homeOK && !awayOK:
		return a.lineupEmpty(m, w)
	case !awayOK:
		return a.oneLineup(home, w)
	case !homeOK:
		return a.oneLineup(away, w)
	}

	if w >= 96 {
		colW := (w - 3) / 2
		left := a.oneLineup(home, colW)
		right := a.oneLineup(away, colW)
		n := maxInt(len(left), len(right))
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			out = append(out, trunc(l, colW)+t.dimmed.Render(" │ ")+trunc(r, colW))
		}
		return out
	}
	out := a.oneLineup(home, w)
	out = append(out, "")
	return append(out, a.oneLineup(away, w)...)
}

// lineupFor resolves a side's team sheet, falling back to an empty one with a
// flag so callers can tell "not loaded" from "no sheet".
func (a *App) lineupFor(m model.Match, team model.Team) (model.Lineup, bool) {
	if l, ok := m.LineupFor(team.ID); ok {
		return l, len(l.Starters) > 0
	}
	if l, ok := a.lineups[m.Key()]; ok {
		for _, c := range l {
			if c.TeamID == team.ID {
				return c, len(c.Starters) > 0
			}
		}
	}
	return model.Lineup{}, false
}

// lineupEmpty explains why there are no team sheets and whether a retry helps.
func (a *App) lineupEmpty(m model.Match, w int) []string {
	t := a.theme
	d, hasDetailer := provider.AsDetailer(a.active)
	switch {
	case !hasDetailer:
		return []string{t.faintText.Render("  " + a.active.Name() + " does not provide team sheets.")}
	case a.detailLoading:
		return []string{t.faintText.Render("  Loading " + d.DetailName() + "…")}
	case a.detailErr != nil:
		return []string{t.warn.Render("  " + trunc("Could not load "+d.DetailName()+": "+a.detailErr.Error(), w-2))}
	default:
		return []string{t.faintText.Render("  Team sheets were not published for this fixture.")}
	}
}

// oneLineup renders one team's sheet inside a column of the given width.
func (a *App) oneLineup(l model.Lineup, w int) []string {
	t := a.theme
	var out []string

	title := l.TeamName
	if l.Formation != "" {
		title += "  " + l.Formation
	}
	out = append(out, t.panelTitle.Render(trunc(title, w)))

	for _, p := range l.Starters {
		out = append(out, a.playerLine(p, w))
	}
	if len(l.Bench) > 0 {
		out = append(out, t.faintText.Render(trunc("Bench", w)))
		for _, p := range l.Bench {
			out = append(out, a.playerLine(p, w))
		}
	}
	return out
}

// playerLine renders one squad member: number, name, position, and any
// in-match substitution they were involved in.
func (a *App) playerLine(p model.Player, w int) string {
	t := a.theme
	const numW, posW = 4, 5
	nameW := maxInt(6, w-numW-posW-5)

	mark := "  "
	switch {
	case p.SubIn && p.SubOut:
		mark = t.warn.Render(" ↔ ")
	case p.SubIn:
		mark = t.ok.Render(" ↑ ")
	case p.SubOut:
		mark = t.faintText.Render(" ↓ ")
	}

	return " " + t.footerKey.Render(padLeft(trunc(p.Number, numW), numW)) + " " +
		mark + " " + t.team.Render(padRight(trunc(p.Name, nameW), nameW)) + " " +
		t.faintText.Render(padRight(trunc(p.Pos, posW), posW))
}

// detailStatusLine summarises state, minute and kickoff.
func (a *App) detailStatusLine(m model.Match) string {
	switch m.State {
	case model.StateLive:
		return fmt.Sprintf("Live · %d' · started %s", m.Minute, m.Kickoff.Format("15:04"))
	case model.StateHalftime:
		return "Half Time · " + m.Kickoff.Format("15:04")
	case model.StateFinished:
		label := "Full Time"
		if m.StatusText != "" && m.StatusText != "FT" {
			label = "Full Time · " + m.StatusText
		}
		return label + " · " + m.Kickoff.Format("Mon 2 Jan 15:04")
	case model.StatePostponed:
		return "Postponed · was " + m.Kickoff.Format("Mon 2 Jan 15:04")
	case model.StateSuspended:
		return "Suspended · " + m.Kickoff.Format("Mon 2 Jan 15:04")
	default:
		return "Kick-off " + m.Kickoff.Format("Mon 2 Jan 15:04")
	}
}

// timelineLine renders one goal or card entry, attributed to the side that
// committed it.
func (a *App) timelineLine(e model.Event, m model.Match, w int) string {
	t := a.theme
	const minW, kindW, scoreW = 5, 9, 5
	sideW := 6
	playerW := maxInt(6, w-minW-kindW-scoreW-sideW-6)

	detail := e.Kind.Label()
	if e.Kind == model.EventGoal && e.Penalty {
		detail = "PEN"
	} else if e.Kind == model.EventGoal && e.OwnGoal {
		detail = "OWN"
	}

	style := t.tlGoal
	if !e.Kind.Scoring() {
		style = t.tlCard
	}

	side := " "
	switch e.TeamID {
	case m.Home.ID:
		side = "H"
	case m.Away.ID:
		side = "A"
	}

	return " " + t.tlMinute.Render(fit(trunc(e.Minute, minW), minW)) + " " +
		t.tlKind.Render(fit(trunc(detail, kindW), kindW)) + " " +
		t.dimmed.Render(fit(trunc(side, sideW), sideW)) + " " +
		style.Render(padRight(trunc(e.Player, playerW), playerW)) + " " +
		t.tlScore.Render(padLeft(trunc(e.Score, scoreW), scoreW))
}

// statLine renders one statistic with a proportional bar.
func (a *App) statLine(s model.Stat, w int) string {
	t := a.theme
	const labelW, valW = 18, 6
	barW := w - labelW - 2*valW - 4

	label := fit(trunc(s.Label, labelW), labelW)
	home := padLeft(trunc(s.Home, valW), valW)
	away := padLeft(trunc(s.Away, valW), valW)

	line := label + " " + home + " "
	if barW > 6 {
		total := s.HomeVal + s.AwayVal
		ratio := 0.5
		if total > 0 {
			ratio = s.HomeVal / total
		}
		line += t.bar(ratio, barW) + " "
	}
	return line + away
}

// openMatch resolves the fixture the match page is showing. It is looked up by
// key rather than by cursor position, so a background refresh that reshuffles
// the list cannot silently swap the page onto a different match.
func (a *App) openMatch() (model.Match, bool) {
	if a.detailKey != "" {
		for _, m := range a.list {
			if m.Key() == a.detailKey {
				return m, true
			}
		}
		// It left the visible list (state changed, filter, window shift): fall
		// back to whatever is loaded under the cursor rather than blanking out.
		for _, m := range a.matches {
			if m.Key() == a.detailKey {
				return m, true
			}
		}
	}
	if a.cursor >= 0 && a.cursor < len(a.list) {
		return a.list[a.cursor], true
	}
	return model.Match{}, false
}

// matchPageFooter is the key hint line drawn under the match page. Hints that
// do not fit the terminal are dropped whole rather than cut mid word.
func (a *App) matchPageFooter() string {
	t := a.theme
	rows := []hint{
		{"esc", "back"}, {"j k", "scroll"}, {"ctrl+d u", "half page"},
		{"g G", "top bottom"}, {"s", "star"}, {"r", "refresh"}, {"q", "quit"},
	}
	parts := make([]string, 0, len(rows)*2)
	line1 := ""
	for i, h := range rows {
		candidate := h.k + " " + h.d
		if i > 0 {
			candidate = "  " + candidate
		}
		if width(line1)+width(candidate) > a.width {
			break
		}
		if i > 0 {
			parts = append(parts, t.footerDim.Render("  "))
		}
		parts = append(parts, t.footerKey.Render(h.k), t.footer.Render(" "+h.d))
		line1 += candidate
	}
	line1 = trunc(strings.Join(parts, ""), a.width)

	prog := ""
	if a.detailTotal > 0 && a.detailTo > 0 {
		prog = fmt.Sprintf("  line %d-%d of %d", a.detailFrom, a.detailTo, a.detailTotal)
	}
	line2 := trunc(a.theme.footerDim.Render(strings.TrimSpace(
		a.active.Name()+" · "+a.contextStatus()+prog)), a.width)
	return lipgloss.JoinVertical(lipgloss.Left, line1, line2)
}

// contextStatus is the short "what am I looking at" line used on the page.
func (a *App) contextStatus() string {
	if a.detailLoading {
		return "loading team sheets"
	}
	if a.detailErr != nil {
		return "team sheets failed to load"
	}
	return a.detailStatusOf()
}

// detailStatusOf reports the state of the fixture the match page is showing.
func (a *App) detailStatusOf() string {
	m, ok := a.openMatch()
	if !ok {
		return ""
	}
	return m.StatusText
}

// ---- overlays -------------------------------------------------------------

// providerMenuView lists the feeds and how to authenticate with each.
func (a *App) providerMenuView() string {
	w := a.panelWidth()
	var b strings.Builder
	b.WriteString(a.theme.panelTitle.Render("Score provider") + "\n\n")

	page := a.menuPageSize()
	a.ensureMenuVisible(len(a.providers))
	for i, p := range a.providers {
		if i < a.menuOffset || i >= a.menuOffset+page {
			continue
		}
		state := "no key needed"
		if p.NeedsKey() {
			if p.KeyPresent() {
				state = "key set"
			} else {
				state = "key required"
			}
		}
		if p.ID() == a.active.ID() {
			state = "active · " + state
		}
		name := padRight(trunc(p.Name(), 20), 20)
		line := "  " + name + a.theme.footerDim.Render(state)
		if i == a.menuCursor {
			line = a.theme.listSel.Render("› " + name + a.theme.dimmed.Render(state))
		}
		b.WriteString(line + "\n")
	}

	if a.menuCursor >= 0 && a.menuCursor < len(a.providers) {
		p := a.providers[a.menuCursor]
		b.WriteString("\n" + a.theme.faintText.Render(wrap(p.Description(), w-6)))
	}
	b.WriteString("\n" + a.theme.footer.Render("↑↓ move · enter switch · esc close"))
	return a.theme.panelBox.Width(w - 2).Render(b.String())
}

// leagueMenuView toggles which competitions are loaded.
func (a *App) leagueMenuView() string {
	w := a.panelWidth()
	var b strings.Builder
	b.WriteString(a.theme.panelTitle.Render("Competitions · "+a.active.Name()) + "\n\n")

	if len(a.leagues) == 0 {
		b.WriteString(a.theme.faintText.Render("Loading competitions…"))
	} else {
		page := a.menuPageSize() - 1
		a.ensureMenuVisible(len(a.leagues))
		for i, l := range a.leagues {
			if i < a.menuOffset || i >= a.menuOffset+page {
				continue
			}
			mark := "[ ]"
			if a.pending[l.ID] {
				mark = "[x]"
			}
			cursor := "  "
			if i == a.menuCursor {
				cursor = "› "
			}
			line := fmt.Sprintf("%s%s %s", cursor, mark, trunc(l.Name, w-14))
			if i == a.menuCursor {
				line = a.theme.listSel.Render(line)
			}
			b.WriteString(line + "\n")
		}
		selected := 0
		for _, on := range a.pending {
			if on {
				selected++
			}
		}
		b.WriteString("\n" + a.theme.footerDim.Render(fmt.Sprintf("%d selected of %d", selected, len(a.leagues))))
	}
	b.WriteString("\n" + a.theme.footer.Render("↑↓ move · space toggle · a all · n none · enter apply · esc cancel"))
	return a.theme.panelBox.Width(w - 2).Render(b.String())
}

// helpView lists the key bindings.
func (a *App) helpView() string {
	w := a.panelWidth()
	rows := [][2]string{
		{"j k  ↑ ↓", "move the selection"},
		{"h l  ← →", "switch between Live, Upcoming, Results, Starred"},
		{"1-4", "jump straight to a tab"},
		{"⏎  i", "open the full-screen match page"},
		{"g  gg  G", "top and bottom of the list or match page"},
		{"ctrl+f b", "page down and up"},
		{"ctrl+d u", "half page down and up"},
		{"pgup pgdn", "page the list or match page"},
		{"[  ]", "shift the upcoming/results window by a few days"},
		{"/", "filter by team, competition or venue"},
		{"s", "star or unstar the selected match"},
		{"r", "refresh now"},
		{"p", "switch score provider (k sets its key)"},
		{"L", "choose which competitions to load"},
		{"K", "set the API key for the current provider"},
		{"?", "show or hide this help"},
		{"q  esc", "close a page, or quit"},
	}

	var b strings.Builder
	b.WriteString(a.theme.panelTitle.Render("Keys") + "\n\n")
	for _, r := range rows {
		b.WriteString("  " + a.theme.footerKey.Render(padRight(r[0], 18)) + a.theme.footer.Render(r[1]) + "\n")
	}
	b.WriteString("\n" + a.theme.footer.Render("esc close"))
	return a.theme.panelBox.Width(w - 2).Render(b.String())
}

// keyPromptView collects an API key for the key-backed provider.
func (a *App) keyPromptView() string {
	w := a.panelWidth()
	name := a.keyFor
	for _, p := range a.providers {
		if p.ID() == a.keyFor {
			name = p.Name()
		}
	}

	var b strings.Builder
	b.WriteString(a.theme.panelTitle.Render("API key · "+name) + "\n\n")
	b.WriteString(a.theme.faintText.Render(wrap(
		"Create a free key at dashboard.api-sports.io, then paste it here. It is stored in your config file and used only for this feed.",
		w-6)) + "\n\n")
	b.WriteString("  " + a.keyInput.View() + "\n\n")
	b.WriteString(a.theme.footer.Render("enter save · esc cancel"))
	return a.theme.panelBox.Width(w - 2).Render(b.String())
}

// ---- footer ---------------------------------------------------------------

// hint is one key binding shown in the footer.
type hint struct{ k, d string }

func (a *App) hints() []hint {
	switch {
	case a.filterMode:
		return []hint{{"enter", "apply"}, {"esc", "clear filter"}}
	case a.menu == menuProvider:
		return []hint{{"↑↓", "move"}, {"enter", "switch"}, {"esc", "close"}}
	case a.menu == menuLeagues:
		return []hint{{"space", "toggle"}, {"enter", "apply"}, {"esc", "cancel"}}
	case a.menu == menuKey:
		return []hint{{"enter", "save"}, {"esc", "cancel"}}
	default:
		return []hint{
			{"j k", "move"}, {"⏎", "match page"}, {"/", "filter"}, {"s", "star"},
			{"h l", "tabs"}, {"[ ]", "window"}, {"p", "provider"}, {"L", "leagues"},
			{"r", "refresh"}, {"?", "help"}, {"q", "quit"},
		}
	}
}

// footerView draws the key hints plus a contextual status line.
func (a *App) footerView() string {
	t := a.theme
	parts := make([]string, 0, len(a.hints())*2)
	for i, h := range a.hints() {
		if i > 0 {
			parts = append(parts, t.footerDim.Render("  "))
		}
		parts = append(parts, t.footerKey.Render(h.k), t.footer.Render(" "+h.d))
	}
	line1 := strings.Join(parts, "")

	line2 := a.theme.footerDim.Render(trunc(a.contextLine(), a.width))
	return lipgloss.JoinVertical(lipgloss.Left, trunc(line1, a.width), line2)
}

// contextLine summarises what is loaded and when it was last refreshed.
func (a *App) contextLine() string {
	base := fmt.Sprintf("%s · %d match(es)", a.active.Name(), len(a.list))
	if !a.lastFetch.IsZero() {
		base += " · updated " + humanDuration(time.Since(a.lastFetch)) + " ago"
	} else if a.loading {
		base += " · loading"
	}
	if a.filter != "" {
		base += " · filter \"" + a.filter + "\""
	}
	return base
}

// ---- small helpers --------------------------------------------------------

// humanDuration renders an age or interval in the coarsest useful unit.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

// wrap breaks text into lines no wider than w cells.
func wrap(s string, w int) string {
	if w <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var (
		out  []string
		line strings.Builder
	)
	for _, word := range words {
		if line.Len() == 0 {
			line.WriteString(word)
			continue
		}
		if width(line.String())+1+width(word) <= w {
			line.WriteString(" ")
			line.WriteString(word)
			continue
		}
		out = append(out, line.String())
		line.Reset()
		line.WriteString(word)
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	return strings.Join(out, "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
