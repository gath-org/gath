package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

// Run opens the terminal on network. An empty network is mainnet.
func Run(ctx context.Context, network string) error {
	if network == "" {
		network = DefaultNetwork
	}
	if err := ValidateNetwork(network); err != nil {
		return err
	}
	home, err := DefaultHome()
	if err != nil {
		return err
	}
	session := NewSession(Options{Network: network, Home: home})
	defer session.Close()
	program := tea.NewProgram(New(session), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err = program.Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}
