// Package config loads and persists user settings.
//
// The file lives in the platform config directory (%AppData% on Windows,
// ~/.config elsewhere) so the app keeps working across working directories.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const appName = "matchtui"

// Colour schemes accepted by the UI.
const (
	SchemeAuto  = "auto"
	SchemeLight = "light"
	SchemeDark  = "dark"
)

// Config is the persisted application state.
type Config struct {
	// Provider is the ID of the score source to use on startup.
	Provider string `json:"provider"`
	// RefreshSeconds is the auto refresh cadence for live scores.
	RefreshSeconds int `json:"refresh_seconds"`
	// UpcomingDays is how many days of future fixtures to load.
	UpcomingDays int `json:"upcoming_days"`
	// ResultsDays is how many days of past results to load.
	ResultsDays int `json:"results_days"`
	// Leagues maps a provider ID to the league IDs the user enabled.
	Leagues map[string][]string `json:"leagues"`
	// APIKeys holds keys for providers that need one.
	APIKeys map[string]string `json:"api_keys,omitempty"`
	// Starred holds the identity keys of matches the user is following.
	Starred []string `json:"starred,omitempty"`
	// ColorScheme is one of auto, light or dark.
	ColorScheme string `json:"color_scheme"`
}

// Default returns the first-run configuration.
func Default() *Config {
	return &Config{
		Provider:       "espn",
		RefreshSeconds: 30,
		UpcomingDays:   5,
		ResultsDays:    5,
		Leagues:        map[string][]string{},
		APIKeys:        map[string]string{},
		ColorScheme:    SchemeAuto,
	}
}

// Path returns the config file location for this platform.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "config.json"), nil
}

// Load reads the configuration. A missing or corrupt file simply yields the
// defaults; the app must never fail to start over settings.
func Load() (*Config, string) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return cfg, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, path
	}
	// Start from defaults so fields absent from an older file stay valid.
	filled := Default()
	if err := json.Unmarshal(raw, filled); err != nil {
		return cfg, path
	}
	cfg = filled
	cfg.normalise()
	return cfg, path
}

// Save writes the configuration atomically so a crash mid-write cannot corrupt
// it.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	c.normalise()
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// normalise clamps values into ranges the UI can render.
func (c *Config) normalise() {
	if c.RefreshSeconds < 10 {
		c.RefreshSeconds = 10
	}
	if c.RefreshSeconds > 3600 {
		c.RefreshSeconds = 3600
	}
	if c.UpcomingDays < 1 {
		c.UpcomingDays = 1
	}
	if c.UpcomingDays > 14 {
		c.UpcomingDays = 14
	}
	if c.ResultsDays < 1 {
		c.ResultsDays = 1
	}
	if c.ResultsDays > 14 {
		c.ResultsDays = 14
	}
	switch c.ColorScheme {
	case SchemeAuto, SchemeLight, SchemeDark:
	default:
		c.ColorScheme = SchemeAuto
	}
	if c.Provider == "" {
		c.Provider = "espn"
	}
	if c.Leagues == nil {
		c.Leagues = map[string][]string{}
	}
	if c.APIKeys == nil {
		c.APIKeys = map[string]string{}
	}
}

// SelectedLeagues returns the league IDs enabled for a provider. An empty slice
// means "let the provider decide", which is its own default set.
func (c *Config) SelectedLeagues(providerID string) []string {
	if c.Leagues == nil {
		return nil
	}
	return c.Leagues[providerID]
}

// SetSelectedLeagues stores the league IDs enabled for a provider.
func (c *Config) SetSelectedLeagues(providerID string, ids []string) {
	if c.Leagues == nil {
		c.Leagues = map[string][]string{}
	}
	c.Leagues[providerID] = ids
}

// GetKey implements provider.Vault.
func (c *Config) GetKey(providerID string) string {
	if c.APIKeys == nil {
		return ""
	}
	return strings.TrimSpace(c.APIKeys[providerID])
}

// SetKey implements provider.Vault.
func (c *Config) SetKey(providerID, key string) {
	if c.APIKeys == nil {
		c.APIKeys = map[string]string{}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		delete(c.APIKeys, providerID)
		return
	}
	c.APIKeys[providerID] = key
}

// ToggleStar adds or removes a match from the watchlist and reports whether it
// is now starred.
func (c *Config) ToggleStar(key string) bool {
	for i, k := range c.Starred {
		if k == key {
			c.Starred = append(c.Starred[:i], c.Starred[i+1:]...)
			return false
		}
	}
	c.Starred = append(c.Starred, key)
	return true
}

// IsStarred reports whether a match identity is on the watchlist.
func (c *Config) IsStarred(key string) bool {
	for _, k := range c.Starred {
		if k == key {
			return true
		}
	}
	return false
}
