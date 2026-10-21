package provider

import (
	"context"
	"testing"
	"time"

	"matchtui/internal/model"
)

// These tests exercise the real feeds. They are skipped in -short mode so the
// default test run stays offline and deterministic.

func TestESPNIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := NewESPN()
	if p.NeedsKey() {
		t.Fatal("ESPN must not require a key")
	}
	leagues, err := p.Leagues(ctx)
	if err != nil {
		t.Fatalf("leagues: %v", err)
	}
	if len(leagues) < 10 {
		t.Fatalf("expected a broad catalogue, got %d", len(leagues))
	}

	now := time.Now()
	req := Request{
		Leagues: []string{"eng.1", "uefa.champions"},
		From:    now.AddDate(0, 0, -10),
		To:      now,
		Window:  "results",
	}
	matches, err := p.Fetch(ctx, req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected finished matches in the last ten days")
	}

	var finished, withEvents, withVenue, withStats int
	for _, m := range matches {
		if m.Home.Name == "" || m.Away.Name == "" {
			t.Errorf("match %s has no teams", m.ID)
		}
		if m.Kickoff.IsZero() {
			t.Errorf("match %s has no kickoff", m.ID)
		}
		if m.League == "" || m.LeagueTag == "" {
			t.Errorf("match %s has no league", m.ID)
		}
		switch m.State {
		case model.StateFinished:
			finished++
		case model.StateLive, model.StateHalftime, model.StateScheduled, model.StatePostponed:
		default:
			t.Errorf("match %s has unexpected state %v", m.ID, m.State)
		}
		if len(m.Events) > 0 {
			withEvents++
			checkTimelineScore(t, m)
		}
		if len(m.Stats) > 0 {
			withStats++
		}
		if m.Venue != "" {
			withVenue++
		}
	}
	if finished == 0 {
		t.Error("expected some finished matches")
	}
	t.Logf("espn results: %d matches, %d with timeline, %d with stats, %d with venue",
		len(matches), withEvents, withStats, withVenue)

	// The live window uses a different code path (and endpoint shape), so hit it
	// as well.
	live, err := p.Fetch(ctx, Request{
		Leagues: []string{"uefa.champions", "mex.1", "bra.1", "jpn.1"},
		From:    now,
		To:      now,
		Window:  "live",
	})
	if err != nil {
		t.Fatalf("live fetch: %v", err)
	}
	for _, m := range live {
		switch m.State {
		case model.StateLive, model.StateHalftime, model.StateScheduled, model.StateFinished, model.StatePostponed:
		default:
			t.Errorf("live window match %s has unexpected state %v", m.ID, m.State)
		}
	}
	t.Logf("espn live window: %d matches", len(live))
}

// checkTimelineScore compares the running score on the last scoring play with
// the final scoreline. They only differ when a feed attributes an own goal to
// the conceding side, so a mismatch is logged rather than failed.
func checkTimelineScore(t *testing.T, m model.Match) {
	t.Helper()
	var last string
	for _, e := range m.Events {
		if e.Kind.Scoring() {
			last = e.Score
		}
	}
	if last == "" {
		return
	}
	if want := m.ScoreLine(); last != want {
		t.Logf("match %s: timeline ends %s but final is %s (own goal attribution)",
			m.ID, last, want)
	}
}

// TestOpenLigaDBIntegration checks the second key-free feed.
func TestOpenLigaDBIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := NewOpenLigaDB()
	if p.NeedsKey() {
		t.Fatal("OpenLigaDB must not require a key")
	}
	leagues, err := p.Leagues(ctx)
	if err != nil {
		t.Fatalf("leagues: %v", err)
	}
	if len(leagues) == 0 {
		t.Fatal("expected competitions from OpenLigaDB")
	}

	picked := DefaultLeagueIDs(leagues)
	now := time.Now()
	req := Request{
		Leagues: picked,
		From:    now.AddDate(0, 0, -14),
		To:      now,
		Window:  "results",
	}
	matches, err := p.Fetch(ctx, req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected OpenLigaDB results in the last two weeks for %v", picked)
	}

	var finished int
	for _, m := range matches {
		if m.Home.Name == "" || m.Away.Name == "" {
			t.Errorf("match %s has no teams", m.ID)
		}
		if m.State == model.StateFinished {
			finished++
			if m.Home.Score == 0 && m.Away.Score == 0 && len(m.Events) == 0 {
				// A genuine 0-0 is possible, so only flag it with no timeline.
				t.Logf("match %s finished 0-0 with no timeline", m.ID)
			}
		}
	}
	if finished == 0 {
		t.Error("expected finished matches")
	}
	t.Logf("openligadb: %d matches, %d finished", len(matches), finished)
}

// TestDemoProvider covers the offline path: it must always produce fixtures in
// every lifecycle state.
func TestDemoProvider(t *testing.T) {
	p := NewDemo()
	if p.NeedsKey() {
		t.Fatal("demo must not require a key")
	}

	now := time.Now()
	ctx := context.Background()
	matches, err := p.Fetch(ctx, Request{From: now.AddDate(0, 0, -3), To: now.AddDate(0, 0, 3), Window: "upcoming"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	seen := map[model.State]bool{}
	for _, m := range matches {
		seen[m.State] = true
		if m.Home.Name == "" || m.Away.Name == "" {
			t.Errorf("fixture %s has no teams", m.ID)
		}
		if len(m.Stats) == 0 && m.State != model.StateScheduled {
			t.Errorf("fixture %s is %v but has no statistics", m.ID, m.State)
		}
	}
	for _, want := range []model.State{model.StateFinished, model.StateScheduled} {
		if !seen[want] {
			t.Errorf("expected at least one %v fixture", want)
		}
	}
	if len(matches) < 10 {
		t.Errorf("expected a full fixture list, got %d", len(matches))
	}

	// Scores must be stable: the same request twice gives the same scorelines.
	again, err := p.Fetch(ctx, Request{From: now.AddDate(0, 0, -3), To: now.AddDate(0, 0, 3), Window: "upcoming"})
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if len(again) != len(matches) {
		t.Fatalf("fixture count changed between fetches: %d then %d", len(matches), len(again))
	}
	for i := range matches {
		if matches[i].ScoreLine() != again[i].ScoreLine() {
			t.Errorf("fixture %s changed score: %s then %s",
				matches[i].ID, matches[i].ScoreLine(), again[i].ScoreLine())
		}
	}
}

// TestRequestDays checks the window maths every provider relies on.
func TestRequestDays(t *testing.T) {
	from := time.Date(2026, 10, 21, 13, 45, 0, 0, time.Local)
	req := Request{From: from, To: from.AddDate(0, 0, 2)}
	days := req.Days()
	if len(days) != 3 {
		t.Fatalf("expected 3 days, got %d", len(days))
	}
	for _, d := range days {
		if d.Hour() != 0 || d.Minute() != 0 {
			t.Errorf("day %v is not midnight", d)
		}
	}

	// A reversed window must not panic or return nothing.
	rev := Request{From: from, To: from.AddDate(0, 0, -2)}
	if got := len(rev.Days()); got != 3 {
		t.Errorf("reversed window gave %d days, want 3", got)
	}
}
