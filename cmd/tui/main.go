// Package main provides the TUI configurator entry point for the RFID gateway.
package main

import (
	"fmt"
	"os"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/tui"
	"github.com/amg-rfid/amg-rfid-gateway/internal/version"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Auto-detect config path (fall back to "./config.yaml" on error)
	cfgPath, err := config.AutoDetectPath()
	if err != nil {
		cfgPath = "./config.yaml"
	}

	// Initialize the TUI application
	app, err := tui.NewApp(cfgPath, version.Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing TUI: %v\n", err)
		os.Exit(1)
	}

	// Run the Bubbletea program
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
