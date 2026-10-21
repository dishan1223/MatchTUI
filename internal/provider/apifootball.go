package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"matchtui/internal/model"
)

// apiFootballBase is the root of API-FOOTBALL (api-sports.io). The free tier
// allows roughly 100 requests per day, which is enough for periodic live
// polling but not for rapid refreshes, so RateHint throttles the UI.
const apiFootballBase = "https://v3.football.api-sports.io"

// apiFootballKeyHeader is the header carrying the free API key.
const apiFootballKeyHeader = "x-apisports-key"

// apiFootballRateHint keeps the free daily quota inside its limit: at fifteen
// minutes apart a day costs 96 polling requests.
const apiFootballRateHint = 15 * time.Minute

// apiFootballCacheTTL is how long the competition catalogue stays valid.
const apiFootballCacheTTL = 24 * time.Hour

// apiFootballDefaultIDs are the competitions selected on first use.
var apiFootballDefaultIDs = map[int]bool{
	39:  true, // Premier League
	140: true, // LaLiga
	135: true, // Serie A
	78:  true, // Bundesliga
	61:  true, // Ligue 1
	2:   true, // UEFA Champions League
}

// apiFootballProvider reads API-FOOTBALL v3.
type apiFootballProvider struct {
	vault    Vault
	mu       sync.Mutex
	leagues  []League
	loadedAt time.Time
}

// NewAPIFootball builds the key-backed provider.
func NewAPIFootball(v Vault) Provider {
	if v == nil {
		v = nullVault{}
	}
	return &apiFootballProvider{vault: v}
}

func (p *apiFootballProvider) ID() string   { return "apifootball" }
func (p *apiFootballProvider) Name() string { return "API-FOOTBALL" }
func (p *apiFootballProvider) Description() string {
	return "Full fixture, stat and timeline coverage via api-sports.io. Free tier needs a key (about 100 requests/day)."
}
func (p *apiFootballProvider) NeedsKey() bool { return true }
func (p *apiFootballProvider) KeyPresent() bool {
	return p.vault.GetKey(p.ID()) != ""
}
func (p *apiFootballProvider) RateHint() time.Duration { return apiFootballRateHint }

// headers returns the authentication headers for a request.
func (p *apiFootballProvider) headers() (map[string]string, error) {
	key := p.vault.GetKey(p.ID())
	if key == "" {
		return nil, fmt.Errorf("API-FOOTBALL needs a key: press k to add one")
	}
	return map[string]string{apiFootballKeyHeader: key}, nil
}

// Leagues lists the competitions the feed exposes. Cached for a day because the
// catalogue rarely changes and the free tier is metered.
func (p *apiFootballProvider) Leagues(ctx context.Context) ([]League, error) {
	p.mu.Lock()
	if len(p.leagues) > 0 && time.Since(p.loadedAt) < apiFootballCacheTTL {
		out := make([]League, len(p.leagues))
		copy(out, p.leagues)
		p.mu.Unlock()
		return out, nil
	}
	p.mu.Unlock()

	headers, err := p.headers()
	if err != nil {
		return nil, err
	}
	var root apiFootballLeagues
	if err := getJSON(ctx, apiFootballBase+"/leagues", headers, &root); err != nil {
		p.mu.Lock()
		cached := p.leagues
		p.mu.Unlock()
		if len(cached) > 0 {
			return cached, nil
		}
		return nil, err
	}

	out := make([]League, 0, len(root.Response))
	for _, r := range root.Response {
		id := strconv.Itoa(r.League.ID)
		name := r.League.Name
		if r.League.Country != "" {
			name += " (" + r.League.Country + ")"
		}
		out = append(out, League{
			ID:      id,
			Name:    name,
			Default: apiFootballDefaultIDs[r.League.ID],
		})
	}
	SortLeagues(out)

	p.mu.Lock()
	p.leagues, p.loadedAt = out, time.Now()
	p.mu.Unlock()

	clean := make([]League, len(out))
	copy(clean, out)
	return clean, nil
}

// Fetch loads live fixtures in a single call, or one request per day for the
// upcoming and results windows.
func (p *apiFootballProvider) Fetch(ctx context.Context, req Request) ([]model.Match, error) {
	headers, err := p.headers()
	if err != nil {
		return nil, err
	}

	var (
		out      []model.Match
		firstErr error
	)
	want := make(map[string]bool, len(req.Leagues))
	for _, id := range req.Leagues {
		want[id] = true
	}

	urls := make([]string, 0, len(req.Days())+1)
	if req.Window == "live" {
		urls = append(urls, apiFootballBase+"/fixtures?live=all")
	} else {
		for _, d := range req.Days() {
			urls = append(urls, fmt.Sprintf("%s/fixtures?date=%s", apiFootballBase, d.Format("2006-01-02")))
		}
	}

	for _, u := range urls {
		var root apiFootballRoot
		if err := getJSON(ctx, u, headers, &root); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if msg := apiFootballError(root.Errors); msg != "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("API-FOOTBALL: %s", msg)
			}
			continue
		}
		for _, fx := range root.Response {
			leagueID := strconv.Itoa(fx.League.ID)
			if len(want) > 0 && !want[leagueID] {
				continue
			}
			out = append(out, p.mapFixture(fx, leagueID))
		}
	}

	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	model.ByKickoff(out)
	return out, nil
}

// apiFootballError renders the feed's error list, which can be strings or
// objects depending on the failure.
func apiFootballError(raw []json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	parts := make([]string, 0, len(raw))
	for _, r := range raw {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			parts = append(parts, s)
			continue
		}
		var o map[string]any
		if err := json.Unmarshal(r, &o); err == nil {
			for _, v := range o {
				parts = append(parts, fmt.Sprintf("%v", v))
			}
			continue
		}
		parts = append(parts, string(r))
	}
	return strings.Join(parts, "; ")
}

// mapFixture converts one API-FOOTBALL fixture into the shared model.
func (p *apiFootballProvider) mapFixture(fx apiFootballFixture, leagueID string) model.Match {
	m := model.Match{
		ID:        fmt.Sprintf("af-%d", fx.Fixture.ID),
		Provider:  p.ID(),
		LeagueID:  leagueID,
		League:    fx.League.Name,
		LeagueTag: afTag(fx.League.Name),
		Round:     fx.League.Round,
		Home: model.Team{
			ID:    strconv.Itoa(fx.Teams.Home.ID),
			Name:  fx.Teams.Home.Name,
			Short: fx.Teams.Home.Name,
			Logo:  fx.Teams.Home.Logo,
		},
		Away: model.Team{
			ID:    strconv.Itoa(fx.Teams.Away.ID),
			Name:  fx.Teams.Away.Name,
			Short: fx.Teams.Away.Name,
			Logo:  fx.Teams.Away.Logo,
		},
		Venue:     fx.Fixture.Venue.Name,
		City:      fx.Fixture.Venue.City,
		UpdatedAt: time.Now(),
	}
	if ts := fx.Fixture.Timestamp; ts > 0 {
		m.Kickoff = time.Unix(ts, 0).Local()
	} else if !fx.Fixture.Date.IsZero() {
		m.Kickoff = fx.Fixture.Date.Time.Local()
	}

	if fx.Goals != nil {
		if fx.Goals.Home != nil {
			m.Home.Score = *fx.Goals.Home
		}
		if fx.Goals.Away != nil {
			m.Away.Score = *fx.Goals.Away
		}
	}
	if fx.Score.Halftime.Home != nil {
		m.Home.ScoreHT = *fx.Score.Halftime.Home
	}
	if fx.Score.Halftime.Away != nil {
		m.Away.ScoreHT = *fx.Score.Halftime.Away
	}
	if fx.Teams.Home.Winner != nil {
		m.Home.Winner = *fx.Teams.Home.Winner
	}
	if fx.Teams.Away.Winner != nil {
		m.Away.Winner = *fx.Teams.Away.Winner
	}

	m.State, m.Minute, m.StatusText = afStatus(fx.Fixture.Status, m.Kickoff, time.Now())
	for _, e := range fx.Events {
		m.Events = append(m.Events, afEvent(e))
	}
	return m
}

// afTag derives a compact league label from the competition name.
func afTag(name string) string {
	name = strings.TrimSpace(name)
	if len(name) <= 6 {
		return strings.ToUpper(name)
	}
	words := strings.Fields(name)
	if len(words) == 1 {
		return strings.ToUpper(words[0][:min(6, len(words[0]))])
	}
	var b strings.Builder
	for _, w := range words {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		b.WriteRune(r[0])
	}
	s := b.String()
	if len(s) > 5 {
		s = s[:5]
	}
	return strings.ToUpper(s)
}

// afStatus normalises an API-FOOTBALL status code.
func afStatus(st apiFootballStatus, kickoff, now time.Time) (model.State, int, string) {
	switch strings.ToUpper(st.Short) {
	case "TBD", "NS":
		return model.StateScheduled, 0, "SCHED"
	case "1H", "2H", "LIVE", "ET", "BT", "P", "INT":
		mins := st.Elapsed
		if mins <= 0 && !kickoff.IsZero() {
			mins = int(now.Sub(kickoff).Minutes()) + 1
		}
		if mins < 1 {
			mins = 1
		}
		if strings.EqualFold(st.Short, "HT") || strings.EqualFold(st.Short, "BT") {
			return model.StateHalftime, 45, "HT"
		}
		return model.StateLive, mins, fmt.Sprintf("%d'", mins)
	case "HT":
		return model.StateHalftime, 45, "HT"
	case "FT":
		return model.StateFinished, 0, "FT"
	case "AET":
		return model.StateFinished, 0, "AET"
	case "PEN":
		return model.StateFinished, 0, "PENS"
	case "PST":
		return model.StatePostponed, 0, "PP"
	case "CANC", "ABD", "AWD", "WO", "SUSP":
		return model.StateSuspended, 0, "SUSP"
	default:
		if st.Long != "" {
			return model.StateScheduled, 0, truncateStatus(st.Long)
		}
		return model.StateScheduled, 0, truncateStatus(st.Short)
	}
}

// afEvent converts one timeline entry.
func afEvent(e apiFootballEvent) model.Event {
	out := model.Event{
		Player: e.Player.Name,
		TeamID: strconv.Itoa(e.Team.ID),
	}
	if e.Time.Elapsed > 0 {
		out.Minute = fmt.Sprintf("%d'", e.Time.Elapsed)
		if e.Time.Extra != nil && *e.Time.Extra > 0 {
			out.Minute = fmt.Sprintf("%d+%d'", e.Time.Elapsed, *e.Time.Extra)
		}
	} else {
		out.Minute = "-"
	}

	detail := strings.ToLower(e.Detail)
	switch {
	case strings.Contains(detail, "penalty"):
		out.Kind, out.Penalty = model.EventPen, true
	case strings.Contains(detail, "own goal"):
		out.Kind, out.OwnGoal = model.EventOwnGoal, true
	case strings.EqualFold(e.Type, "Goal"):
		out.Kind = model.EventGoal
	case strings.Contains(detail, "red card"):
		out.Kind = model.EventRed
	case strings.Contains(detail, "yellow card"):
		out.Kind = model.EventYellow
	case strings.EqualFold(e.Type, "Card"):
		out.Kind = model.EventYellow
	case strings.EqualFold(e.Type, "subst"):
		out.Kind = model.EventSub
	default:
		out.Kind = model.EventVar
	}
	return out
}

// ---- wire types -----------------------------------------------------------

type apiFootballRoot struct {
	Response []apiFootballFixture `json:"response"`
	Errors   []json.RawMessage    `json:"errors"`
}

type apiFootballLeagues struct {
	Response []struct {
		League struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Country string `json:"country"`
			Logo    string `json:"logo"`
			Flag    string `json:"flag"`
		} `json:"league"`
		Countries []string `json:"countries"`
	} `json:"response"`
	Errors []json.RawMessage `json:"errors"`
}

type apiFootballFixture struct {
	Fixture struct {
		ID        int64             `json:"id"`
		Referee   string            `json:"referee"`
		Timezone  string            `json:"timezone"`
		Date      flexTime          `json:"date"`
		Timestamp int64             `json:"timestamp"`
		Status    apiFootballStatus `json:"status"`
		Venue     struct {
			Name string `json:"name"`
			City string `json:"city"`
		} `json:"venue"`
	} `json:"fixture"`
	League struct {
		ID      int      `json:"id"`
		Name    string   `json:"name"`
		Country string   `json:"country"`
		Logo    string   `json:"logo"`
		Flag    string   `json:"flag"`
		Season  asString `json:"season"`
		Round   string   `json:"round"`
	} `json:"league"`
	Teams struct {
		Home struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Logo   string `json:"logo"`
			Winner *bool  `json:"winner"`
		} `json:"home"`
		Away struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Logo   string `json:"logo"`
			Winner *bool  `json:"winner"`
		} `json:"away"`
	} `json:"teams"`
	Goals *struct {
		Home *int `json:"home"`
		Away *int `json:"away"`
	} `json:"goals"`
	Score struct {
		Halftime struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"halftime"`
		Fulltime struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"fulltime"`
		Penalty struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"penalty"`
	} `json:"score"`
	Events []apiFootballEvent `json:"events"`
}

type apiFootballStatus struct {
	Short   string `json:"short"`
	Long    string `json:"long"`
	Elapsed int    `json:"elapsed"`
}

type apiFootballEvent struct {
	Time struct {
		Elapsed int  `json:"elapsed"`
		Extra   *int `json:"extra"`
	} `json:"time"`
	Team struct {
		ID int `json:"id"`
	} `json:"team"`
	Player struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"player"`
	Type     string `json:"type"`
	Detail   string `json:"detail"`
	Comments string `json:"comments"`
}
