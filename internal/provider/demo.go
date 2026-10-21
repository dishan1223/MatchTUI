package provider

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"matchtui/internal/model"
)

// demoProvider fabricates fixtures so the TUI is useful offline, before a key
// is added, or while the layout is being tuned. Scores are derived from a
// deterministic seed plus the real clock, so a given minute always shows the
// same scoreline and live matches tick along like the real thing.
type demoProvider struct{}

// demoCatalogue is the competition list the demo offers.
var demoCatalogue = []League{
	{ID: "eng.1", Name: "Premier League (England)", Default: true},
	{ID: "esp.1", Name: "LaLiga (Spain)", Default: true},
	{ID: "ita.1", Name: "Serie A (Italy)", Default: true},
	{ID: "ger.1", Name: "Bundesliga (Germany)", Default: true},
	{ID: "uefa.champions", Name: "UEFA Champions League", Default: true},
}

// demoTags are the compact labels shown in the list column.
var demoTags = map[string]string{
	"eng.1":          "EPL",
	"esp.1":          "LaLiga",
	"ita.1":          "Serie A",
	"ger.1":          "Bundesliga",
	"uefa.champions": "UCL",
}

// demoTeams are the squads fixtures are drawn from.
var demoTeams = map[string][]string{
	"eng.1": {
		"Arsenal", "Aston Villa", "Bournemouth", "Brentford", "Brighton",
		"Chelsea", "Crystal Palace", "Everton", "Fulham", "Leeds United",
		"Liverpool", "Manchester City", "Manchester United", "Newcastle United",
		"Nottingham Forest", "Sunderland", "Tottenham Hotspur", "West Ham United",
	},
	"esp.1": {
		"Real Madrid", "FC Barcelona", "Atletico Madrid", "Athletic Club",
		"Real Betis", "Villarreal", "Real Sociedad", "Sevilla", "Valencia",
		"Girona", "Osasuna", "Celta Vigo", "Getafe", "Rayo Vallecano",
		"Mallorca", "Alaves",
	},
	"ita.1": {
		"Inter Milan", "AC Milan", "Juventus", "Napoli", "Roma", "Lazio",
		"Atalanta", "Fiorentina", "Bologna", "Torino", "Udinese", "Genoa",
		"Como", "Cagliari", "Hellas Verona", "Parma", "Lecce", "Pisa",
	},
	"ger.1": {
		"Bayern Munich", "Bayer Leverkusen", "Borussia Dortmund", "RB Leipzig",
		"Eintracht Frankfurt", "Stuttgart", "Freiburg", "Mainz 05", "Werder Bremen",
		"Wolfsburg", "Augsburg", "Hoffenheim", "Union Berlin", "Monchengladbach",
		"Heidenheim", "St. Pauli", "Koln", "Bochum",
	},
	"uefa.champions": {
		"Real Madrid", "Manchester City", "Bayern Munich", "Inter Milan",
		"Arsenal", "FC Barcelona", "Paris Saint-Germain", "Liverpool",
		"Bayer Leverkusen", "Atletico Madrid", "Borussia Dortmund", "Juventus",
		"Napoli", "Sporting CP", "PSV Eindhoven", "Club Brugge", "Celtic",
		"Galatasaray", "Monaco", "Ajax",
	},
}

// demoSlots are the kickoff times used across a day.
var demoSlots = []string{"12:30", "15:00", "17:30", "20:00", "21:00"}

// demoSurnames feed the simulated timeline.
var demoSurnames = []string{
	"Silva", "Rodriguez", "Muller", "Rossi", "Dubois", "Andersen", "Kowalski",
	"Novak", "Fernandez", "Costa", "Ibrahimovic", "Petrov", "Santos", "Larsen",
	"Moreno", "Bakker", "Okafor", "Yilmaz", "Kovacic", "Dembele", "Ricci",
	"Hansen", "Marques", "Schmidt", "Vargas", "Lindqvist", "Pereira", "Bauer",
	"Conte", "Nunez", "Diallo", "Eriksen", "Fabian", "Grimaldo", "Hradecky",
}

// NewDemo builds the offline provider.
func NewDemo() Provider { return &demoProvider{} }

func (p *demoProvider) ID() string   { return "demo" }
func (p *demoProvider) Name() string { return "Demo (offline)" }
func (p *demoProvider) Description() string {
	return "Simulated fixtures generated locally. No network or key required; scores advance in real time."
}
func (p *demoProvider) NeedsKey() bool          { return false }
func (p *demoProvider) KeyPresent() bool        { return true }
func (p *demoProvider) RateHint() time.Duration { return 0 }

func (p *demoProvider) Leagues(ctx context.Context) ([]League, error) {
	out := make([]League, len(demoCatalogue))
	copy(out, demoCatalogue)
	SortLeagues(out)
	return out, nil
}

// Fetch builds every requested (league, day) combination.
func (p *demoProvider) Fetch(ctx context.Context, req Request) ([]model.Match, error) {
	leagues := req.Leagues
	if len(leagues) == 0 {
		leagues = DefaultLeagueIDs(demoCatalogue)
	}

	now := time.Now()
	var out []model.Match
	for _, id := range leagues {
		name := id
		for _, l := range demoCatalogue {
			if l.ID == id {
				name = l.Name
				break
			}
		}
		for _, day := range req.Days() {
			out = append(out, demoFixtures(day, id, name, now)...)
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			default:
			}
		}
	}
	model.ByKickoff(out)
	return out, nil
}

// demoFixtures pairs up a shuffled squad list for one league and day.
func demoFixtures(day time.Time, leagueID, leagueName string, now time.Time) []model.Match {
	teams := demoTeams[leagueID]
	if len(teams) < 2 {
		return nil
	}
	dayKey := day.Format("20060102")
	rng := rand.New(rand.NewSource(int64(demoSeed(dayKey + "|" + leagueID))))

	order := make([]string, len(teams))
	copy(order, teams)
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	count := len(order) / 2
	if count > 4 {
		count = 4
	}
	out := make([]model.Match, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, demoFixture(day, dayKey, leagueID, leagueName, i, order[2*i], order[2*i+1], now))
	}
	return out
}

// demoGoal is one simulated scoring play.
type demoGoal struct {
	minute   int
	homeSide bool
	player   string
	penalty  bool
	ownGoal  bool
}

// demoFixture builds a single fixture and scores it against the real clock.
func demoFixture(day time.Time, dayKey, leagueID, leagueName string, idx int, home, away string, now time.Time) model.Match {
	kickoff := day
	if idx < len(demoSlots) {
		parts := strings.SplitN(demoSlots[idx], ":", 2)
		hh, _ := strconv.Atoi(parts[0])
		mm, _ := strconv.Atoi(parts[1])
		kickoff = time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, day.Location())
	}

	seed := demoSeed(strings.Join([]string{dayKey, leagueID, home, away}, "|"))
	rng := rand.New(rand.NewSource(int64(seed)))

	tag := strings.ToUpper(leagueID)
	if t, ok := demoTags[leagueID]; ok {
		tag = t
	}

	m := model.Match{
		ID:        fmt.Sprintf("demo-%s-%s-%d", dayKey, leagueID, idx),
		Provider:  "demo",
		LeagueID:  leagueID,
		League:    leagueName,
		LeagueTag: tag,
		Round:     "Matchweek " + strconv.Itoa(int(1+((seed>>3)%38))),
		Kickoff:   kickoff,
		Venue:     demoStadium(home),
		City:      home,
		Broadcast: []string{"MatchTUI Sports", "DemoTV HD"},
		Home:      model.Team{ID: demoTeamID(home), Name: home, Short: home, Abbr: demoAbbr(home)},
		Away:      model.Team{ID: demoTeamID(away), Name: away, Short: away, Abbr: demoAbbr(away)},
		UpdatedAt: now,
	}

	// Goals are generated once per fixture and revealed by the clock.
	total := rng.Intn(5)
	goals := make([]demoGoal, 0, total)
	for g := 0; g < total; g++ {
		gl := demoGoal{
			minute:   2 + rng.Intn(88),
			homeSide: rng.Intn(2) == 0,
			penalty:  rng.Intn(12) == 0,
		}
		gl.player = demoSurnames[rng.Intn(len(demoSurnames))]
		goals = append(goals, gl)
	}
	sort.SliceStable(goals, func(i, j int) bool { return goals[i].minute < goals[j].minute })

	elapsed := int(now.Sub(kickoff).Minutes())
	cutoff := elapsed
	if cutoff > 90 {
		cutoff = 90
	}

	var homeScore, awayScore, htHome, htAway int
	for _, gl := range goals {
		if gl.minute <= 45 {
			if gl.homeSide {
				htHome++
			} else {
				htAway++
			}
		}
		if gl.minute <= cutoff {
			if gl.homeSide {
				homeScore++
			} else {
				awayScore++
			}
		}
	}
	m.Home.Score, m.Away.Score = homeScore, awayScore
	m.Home.ScoreHT, m.Away.ScoreHT = htHome, htAway
	m.Home.Winner, m.Away.Winner = homeScore > awayScore, awayScore > homeScore

	switch {
	case elapsed < 0:
		m.State = model.StateScheduled
		m.StatusText = kickoff.Format("15:04")
	case elapsed < 45:
		m.State = model.StateLive
		m.Minute = elapsed + 1
		m.StatusText = fmt.Sprintf("%d'", m.Minute)
	case elapsed < 60:
		m.State = model.StateHalftime
		m.Minute = 45
		m.StatusText = "HT"
	case elapsed < 96:
		m.State = model.StateLive
		m.Minute = elapsed + 1
		m.StatusText = fmt.Sprintf("%d'", m.Minute)
	default:
		m.State = model.StateFinished
		m.StatusText = "FT"
	}

	// Timeline entries only appear once their minute has been reached.
	for _, gl := range goals {
		if gl.minute > cutoff {
			continue
		}
		runningHome, runningAway := runningScore(goals, gl)
		e := model.Event{
			Kind:    model.EventGoal,
			Minute:  fmt.Sprintf("%d'", gl.minute),
			Player:  gl.player,
			OwnGoal: gl.ownGoal,
			Penalty: gl.penalty,
			Score:   fmt.Sprintf("%d-%d", runningHome, runningAway),
		}
		if gl.homeSide {
			e.TeamID = m.Home.ID
		} else {
			e.TeamID = m.Away.ID
		}
		if gl.ownGoal {
			e.Kind = model.EventOwnGoal
		} else if gl.penalty {
			e.Kind = model.EventPen
		}
		m.Events = append(m.Events, e)
	}

	if m.State != model.StateScheduled {
		m.Stats = demoStats(seed, homeScore, awayScore)
	}
	return m
}

// runningScore renders the scoreline as it stood after a given goal.
func runningScore(goals []demoGoal, target demoGoal) (int, int) {
	var h, a int
	for _, gl := range goals {
		if gl.minute > target.minute {
			break
		}
		if gl.homeSide {
			h++
		} else {
			a++
		}
	}
	return h, a
}

// demoStats fabricates plausible statistics tied to the seed and scoreline.
func demoStats(seed uint32, homeScore, awayScore int) []model.Stat {
	rng := rand.New(rand.NewSource(int64(seed) + 7))
	poss := 38 + rng.Intn(25)
	shotsH, shotsA := 6+rng.Intn(12), 4+rng.Intn(11)
	sotH, sotA := 1+rng.Intn(6), 1+rng.Intn(5)
	if sotH > shotsH {
		sotH = shotsH
	}
	if sotA > shotsA {
		sotA = shotsA
	}
	cornersH, cornersA := 2+rng.Intn(8), 1+rng.Intn(7)
	foulsH, foulsA := 6+rng.Intn(10), 5+rng.Intn(11)
	yellowH, yellowA := rng.Intn(4), rng.Intn(4)
	xgH := float64(homeScore) + rng.Float64()*1.5
	xgA := float64(awayScore) + rng.Float64()*1.3

	return []model.Stat{
		{Label: "Possession", Home: fmt.Sprintf("%d%%", poss), Away: fmt.Sprintf("%d%%", 100-poss), HomeVal: float64(poss), AwayVal: float64(100 - poss)},
		{Label: "Shots", Home: strconv.Itoa(shotsH), Away: strconv.Itoa(shotsA), HomeVal: float64(shotsH), AwayVal: float64(shotsA)},
		{Label: "Shots on target", Home: strconv.Itoa(sotH), Away: strconv.Itoa(sotA), HomeVal: float64(sotH), AwayVal: float64(sotA)},
		{Label: "Expected goals", Home: fmt.Sprintf("%.2f", xgH), Away: fmt.Sprintf("%.2f", xgA), HomeVal: xgH, AwayVal: xgA},
		{Label: "Corners", Home: strconv.Itoa(cornersH), Away: strconv.Itoa(cornersA), HomeVal: float64(cornersH), AwayVal: float64(cornersA)},
		{Label: "Fouls", Home: strconv.Itoa(foulsH), Away: strconv.Itoa(foulsA), HomeVal: float64(foulsH), AwayVal: float64(foulsA)},
		{Label: "Yellow cards", Home: strconv.Itoa(yellowH), Away: strconv.Itoa(yellowA), HomeVal: float64(yellowH), AwayVal: float64(yellowA)},
	}
}

// FetchDetail invents team sheets so the full-screen match page can be built
// and tested offline.
func (p *demoProvider) FetchDetail(ctx context.Context, m model.Match) (model.Match, error) {
	seed := demoSeed(m.ID + "|lineups")
	rng := rand.New(rand.NewSource(int64(seed)))

	formation := []string{"4-3-3", "4-2-3-1", "3-4-2-1", "4-4-2"}[seed%4]
	m.Lineups = []model.Lineup{
		demoLineup(rng, m.Home, formation),
		demoLineup(rng, m.Away, []string{"4-3-3", "4-4-2", "3-5-2", "4-2-3-1"}[(seed>>3)%4]),
	}
	return m, nil
}

func (p *demoProvider) DetailName() string { return "team sheets" }

// demoLineup builds a plausible eleven plus bench for one side.
func demoLineup(rng *rand.Rand, team model.Team, formation string) model.Lineup {
	positions := []string{"G-K", "D-L", "D-C", "D-C", "D-R", "M-C", "M-C", "M-C", "F-L", "F-C", "F-R"}
	l := model.Lineup{
		TeamID:    team.ID,
		TeamName:  team.Name,
		Formation: formation,
	}
	used := map[string]bool{}
	for i, pos := range positions {
		l.Starters = append(l.Starters, model.Player{
			Number:  strconv.Itoa(i + 1),
			Name:    demoName(rng, used),
			Pos:     pos,
			Starter: true,
		})
	}
	for i := 0; i < 9; i++ {
		l.Bench = append(l.Bench, model.Player{
			Number: strconv.Itoa(12 + i),
			Name:   demoName(rng, used),
			Pos:    []string{"G-K", "D-C", "M-C", "F-C"}[i%4],
		})
	}
	return l
}

// demoName draws a not-yet-used surname so a lineup has no duplicates.
func demoName(rng *rand.Rand, used map[string]bool) string {
	for tries := 0; tries < 64; tries++ {
		n := demoSurnames[rng.Intn(len(demoSurnames))]
		if !used[n] {
			used[n] = true
			return n
		}
	}
	return demoSurnames[rng.Intn(len(demoSurnames))]
}

// demoStadium invents a plausible venue name.
func demoStadium(team string) string {
	fields := strings.Fields(team)
	if len(fields) > 1 && len(fields[0]) < 12 {
		return fields[0] + " Park"
	}
	return team + " Park"
}

// demoAbbr derives a short label, e.g. "City" for Manchester City.
func demoAbbr(team string) string {
	fields := strings.Fields(team)
	if len(fields) == 0 {
		return team
	}
	if len(fields) > 1 {
		last := fields[len(fields)-1]
		if r := []rune(last); len(r) <= 8 {
			return last
		}
	}
	first := fields[0]
	if r := []rune(first); len(r) > 8 {
		return string(r[:8])
	}
	return first
}

// demoTeamID mints a stable identifier for a team name.
func demoTeamID(team string) string {
	return "demo-team-" + strconv.FormatUint(uint64(demoSeed(team)), 10)
}

// demoSeed hashes a string into a reproducible fixture seed.
func demoSeed(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
