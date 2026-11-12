package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenDashboard screen = iota
	screenWallet
	screenMiner
	screenNetwork
)

type walletStep int

const (
	stepIdle walletStep = iota
	stepCreatePass
	stepCreateConfirm
	stepShowPhrase
	stepRestorePhrase
	stepRestorePass
	stepUnlock
	stepSendTo
	stepSendAmount
	stepSendConfirm
)

// host is the node, wallet, and miner the terminal drives.
type host interface {
	Network() string
	Open(context.Context) (Snapshot, error)
	Reopen(context.Context) (Snapshot, error)
	Poll(context.Context) Snapshot
	Switch(context.Context, string) (Snapshot, error)
	SetThreads(int) MinerStatus
	StartMiner() (MinerStatus, error)
	StopMiner() MinerStatus
	CreateWallet(string) (string, string, error)
	RestoreWallet(string, string) (string, error)
	UnlockWallet(string) (string, error)
	LockWallet()
	Send(context.Context, string, string) (string, error)
}

// Model is the terminal screen.
type Model struct {
	host      host
	screen    screen
	width     int
	height    int
	snap      Snapshot
	step      walletStep
	input     textinput.Model
	draftPass string
	phrase    string
	to        string
	amount    string
	netIndex  int
	actionErr string
	nodeErr   string
	note      string
	busy      bool
	ready     bool
}

type openedMsg struct {
	snap Snapshot
	err  error
}

type pollMsg struct {
	network string
	snap    Snapshot
}

type tickMsg time.Time

type switchedMsg struct {
	snap Snapshot
	err  error
}

type walletReadyMsg struct {
	phrase  string
	address string
	created bool
	err     error
}

type minerMsg struct {
	status MinerStatus
	err    error
}

type sentMsg struct {
	txid string
	err  error
}

// New builds the terminal model. The first screen is the dashboard.
func New(h host) Model {
	network := DefaultNetwork
	if h != nil && h.Network() != "" {
		network = h.Network()
	}
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = ""
	in.CharLimit = 256
	in.Width = 60
	return Model{
		host:     h,
		screen:   screenDashboard,
		width:    80,
		height:   24,
		input:    in,
		snap:     Snapshot{Network: network},
		netIndex: networkIndex(network),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.openCmd(), m.tickCmd())
}

func (m Model) openCmd() tea.Cmd {
	return func() tea.Msg {
		snap, err := m.host.Open(context.Background())
		return openedMsg{snap: snap, err: err}
	}
}

func (m Model) reopenCmd() tea.Cmd {
	return func() tea.Msg {
		snap, err := m.host.Reopen(context.Background())
		return openedMsg{snap: snap, err: err}
	}
}

func (m Model) pollCmd() tea.Cmd {
	network := m.snap.Network
	return func() tea.Msg {
		return pollMsg{network: network, snap: m.host.Poll(context.Background())}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func networkIndex(name string) int {
	for i, n := range Networks() {
		if n == name {
			return i
		}
	}
	return 0
}

func cycle(s screen, dir int) screen {
	n := int(s) + dir
	switch {
	case n < int(screenDashboard):
		return screenNetwork
	case n > int(screenNetwork):
		return screenDashboard
	default:
		return screen(n)
	}
}
