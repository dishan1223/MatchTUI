// Package model holds the provider-agnostic data the TUI renders. Every feed is
// normalised into these types so the UI never has to know which API it is
// talking to.
package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// State is the lifecycle stage of a match.
type State int

const (
	// StateScheduled means kickoff is still in the future.
	StateScheduled State = iota
	// StateLive means play is in progress.
	StateLive
	// StateHalftime means the interval between halves.
	StateHalftime
	// StateFinished means the match has concluded.
	StateFinished
	// StatePostponed means the fixture was called off.
	StatePostponed
	// StateSuspended means play was halted or abandoned.
	StateSuspended
)

// String returns a human readable label.
func (s State) String() string {
	switch s {
	case StateLive:
		return "Live"
	case StateHalftime:
		return "Half Time"
	case StateFinished:
		return "Full Time"
	case StatePostponed:
		return "Postponed"
	case StateSuspended:
		return "Suspended"
	default:
		return "Scheduled"
	}
}

// Live reports whether the ball may be in play right now.
func (s State) Live() bool { return s == StateLive || s == StateHalftime }

// Finished reports whether the match has concluded.
func (s State) Finished() bool { return s == StateFinished }

// Upcoming reports whether the match has not started yet.
func (s State) Upcoming() bool {
	return s == StateScheduled || s == StatePostponed || s == StateSuspended
}

// Team is one side of a fixture.
type Team struct {
	ID      string
	Name    string
	Short   string
	Abbr    string
	Logo    string
	Score   int
	ScoreHT int
	Winner  bool
	Form    string
}

// Label returns the name that fits the available space: short when the terminal
// is cramped, full otherwise.
func (t Team) Label(full bool) string {
	if !full && t.Short != "" {
		return t.Short
	}
	if t.Name != "" {
		return t.Name
	}
	return t.Abbr
}

// EventKind classifies one entry on the match timeline.
type EventKind string

// Timeline event kinds.
const (
	EventGoal    EventKind = "GOAL"
	EventOwnGoal EventKind = "OWN GOAL"
	EventPen     EventKind = "PEN"
	EventYellow  EventKind = "YELLOW"
	EventRed     EventKind = "RED"
	EventSub     EventKind = "SUB"
	EventVar     EventKind = "VAR"
)

// Scoring reports whether the event changes the scoreline.
func (k EventKind) Scoring() bool {
	return k == EventGoal || k == EventOwnGoal || k == EventPen
}

// Label is the short tag shown in the timeline column.
func (k EventKind) Label() string { return string(k) }

// Event is a single timeline entry.
type Event struct {
	Kind    EventKind
	Minute  string
	Player  string
	TeamID  string
	OwnGoal bool
	Penalty bool
	Score   string
}

// Stat is one comparable team statistic.
type Stat struct {
	Label   string
	Home    string
	Away    string
	HomeVal float64
	AwayVal float64
}

// Player is one named participant in a team sheet.
type Player struct {
	Number  string
	Name    string
	Pos     string
	Starter bool
	SubIn   bool
	SubOut  bool
}

// Lineup is one team's team sheet for a fixture. It is only populated when the
// provider was asked for detail, which happens lazily when a match is opened.
type Lineup struct {
	TeamID    string
	TeamName  string
	Formation string
	Starters  []Player
	Bench     []Player
}

// Match is a single fixture.
type Match struct {
	ID         string
	Provider   string
	LeagueID   string
	League     string
	LeagueTag  string
	Round      string
	Home       Team
	Away       Team
	State      State
	StatusText string
	Minute     int
	Kickoff    time.Time
	Venue      string
	City       string
	Broadcast  []string
	Events     []Event
	Stats      []Stat
	Lineups    []Lineup
	UpdatedAt  time.Time
}

// LineupFor returns the team sheet for one side, or a zero value when the
// fixture has none loaded.
func (m Match) LineupFor(teamID string) (Lineup, bool) {
	for _, l := range m.Lineups {
		if l.TeamID == teamID {
			return l, true
		}
	}
	return Lineup{}, false
}

// ScoreLine renders the current or final scoreline.
func (m Match) ScoreLine() string {
	return fmt.Sprintf("%d - %d", m.Home.Score, m.Away.Score)
}

// KickoffLabel renders the local kickoff time using the given layout.
func (m Match) KickoffLabel(layout string) string {
	if m.Kickoff.IsZero() {
		return "--:--"
	}
	return m.Kickoff.Format(layout)
}

// Key is a stable identity for the fixture. It is used to preserve the cursor
// across refreshes and to remember starred matches between runs.
func (m Match) Key() string {
	return strings.Join([]string{
		m.Provider,
		m.LeagueID,
		m.Home.ID,
		m.Away.ID,
		m.Kickoff.UTC().Format(time.RFC3339),
	}, "|")
}

// Day returns the local calendar day the fixture is anchored to.
func (m Match) Day() time.Time {
	if m.Kickoff.IsZero() {
		return time.Time{}
	}
	y, mo, d := m.Kickoff.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, m.Kickoff.Location())
}

// ByKickoff sorts matches oldest first, which is the natural reading order for
// both results and fixtures.
func ByKickoff(ms []Match) {
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Kickoff.Equal(ms[j].Kickoff) {
			return ms[i].Home.Name < ms[j].Home.Name
		}
		return ms[i].Kickoff.Before(ms[j].Kickoff)
	})
}

// MergeByID appends matches that are not already present, keeping live state
// from a previous fetch from being lost during background refreshes.
func MergeByID(dst, src []Match) []Match {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst)+len(src))
	for _, m := range dst {
		seen[m.Key()] = struct{}{}
	}
	for _, m := range src {
		if _, ok := seen[m.Key()]; ok {
			continue
		}
		seen[m.Key()] = struct{}{}
		dst = append(dst, m)
	}
	return dst
}
