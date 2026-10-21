// Command matchtui is a terminal dashboard for football fixtures: live scores,
// upcoming kick-offs, past results and a watchlist, switching between several
// data providers from inside the app.
package main

import (
	"flag"
	"fmt"
	"os"

	"matchtui/internal/config"
	"matchtui/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print the version and exit")
		color       = flag.String("color", "", "colour scheme: auto, light or dark")
		provider    = flag.String("provider", "", "score provider: espn, openligadb, apifootball or demo")
		refresh     = flag.Int("refresh", 0, "auto refresh interval in seconds (minimum 10)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("matchtui " + ui.Version)
		return
	}

	cfg, cfgPath := config.Load()
	switch *color {
	case config.SchemeAuto, config.SchemeLight, config.SchemeDark:
		cfg.ColorScheme = *color
	case "":
	default:
		fmt.Fprintf(os.Stderr, "matchtui: unknown colour scheme %q (use auto, light or dark)\n", *color)
		os.Exit(2)
	}
	if *provider != "" {
		cfg.Provider = *provider
	}
	if *refresh > 0 {
		cfg.RefreshSeconds = *refresh
	}

	app := ui.New(cfg, cfgPath)
	program := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "matchtui:", err)
		os.Exit(1)
	}

	// Persist anything that changed during the session, such as stars.
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "matchtui: could not save config:", err)
	}
}
