package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"matchtui/internal/model"
)

// olBase is the root of OpenLigaDB's public API. It is free, needs no key and
// is strongest on the German leagues plus European competitions.
const olBase = "https://api.openligadb.de"

// olCacheTTL is how long the competition catalogue is considered fresh.
const olCacheTTL = 24 * time.Hour

// olDefaults are the competitions selected the first time the menu is opened.
var olDefaults = map[string]bool{
	"bl1": true, // 1. Bundesliga
	"bl3": true, // 3. Liga
	"dfb": true, // DFB-Pokal
	"cl":  true, // Champions League
}

// olNameSuffix matches the trailing season a league name carries, e.g.
// "1. Bundesliga 2025/2026".
var olNameSuffix = regexp.MustCompile(`\s+\d{4}(/\d{4})?$`)

// openLigaProvider reads OpenLigaDB.
type openLigaProvider struct {
	mu       sync.Mutex
	leagues  []League
	seasons  map[string]int
	loadedAt time.Time
}

// NewOpenLigaDB builds the second key-free provider.
func NewOpenLigaDB() Provider { return &openLigaProvider{} }

func (p *openLigaProvider) ID() string   { return "openligadb" }
func (p *openLigaProvider) Name() string { return "OpenLigaDB" }
func (p *openLigaProvider) Description() string {
	return "Free community feed with no key required. Strong on German leagues, DFB-Pokal and UEFA competitions."
}
func (p *openLigaProvider) NeedsKey() bool          { return false }
func (p *openLigaProvider) KeyPresent() bool        { return true }
func (p *openLigaProvider) RateHint() time.Duration { return 0 }

// Leagues returns the competitions OpenLigaDB currently publishes, restricted
// to recent seasons so the menu stays readable.
func (p *openLigaProvider) Leagues(ctx context.Context) ([]League, error) {
	p.mu.Lock()
	if len(p.leagues) > 0 && time.Since(p.loadedAt) < olCacheTTL {
		out := make([]League, len(p.leagues))
		copy(out, p.leagues)
		p.mu.Unlock()
		return out, nil
	}
	p.mu.Unlock()

	var raw []olLeague
	if err := getJSON(ctx, olBase+"/getavailableleagues", nil, &raw); err != nil {
		p.mu.Lock()
		cached := p.leagues
		p.mu.Unlock()
		if len(cached) > 0 {
			return cached, nil // stale beats empty
		}
		return nil, err
	}

	// Keep the newest season seen for each competition.
	newest := make(map[string]olLeague, len(raw))
	for _, l := range raw {
		if strings.TrimSpace(l.LeagueShortcut) == "" {
			continue
		}
		if cur, ok := newest[l.LeagueShortcut]; !ok || l.LeagueSeason > cur.LeagueSeason {
			newest[l.LeagueShortcut] = l
		}
	}

	cutoff := time.Now().Year() - 1
	out := make([]League, 0, len(newest))
	for shortcut, l := range newest {
		if l.LeagueSeason.Int() < cutoff {
			continue // long finished seasons only add noise
		}
		season := strconv.Itoa(l.LeagueSeason.Int())
		out = append(out, League{
			ID:      shortcut,
			Name:    olDisplayName(l.LeagueName, season),
			Season:  season,
			Default: olDefaults[shortcut],
		})
	}
	SortLeagues(out)

	seasons := make(map[string]int, len(out))
	for _, l := range out {
		if n, err := strconv.Atoi(l.Season); err == nil {
			seasons[l.ID] = n
		}
	}

	p.mu.Lock()
	p.leagues, p.seasons, p.loadedAt = out, seasons, time.Now()
	p.mu.Unlock()

	clean := make([]League, len(out))
	copy(clean, out)
	return clean, nil
}

// olDisplayName strips the trailing season from a competition name.
func olDisplayName(name, season string) string {
	name = strings.TrimSpace(olNameSuffix.ReplaceAllString(strings.TrimSpace(name), ""))
	if name == "" {
		return season
	}
	return name
}

// seasons returns the season to request for each competition, filling gaps with
// the current and previous calendar year.
func (p *openLigaProvider) resolveSeasons(ctx context.Context, shortcuts []string) map[string]int {
	if _, err := p.Leagues(ctx); err != nil {
		// Catalogue fetch failed; fall back to guessing from the calendar.
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make(map[string]int, len(shortcuts))
	now := time.Now().Year()
	for _, sc := range shortcuts {
		if s, ok := p.seasons[sc]; ok {
			out[sc] = s
			continue
		}
		// Northern hemisphere seasons are labelled by their starting year.
		out[sc] = now
	}
	return out
}

// Fetch pulls each requested competition's season in one call and keeps the
// matches inside the requested window.
func (p *openLigaProvider) Fetch(ctx context.Context, req Request) ([]model.Match, error) {
	shortcuts := req.Leagues
	if len(shortcuts) == 0 {
		if ls, err := p.Leagues(ctx); err == nil {
			shortcuts = DefaultLeagueIDs(ls)
		}
	}
	if len(shortcuts) == 0 {
		return nil, fmt.Errorf("no competitions available from OpenLigaDB")
	}

	seasons := p.resolveSeasons(ctx, shortcuts)
	from := startOfDay(req.From).Add(-time.Hour)
	to := startOfDay(req.To).Add(24*time.Hour - time.Minute)

	var (
		out      []model.Match
		firstErr error
	)
	now := time.Now()
	for _, sc := range shortcuts {
		season := seasons[sc]
		url := fmt.Sprintf("%s/getmatchdata/%s/%d", olBase, sc, season)
		var raw []olMatch
		if err := getJSON(ctx, url, nil, &raw); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		seasonLabel := strconv.Itoa(season)
		for _, om := range raw {
			kickoff, ok := olKickoff(om)
			if !ok || kickoff.Before(from) || kickoff.After(to) {
				continue
			}
			out = append(out, p.mapMatch(om, kickoff, sc, seasonLabel, now))
		}
	}

	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	model.ByKickoff(out)
	return out, nil
}

// olKickoff prefers the UTC timestamp and falls back to the local one.
func olKickoff(om olMatch) (time.Time, bool) {
	if s := strings.TrimSpace(om.DateTimeUTC); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Local(), true
		}
	}
	if s := strings.TrimSpace(om.DateTime); s != "" {
		for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.Local(), true
			}
		}
	}
	return time.Time{}, false
}

// mapMatch converts one OpenLigaDB fixture into the shared model.
func (p *openLigaProvider) mapMatch(om olMatch, kickoff time.Time, shortcut, season string, now time.Time) model.Match {
	m := model.Match{
		ID:        fmt.Sprintf("ol-%d", om.MatchID),
		Provider:  p.ID(),
		LeagueID:  shortcut,
		League:    olDisplayName(om.LeagueName, season),
		LeagueTag: strings.ToUpper(shortcut),
		Round:     om.Group.GroupName,
		Kickoff:   kickoff,
		Home: model.Team{
			ID:    strconv.FormatInt(om.Team1.TeamID, 10),
			Name:  om.Team1.TeamName,
			Short: om.Team1.ShortName,
			Logo:  om.Team1.TeamIconURL,
		},
		Away: model.Team{
			ID:    strconv.FormatInt(om.Team2.TeamID, 10),
			Name:  om.Team2.TeamName,
			Short: om.Team2.ShortName,
			Logo:  om.Team2.TeamIconURL,
		},
		UpdatedAt: now,
	}
	if om.Location != nil {
		m.Venue, m.City = om.Location.Stadium, om.Location.City
	}

	ht, final, pens := olResults(om.MatchResults)
	if om.MatchIsFinished {
		m.State = model.StateFinished
		m.Home.Score, m.Away.Score = final[0], final[1]
		m.Home.ScoreHT, m.Away.ScoreHT = ht[0], ht[1]
		m.Home.Winner, m.Away.Winner = final[0] > final[1], final[1] > final[0]
		switch {
		case pens:
			m.StatusText = "PENS"
		case !m.Home.Winner && !m.Away.Winner:
			m.StatusText = "DRAW"
		default:
			m.StatusText = "FT"
		}
	} else if !kickoff.After(now) {
		m.State = model.StateLive
		m.Minute = int(now.Sub(kickoff).Minutes()) + 1
		if m.Minute < 1 {
			m.Minute = 1
		}
		if m.Minute > 130 {
			m.Minute = 130
		}
		m.StatusText = fmt.Sprintf("%d'", m.Minute)
	} else {
		m.State = model.StateScheduled
		m.StatusText = kickoff.Format("15:04")
	}

	for _, g := range om.Goals {
		e := model.Event{
			Kind:    model.EventGoal,
			Player:  g.GoalGetterName,
			TeamID:  strconv.FormatInt(g.ScoringTeamID, 10),
			OwnGoal: g.IsOwnGoal,
			Penalty: g.IsPenalty,
			Score:   fmt.Sprintf("%d-%d", g.ScoreTeam1.Int(), g.ScoreTeam2.Int()),
		}
		if g.IsOwnGoal {
			e.Kind = model.EventOwnGoal
		} else if g.IsPenalty {
			e.Kind = model.EventPen
		}
		if g.MatchMinute != nil {
			e.Minute = fmt.Sprintf("%d'", g.MatchMinute.Int())
		} else {
			e.Minute = "-"
		}
		m.Events = append(m.Events, e)
	}
	return m
}

// olResults extracts half-time, final and penalty information from the result
// list OpenLigaDB accumulates as a match progresses.
func olResults(rs []olResult) (ht, final [2]int, pens bool) {
	htFound := false
	var last [2]int
	for _, r := range rs {
		last = [2]int{r.PointsTeam1.Int(), r.PointsTeam2.Int()}
		kind := strings.ToLower(r.ResultTypeKind)
		switch {
		case kind == "halftime":
			ht, htFound = [2]int{r.PointsTeam1.Int(), r.PointsTeam2.Int()}, true
		case strings.Contains(kind, "penalt"):
			pens = true
		case strings.Contains(kind, "after90"):
			final = [2]int{r.PointsTeam1.Int(), r.PointsTeam2.Int()}
		}
	}
	if final == [2]int{} && last != [2]int{} {
		final = last
	}
	if !htFound {
		// No half-time record: fall back to nothing rather than guessing.
		ht = [2]int{}
	}
	return ht, final, pens
}

// ---- wire types -----------------------------------------------------------

type olLeague struct {
	LeagueShortcut string `json:"leagueShortcut"`
	LeagueName     string `json:"leagueName"`
	LeagueSeason   asInt  `json:"leagueSeason"`
	LeagueID       int64  `json:"leagueId"`
	IsCup          bool   `json:"isCup"`
}

type olMatch struct {
	MatchID         int64       `json:"matchID"`
	DateTime        string      `json:"matchDateTime"`
	DateTimeUTC     string      `json:"matchDateTimeUTC"`
	LeagueName      string      `json:"leagueName"`
	LeagueShortcut  string      `json:"leagueShortcut"`
	Group           olGroup     `json:"group"`
	Team1           olTeam      `json:"team1"`
	Team2           olTeam      `json:"team2"`
	MatchIsFinished bool        `json:"matchIsFinished"`
	MatchResults    []olResult  `json:"matchResults"`
	Goals           []olGoal    `json:"goals"`
	Location        *olLocation `json:"location"`
}

type olGroup struct {
	GroupName    string `json:"groupName"`
	GroupOrderID int    `json:"groupOrderID"`
}

type olTeam struct {
	TeamID      int64  `json:"teamId"`
	TeamName    string `json:"teamName"`
	ShortName   string `json:"shortName"`
	TeamIconURL string `json:"teamIconUrl"`
}

type olResult struct {
	ResultID       int64  `json:"resultID"`
	ResultName     string `json:"resultName"`
	PointsTeam1    asInt  `json:"pointsTeam1"`
	PointsTeam2    asInt  `json:"pointsTeam2"`
	ResultOrderID  int    `json:"resultOrderID"`
	ResultTypeID   int    `json:"resultTypeID"`
	ResultTypeKind string `json:"resultTypeKind"`
}

type olGoal struct {
	GoalID         int64  `json:"goalID"`
	ScoreTeam1     asInt  `json:"scoreTeam1"`
	ScoreTeam2     asInt  `json:"scoreTeam2"`
	MatchMinute    *asInt `json:"matchMinute"`
	GoalGetterName string `json:"goalGetterName"`
	ScoringTeamID  int64  `json:"scoringTeamId"`
	IsPenalty      bool   `json:"isPenalty"`
	IsOwnGoal      bool   `json:"isOwnGoal"`
	Comment        string `json:"comment"`
}

type olLocation struct {
	Stadium string `json:"stadium"`
	City    string `json:"city"`
}
