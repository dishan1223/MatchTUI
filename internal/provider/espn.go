package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"matchtui/internal/model"
)

// espnBase is the root of ESPN's public scoreboard API. It needs no key, no
// registration and covers most professional competitions worldwide.
const espnBase = "https://site.api.espn.com/apis/site/v2/sports/soccer"

// espnConcurrency caps parallel requests: ESPN serves one day of one league per
// call, so a multi-day window across several leagues needs a small pool.
const espnConcurrency = 6

// espnCatalogue is the curated league list offered in the league menu. Every
// slug has been verified against the live API.
var espnCatalogue = []League{
	{ID: "eng.1", Name: "Premier League (England)", Default: true},
	{ID: "esp.1", Name: "LaLiga (Spain)", Default: true},
	{ID: "ita.1", Name: "Serie A (Italy)", Default: true},
	{ID: "ger.1", Name: "Bundesliga (Germany)", Default: true},
	{ID: "fra.1", Name: "Ligue 1 (France)", Default: true},
	{ID: "uefa.champions", Name: "UEFA Champions League", Default: true},
	{ID: "uefa.europa", Name: "UEFA Europa League"},
	{ID: "uefa.europa.conf", Name: "UEFA Conference League"},
	{ID: "uefa.nations", Name: "UEFA Nations League"},
	{ID: "ned.1", Name: "Eredivisie (Netherlands)"},
	{ID: "por.1", Name: "Primeira Liga (Portugal)"},
	{ID: "bra.1", Name: "Serie A (Brazil)"},
	{ID: "arg.1", Name: "Liga Profesional (Argentina)"},
	{ID: "mex.1", Name: "Liga MX (Mexico)"},
	{ID: "usa.1", Name: "MLS (United States)"},
	{ID: "usa.nwsl", Name: "NWSL (United States, women)"},
	{ID: "eng.2", Name: "Championship (England)"},
	{ID: "sco.1", Name: "Premiership (Scotland)"},
	{ID: "tur.1", Name: "Super Lig (Turkiye)"},
	{ID: "bel.1", Name: "Pro League (Belgium)"},
	{ID: "gre.1", Name: "Super League (Greece)"},
	{ID: "aut.1", Name: "Bundesliga (Austria)"},
	{ID: "sui.1", Name: "Super League (Switzerland)"},
	{ID: "den.1", Name: "Superliga (Denmark)"},
	{ID: "nor.1", Name: "Eliteserien (Norway)"},
	{ID: "swe.1", Name: "Allsvenskan (Sweden)"},
	{ID: "jpn.1", Name: "J1 League (Japan)"},
	{ID: "chn.1", Name: "Chinese Super League"},
	{ID: "fifa.world", Name: "FIFA World Cup"},
}

// espnProvider reads ESPN's public scoreboard.
type espnProvider struct{}

// NewESPN builds the default, key-free provider.
func NewESPN() Provider { return &espnProvider{} }

func (p *espnProvider) ID() string   { return "espn" }
func (p *espnProvider) Name() string { return "ESPN" }
func (p *espnProvider) Description() string {
	return "Live and historical scores from ESPN's public scoreboard API. No key, no sign-up."
}
func (p *espnProvider) NeedsKey() bool          { return false }
func (p *espnProvider) KeyPresent() bool        { return true }
func (p *espnProvider) RateHint() time.Duration { return 0 }

func (p *espnProvider) Leagues(ctx context.Context) ([]League, error) {
	out := make([]League, len(espnCatalogue))
	copy(out, espnCatalogue)
	SortLeagues(out)
	return out, nil
}

// Fetch requests every (league, day) pair in the window concurrently.
func (p *espnProvider) Fetch(ctx context.Context, req Request) ([]model.Match, error) {
	leagues := req.Leagues
	if len(leagues) == 0 {
		leagues = DefaultLeagueIDs(espnCatalogue)
	}
	days := req.Days()
	if len(days) == 0 {
		return nil, nil
	}

	type job struct{ league, day string }
	var jobs []job
	for _, l := range leagues {
		for _, d := range days {
			jobs = append(jobs, job{league: l, day: d.Format("20060102")})
		}
	}

	var (
		mu       sync.Mutex
		out      []model.Match
		firstErr error
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, espnConcurrency)

	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			ms, err := p.fetchDay(ctx, j.league, j.day)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			out = append(out, ms...)
		}(j)
	}
	wg.Wait()

	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	model.ByKickoff(out)
	return out, nil
}

// fetchDay pulls one league on one date and maps it into the shared model.
func (p *espnProvider) fetchDay(ctx context.Context, slug, day string) ([]model.Match, error) {
	url := fmt.Sprintf("%s/%s/scoreboard?dates=%s", espnBase, slug, day)
	var root espnRoot
	if err := getJSON(ctx, url, nil, &root); err != nil {
		return nil, err
	}

	leagueName, leagueTag := slug, strings.ToUpper(slug)
	if len(root.Leagues) > 0 {
		leagueName = root.Leagues[0].Name
		if ab := strings.TrimSpace(root.Leagues[0].Abbreviation); ab != "" {
			leagueTag = ab
		}
	}

	out := make([]model.Match, 0, len(root.Events))
	for _, ev := range root.Events {
		if m, ok := p.mapEvent(ev, slug, leagueName, leagueTag); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// mapEvent converts one ESPN event, returning false when the payload has no
// usable competition block.
func (p *espnProvider) mapEvent(ev espnEvent, slug, leagueName, leagueTag string) (model.Match, bool) {
	if len(ev.Competitions) == 0 {
		return model.Match{}, false
	}
	comp := ev.Competitions[0]

	m := model.Match{
		ID:        "espn-" + ev.ID,
		Provider:  p.ID(),
		LeagueID:  slug,
		League:    leagueName,
		LeagueTag: leagueTag,
		Round:     comp.Season.Type.Name,
		Kickoff:   ev.Date.Local(),
		Venue:     comp.Venue.FullName,
		City:      comp.Venue.Address.City,
		UpdatedAt: time.Now(),
	}
	if m.Venue == "" {
		m.Venue = ev.Venue.FullName
	}
	if m.City == "" {
		m.City = ev.Venue.Address.City
	}

	for _, c := range comp.Competitors {
		t := model.Team{
			ID:    c.Team.ID,
			Name:  c.Team.DisplayName,
			Short: c.Team.ShortDisplayName,
			Abbr:  c.Team.Abbreviation,
			Logo:  c.Team.Logo,
			Form:  c.Form,
		}
		if n, err := strconv.Atoi(strings.TrimSpace(string(c.Score))); err == nil {
			t.Score = n
		}
		t.Winner = c.Winner
		if c.HomeAway == "home" {
			m.Home = t
		} else {
			m.Away = t
		}
	}
	if m.Home.Name == "" && m.Away.Name == "" {
		return model.Match{}, false
	}

	m.State, m.Minute, m.StatusText = espnState(ev.Status, comp.Status)
	m.Events, m.Home.ScoreHT, m.Away.ScoreHT = espnTimeline(comp.Details, m.Home.ID, m.Away.ID)
	m.Stats = espnStats(comp.Stats)
	m.Broadcast = espnBroadcasts(comp.Broadcasts)
	return m, true
}

// espnState normalises an ESPN status into the shared lifecycle states.
//
// ESPN splits a fixture's status in two: the event status carries the lifecycle
// state and the running clock, while the competition status carries the human
// readable detail ("FT", "45'", "Full Time"). Both have to be merged, because
// each is missing fields the other carries.
func espnState(ev espnStatus, comp espnStatus) (model.State, int, string) {
	// The lifecycle fields live in status.type. The event status carries the
	// running clock; the competition status repeats the type block, so prefer
	// whichever of the two actually has a value.
	et, ct := ev.Type, comp.Type

	name, shortDetail, detail := et.Name, et.ShortDetail, et.Detail
	state := et.State
	completed := et.Completed
	if ct.Name != "" {
		name = ct.Name
	}
	if ct.ShortDetail != "" {
		shortDetail = ct.ShortDetail
	}
	if ct.Detail != "" {
		detail = ct.Detail
	}
	if ct.State != "" {
		state = ct.State
	}
	if ct.Completed {
		completed = true
	}

	shown := strings.TrimSpace(shortDetail)
	if shown == "" {
		shown = strings.TrimSpace(detail)
	}
	blob := strings.ToUpper(name + " " + shown)

	finished := state == "post" || completed ||
		strings.Contains(blob, "FULL TIME") ||
		strings.EqualFold(shown, "FT") ||
		strings.Contains(blob, "EXTRA TIME") ||
		strings.Contains(blob, "PENALT")

	clock := ev.Clock.Value
	if comp.Clock.Value > clock {
		clock = comp.Clock.Value
	}

	switch {
	case strings.Contains(blob, "HALFTIME") || strings.EqualFold(shown, "HT"):
		return model.StateHalftime, 45, "HT"
	case strings.Contains(blob, "POSTPONED"):
		return model.StatePostponed, 0, "PP"
	case strings.Contains(blob, "SUSPEND") || strings.Contains(blob, "ABANDON") || strings.Contains(blob, "CANC"):
		return model.StateSuspended, 0, "SUSP"
	case state == "in":
		mins := int(clock/60) + 1
		if mins < 1 {
			mins = 1
		}
		return model.StateLive, mins, fmt.Sprintf("%d'", mins)
	case finished:
		label := "FT"
		if shown != "" && !strings.EqualFold(shown, "full time") {
			label = truncateStatus(shown)
		}
		return model.StateFinished, 0, label
	default:
		return model.StateScheduled, 0, truncateStatus(shown)
	}
}

// truncateStatus keeps the status column at a readable width.
func truncateStatus(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 8 {
		return s[:8]
	}
	if s == "" {
		return "-"
	}
	return s
}

// espnTimeline maps ESPN's play-by-play into goals and cards, and computes the
// half-time scoreline while it is at it.
func espnTimeline(details []espnDetail, homeID, awayID string) ([]model.Event, int, int) {
	if len(details) == 0 {
		return nil, 0, 0
	}

	var (
		events     []model.Event
		home, away int
	)
	for _, d := range details {
		if d.Shootout {
			continue
		}
		kind := espnEventKind(d)
		if kind == "" {
			continue
		}
		e := model.Event{
			Kind:    kind,
			TeamID:  d.Team.ID,
			Penalty: d.PenaltyKick,
			OwnGoal: d.OwnGoal,
		}
		if d.Clock.Value > 0 {
			e.Minute = fmt.Sprintf("%d'", int(d.Clock.Value/60)+1)
		} else {
			e.Minute = "-"
		}
		if len(d.AthletesInvolved) > 0 {
			e.Player = d.AthletesInvolved[0].DisplayName
		}
		if kind.Scoring() {
			// ESPN attributes the play to the benefiting side already, so the
			// running score simply follows the credited team.
			if d.Team.ID == homeID {
				home += d.scoreValue()
			} else {
				away += d.scoreValue()
			}
			e.Score = fmt.Sprintf("%d-%d", home, away)
		}
		events = append(events, e)
	}

	// Half-time score: goals timed inside the first 45 minutes plus stoppage.
	htHome, htAway := 0, 0
	for _, d := range details {
		if d.Shootout || !d.ScoringPlay || d.Clock.Value > 2700 {
			continue
		}
		if d.Team.ID == homeID {
			htHome += d.scoreValue()
		} else {
			htAway += d.scoreValue()
		}
	}
	return events, htHome, htAway
}

// espnEventKind maps an ESPN play to a timeline kind, or "" to skip it.
func espnEventKind(d espnDetail) model.EventKind {
	switch {
	case d.ScoringPlay && d.OwnGoal:
		return model.EventOwnGoal
	case d.ScoringPlay && d.PenaltyKick:
		return model.EventPen
	case d.ScoringPlay:
		return model.EventGoal
	case d.RedCard:
		return model.EventRed
	case d.YellowCard:
		return model.EventYellow
	}
	switch strings.ToLower(strings.TrimSpace(d.Type.Text)) {
	case "substition", "substitution":
		return model.EventSub
	case "var":
		return model.EventVar
	}
	return ""
}

// scoreValue is how many goals the play added, defaulting to one.
func (d espnDetail) scoreValue() int {
	if d.ScoreValue > 0 {
		return d.ScoreValue
	}
	return 1
}

// espnStatLabels maps ESPN stat keys onto the labels shown in the detail panel.
var espnStatLabels = map[string]string{
	"possessionPct":     "Possession",
	"shotsTotal":        "Shots",
	"shotsOnTarget":     "Shots on target",
	"fouls":             "Fouls",
	"corners":           "Corners",
	"offsides":          "Offsides",
	"yellowCards":       "Yellow cards",
	"redCards":          "Red cards",
	"passes":            "Passes",
	"passAccuracyPct":   "Pass accuracy",
	"saves":             "Saves",
	"expectedGoals":     "Expected goals",
	"wonCorners":        "Corners",
	"totalXgoal":        "Expected goals",
	"possessionTimeout": "Possession",
}

// espnStats keeps only the statistics worth showing, in a fixed order.
func espnStats(stats []espnStat) []model.Stat {
	if len(stats) == 0 {
		return nil
	}
	order := []string{
		"possessionPct", "shotsTotal", "shotsOnTarget", "expectedGoals",
		"passes", "passAccuracyPct", "corners", "offsides", "fouls",
		"yellowCards", "redCards", "saves",
	}
	byName := make(map[string]espnStat, len(stats))
	for _, s := range stats {
		byName[s.Name] = s
	}

	var out []model.Stat
	for _, name := range order {
		s, ok := byName[name]
		if !ok {
			continue
		}
		st := model.Stat{
			Label:   espnStatLabels[name],
			HomeVal: s.HomeValue,
			AwayVal: s.AwayValue,
			Home:    strings.TrimSpace(s.DisplayValue),
			Away:    "",
		}
		// ESPN puts a combined display value on the stat itself, so split it
		// when per-team numbers are missing.
		if strings.Contains(st.Home, "-") {
			parts := strings.SplitN(st.Home, "-", 2)
			st.Home, st.Away = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
		if st.Home == "" {
			st.Home = formatStatValue(name, s.HomeValue)
		}
		if st.Away == "" {
			st.Away = formatStatValue(name, s.AwayValue)
		}
		if st.Home == st.Away && st.HomeVal == 0 && st.AwayVal == 0 {
			continue
		}
		out = append(out, st)
	}
	return out
}

// formatStatValue renders a numeric statistic with the right suffix.
func formatStatValue(name string, v float64) string {
	if name == "possessionPct" || name == "passAccuracyPct" {
		return fmt.Sprintf("%.0f%%", v)
	}
	if name == "expectedGoals" || name == "totalXgoal" {
		return fmt.Sprintf("%.2f", v)
	}
	return trimFloat(v)
}

// espnBroadcasts collects the broadcaster names for a fixture.
func espnBroadcasts(bs []espnBroadcast) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, b := range bs {
		for _, n := range b.Names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

// FetchDetail enriches one fixture with the team sheets from ESPN's summary
// endpoint. The scoreboard payload carries no rosters, so this is a second,
// on-demand request made only when the user actually opens a match.
func (p *espnProvider) FetchDetail(ctx context.Context, m model.Match) (model.Match, error) {
	eventID, ok := strings.CutPrefix(m.ID, "espn-")
	if !ok || m.LeagueID == "" {
		return m, nil // not one of ours; return unchanged rather than guessing
	}
	url := fmt.Sprintf("%s/%s/summary?event=%s", espnBase, m.LeagueID, eventID)
	var root espnSummary
	if err := getJSON(ctx, url, nil, &root); err != nil {
		return m, err
	}
	m.Lineups = espnLineups(root.Rosters)
	return m, nil
}

func (p *espnProvider) DetailName() string { return "team sheets" }

// espnLineups maps the summary rosters into the shared lineup model, keeping
// the starting eleven apart from the bench.
func espnLineups(rs []espnRoster) []model.Lineup {
	out := make([]model.Lineup, 0, len(rs))
	for _, r := range rs {
		if r.Team.ID == "" && len(r.Roster) == 0 {
			continue
		}
		l := model.Lineup{
			TeamID:    r.Team.ID,
			TeamName:  r.Team.DisplayName,
			Formation: r.Formation,
		}
		for _, pl := range r.Roster {
			name := strings.TrimSpace(pl.Athlete.DisplayName)
			if name == "" {
				continue
			}
			p := model.Player{
				Number:  strings.TrimSpace(pl.Jersey),
				Name:    name,
				Pos:     pl.Position.Abbreviation,
				Starter: pl.Starter,
				SubIn:   pl.SubbedIn,
				SubOut:  pl.SubbedOut,
			}
			if p.Starter {
				l.Starters = append(l.Starters, p)
			} else {
				l.Bench = append(l.Bench, p)
			}
		}
		// The starting eleven is listed in formation order by ESPN, but a feed
		// change must not scramble the view, so pin the goalkeeper first and
		// keep everything else in the order it arrived.
		sort.SliceStable(l.Starters, func(i, j int) bool {
			return l.Starters[i].Pos == "G-K" && l.Starters[j].Pos != "G-K"
		})
		out = append(out, l)
	}
	return out
}

// ---- wire types -----------------------------------------------------------

type espnRoot struct {
	Leagues []espnLeagueRef `json:"leagues"`
	Events  []espnEvent     `json:"events"`
}

// espnSummary is the payload of the per-event summary endpoint, which carries
// the team sheets the scoreboard omits.
type espnSummary struct {
	Rosters []espnRoster `json:"rosters"`
}

type espnRoster struct {
	HomeAway  string        `json:"homeAway"`
	Team      espnTeam      `json:"team"`
	Formation string        `json:"formation"`
	Roster    []espnRosterP `json:"roster"`
}

type espnRosterP struct {
	Starter   bool   `json:"starter"`
	Jersey    string `json:"jersey"`
	SubbedIn  bool   `json:"subbedIn"`
	SubbedOut bool   `json:"subbedOut"`
	Athlete   struct {
		DisplayName string `json:"displayName"`
	} `json:"athlete"`
	Position struct {
		Abbreviation string `json:"abbreviation"`
	} `json:"position"`
}

type espnLeagueRef struct {
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
	Slug         string `json:"slug"`
}

type espnEvent struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Date         flexTime   `json:"date"`
	Status       espnStatus `json:"status"`
	Venue        espnVenue  `json:"venue"`
	Competitions []espnComp `json:"competitions"`
}

type espnStatus struct {
	Clock        espnClock      `json:"clock"`
	DisplayClock string         `json:"displayClock"`
	Period       int            `json:"period"`
	Type         espnStatusType `json:"type"`
}

// espnStatusType holds the lifecycle fields. ESPN nests them under "type"
// inside the status object.
type espnStatusType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Completed   bool   `json:"completed"`
	Description string `json:"description"`
	Detail      string `json:"detail"`
	ShortDetail string `json:"shortDetail"`
}

type espnComp struct {
	ID          string           `json:"id"`
	Season      espnCompSeason   `json:"season"`
	Status      espnStatus       `json:"status"`
	Venue       espnVenue        `json:"venue"`
	Competitors []espnCompetitor `json:"competitors"`
	Details     []espnDetail     `json:"details"`
	Stats       []espnStat       `json:"stats"`
	Broadcasts  []espnBroadcast  `json:"broadcasts"`
}

type espnCompSeason struct {
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
}

type espnVenue struct {
	FullName string `json:"fullName"`
	Address  struct {
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"address"`
}

type espnCompetitor struct {
	ID       string   `json:"id"`
	HomeAway string   `json:"homeAway"`
	Winner   bool     `json:"winner"`
	Score    asString `json:"score"`
	Form     string   `json:"form"`
	Team     espnTeam `json:"team"`
}

type espnTeam struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ShortDisplayName string `json:"shortDisplayName"`
	Abbreviation     string `json:"abbreviation"`
	Logo             string `json:"logo"`
	Color            string `json:"color"`
}

type espnDetail struct {
	Clock espnClock `json:"clock"`
	Type  struct {
		Text string `json:"text"`
	} `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	ScoreValue       int  `json:"scoreValue"`
	ScoringPlay      bool `json:"scoringPlay"`
	RedCard          bool `json:"redCard"`
	YellowCard       bool `json:"yellowCard"`
	PenaltyKick      bool `json:"penaltyKick"`
	OwnGoal          bool `json:"ownGoal"`
	Shootout         bool `json:"shootout"`
	AthletesInvolved []struct {
		DisplayName string `json:"displayName"`
	} `json:"athletesInvolved"`
}

type espnStat struct {
	Name         string  `json:"name"`
	DisplayValue string  `json:"displayValue"`
	HomeValue    float64 `json:"homeValue"`
	AwayValue    float64 `json:"awayValue"`
}

type espnBroadcast struct {
	Market string   `json:"market"`
	Names  []string `json:"names"`
}

// espnClock accepts both shapes ESPN uses: a bare number of seconds on an
// event status, or an object on an individual play.
type espnClock struct {
	Value        float64
	DisplayValue string
}

func (c *espnClock) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '{' {
		var o struct {
			Value        float64 `json:"value"`
			DisplayValue string  `json:"displayValue"`
		}
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		c.Value, c.DisplayValue = o.Value, o.DisplayValue
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		c.Value = f
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return nil // tolerate unexpected shapes rather than failing the fetch
	}
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimRight(s, "'sS "), 64)
	if err != nil {
		return nil
	}
	c.Value = f
	return nil
}
