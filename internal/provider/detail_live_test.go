package provider

import (
	"context"
	"testing"
	"time"
)

// TestESPNDetailLive fetches a real finished fixture and asks ESPN for its team
// sheets, which is the only way to prove the summary endpoint mapping.
func TestESPNDetailLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := NewESPN()
	req := Request{
		Leagues: []string{"eng.1"},
		From:    time.Now().AddDate(0, 0, -7),
		To:      time.Now().AddDate(0, 0, -1),
		Window:  "results",
	}
	matches, err := p.Fetch(ctx, req)
	if err != nil {
		t.Fatalf("espn fetch: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no real matches in this window")
	}

	var target = matches[0]
	for _, m := range matches {
		if m.State.Finished() {
			target = m
			break
		}
	}

	d, ok := AsDetailer(p)
	if !ok {
		t.Fatal("espn should implement Detailer")
	}
	enriched, err := d.FetchDetail(ctx, target)
	if err != nil {
		t.Fatalf("espn detail: %v", err)
	}
	if len(enriched.Lineups) != 2 {
		t.Fatalf("espn returned %d team sheets, want 2 (%s v %s)",
			len(enriched.Lineups), target.Home.Name, target.Away.Name)
	}
	for _, l := range enriched.Lineups {
		t.Logf("%s (%s): %d starters, %d bench",
			l.TeamName, l.Formation, len(l.Starters), len(l.Bench))
		if len(l.Starters) == 0 {
			t.Errorf("%s has no starters", l.TeamName)
		}
		if l.TeamID == "" {
			t.Errorf("%s has no team id, so it cannot be matched to a side", l.TeamName)
		}
		for _, pl := range l.Starters {
			if pl.Name == "" {
				t.Errorf("%s has an unnamed starter", l.TeamName)
			}
		}
		t.Logf("  first XI: %s", l.Starters[0].Name)
	}
}
