// Package provider turns third party football feeds into model.Match values.
//
// Each feed implements the Provider interface, which keeps the TUI decoupled
// from any particular API. Users switch between them at runtime.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"matchtui/internal/model"
)

// httpTimeout bounds a single feed request so a hung endpoint cannot stall the
// whole UI.
const httpTimeout = 25 * time.Second

// httpClient is shared so connection reuse and timeouts stay consistent.
var httpClient = &http.Client{Timeout: httpTimeout}

// userAgent identifies us to the feeds and avoids default-client filtering.
const userAgent = "matchtui/1.0 (football TUI; +https://github.com/)"

// League is one competition a provider can deliver.
type League struct {
	// ID is the provider's identifier for the competition.
	ID string
	// Name is the human readable competition name.
	Name string
	// Season is optional provider-specific season scoping.
	Season string
	// Default marks leagues selected on first run.
	Default bool
}

// Request describes the slice of data the UI wants. From and To are inclusive
// local days; Window is a hint ("live", "upcoming", "results") that lets a
// feed use a cheaper endpoint when it has one.
type Request struct {
	Leagues []string
	From    time.Time
	To      time.Time
	Window  string
}

// Days lists the local calendar days covered by the request.
func (r Request) Days() []time.Time {
	from := startOfDay(r.From)
	to := startOfDay(r.To)
	if to.Before(from) {
		from, to = to, from
	}
	var days []time.Time
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	return days
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Provider is a score source the user can switch between at runtime.
type Provider interface {
	// ID is a stable machine identifier, persisted in the config file.
	ID() string
	// Name is shown in the provider menu.
	Name() string
	// Description explains the feed, its coverage and its limits.
	Description() string
	// NeedsKey reports whether the feed requires an API key.
	NeedsKey() bool
	// KeyPresent reports whether a usable key is configured.
	KeyPresent() bool
	// RateHint returns the shortest refresh interval the feed can sustain, or
	// zero when the feed has no practical limit. The UI never polls faster
	// than this, which protects free daily quotas.
	RateHint() time.Duration
	// Leagues lists the competitions the feed offers.
	Leagues(ctx context.Context) ([]League, error)
	// Fetch returns the matches described by the request.
	Fetch(ctx context.Context, req Request) ([]model.Match, error)
}

// Detailer is an optional capability: providers that can enrich one fixture
// with data the scoreboard feed does not carry (team sheets, for example)
// implement it. Detail is fetched lazily when the user opens a match, so it
// costs one request per match actually inspected rather than one per list load.
type Detailer interface {
	// FetchDetail returns the fixture enriched with extra data. Implementations
	// must tolerate a zero-valued match and are expected to preserve every
	// field of the input they do not augment.
	FetchDetail(ctx context.Context, m model.Match) (model.Match, error)
	// DetailName is the label for the extra data, used in hints ("team sheets").
	DetailName() string
}

// AsDetailer reports whether a provider can enrich fixtures on demand.
func AsDetailer(p Provider) (Detailer, bool) {
	d, ok := p.(Detailer)
	return d, ok
}

// Vault abstracts where API keys live so this package does not depend on the
// config package.
type Vault interface {
	// GetKey returns the stored key for a provider, or an empty string.
	GetKey(providerID string) string
	// SetKey stores a key; an empty value removes it.
	SetKey(providerID, key string)
}

// nullVault is used when no configuration is available.
type nullVault struct{}

func (nullVault) GetKey(string) string  { return "" }
func (nullVault) SetKey(string, string) {}

// Builtin returns every bundled provider in menu order. ESPN comes first
// because it needs no key and covers the widest set of competitions.
func Builtin(v Vault) []Provider {
	if v == nil {
		v = nullVault{}
	}
	return []Provider{
		NewESPN(),
		NewOpenLigaDB(),
		NewAPIFootball(v),
		NewDemo(),
	}
}

// ByID finds a provider by identifier.
func ByID(ps []Provider, id string) (Provider, bool) {
	for _, p := range ps {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// DefaultLeagueIDs returns the IDs of a feed's default competitions.
func DefaultLeagueIDs(ls []League) []string {
	var out []string
	for _, l := range ls {
		if l.Default {
			out = append(out, l.ID)
		}
	}
	if len(out) == 0 {
		for _, l := range ls {
			out = append(out, l.ID)
		}
	}
	return out
}

// SortLeagues keeps a feed's own ordering meaningful: defaults first, then
// alphabetical.
func SortLeagues(ls []League) {
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].Default != ls[j].Default {
			return ls[i].Default
		}
		return ls[i].Name < ls[j].Name
	})
}

// getJSON performs a GET request and decodes the body into out. Feed level
// errors are returned so callers can surface them in the status bar.
func getJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 160 {
			snippet = snippet[:160] + "..."
		}
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, shortURL(url), snippet)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding %s: %w", shortURL(url), err)
	}
	return nil
}

// shortURL trims a URL down to something that fits a status bar.
func shortURL(u string) string {
	const prefix = "https://"
	if len(u) > len(prefix) && u[:len(prefix)] == prefix {
		u = u[len(prefix):]
	}
	if len(u) > 48 {
		return u[:48] + "..."
	}
	return u
}

// asString decodes JSON values that some feeds emit as either a number or a
// string, which is common for scores and seasons.
type asString string

func (s *asString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch t := v.(type) {
	case string:
		*s = asString(t)
	case float64:
		*s = asString(trimFloat(t))
	}
	return nil
}

func (s asString) String() string { return string(s) }

// asInt decodes integers that a feed may also emit as strings.
type asInt int

func (n *asInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = asInt(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return nil // tolerate unexpected shapes rather than failing the fetch
	}
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		*n = asInt(v)
	}
	return nil
}

// Int returns the decoded value.
func (n asInt) Int() int { return int(n) }

// flexTime accepts the several timestamp shapes feeds emit. ESPN in particular
// drops the seconds field ("2026-10-21T16:45Z"), which is RFC3339-ish but not
// RFC3339, so the standard decoder rejects it.
type flexTime struct {
	time.Time
}

func (t *flexTime) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		return t.parse(s)
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		t.Time = time.Unix(int64(f), 0)
		return nil
	}
	return nil
}

func (t *flexTime) parse(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("unrecognised timestamp %q", s)
}

// trimFloat renders whole numbers without a trailing ".0".
func trimFloat(f float64) string {
	i := int64(f)
	if float64(i) == f {
		return fmt.Sprintf("%d", i)
	}
	return fmt.Sprintf("%g", f)
}
