package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gath-org/gath/internal/amount"
	"github.com/gath-org/gath/internal/wallet"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if msg.Width > 8 {
			m.input.Width = msg.Width - 4
		}
		if m.input.Width > 72 {
			m.input.Width = 72
		}
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	case openedMsg:
		m.busy = false
		m.ready = true
		if msg.snap.Network != "" {
			m.snap = msg.snap
			m.netIndex = networkIndex(msg.snap.Network)
		}
		m.nodeErr = ""
		if msg.err != nil {
			m.nodeErr = msg.err.Error()
		} else if msg.snap.NodeErr != "" {
			m.nodeErr = msg.snap.NodeErr
		}
		return m, m.pollCmd()
	case pollMsg:
		if !m.ready || (msg.network != "" && msg.network != m.snap.Network) {
			return m, nil
		}
		m.snap = msg.snap
		m.netIndex = networkIndex(msg.snap.Network)
		m.nodeErr = msg.snap.NodeErr
		return m, nil
	case tickMsg:
		if !m.ready {
			return m, m.tickCmd()
		}
		return m, tea.Batch(m.pollCmd(), m.tickCmd())
	case switchedMsg:
		m.busy = false
		m.ready = true
		m.step = stepIdle
		m.phrase = ""
		m.draftPass = ""
		m.to = ""
		m.amount = ""
		m = m.blur()
		if msg.err != nil {
			m.actionErr = msg.err.Error()
			m.note = ""
			if msg.snap.Network != "" {
				m.snap = msg.snap
				m.netIndex = networkIndex(msg.snap.Network)
			}
			m.nodeErr = msg.snap.NodeErr
			return m, nil
		}
		m.snap = msg.snap
		m.netIndex = networkIndex(msg.snap.Network)
		m.nodeErr = msg.snap.NodeErr
		m.actionErr = ""
		m.note = "On " + msg.snap.Network + "."
		return m, m.pollCmd()
	case walletReadyMsg:
		m.busy = false
		if msg.err != nil {
			m.actionErr = msg.err.Error()
			m.note = ""
			if msg.created && msg.phrase != "" {
				m.phrase = msg.phrase
				m.step = stepShowPhrase
				m.screen = screenWallet
				return m, m.pollCmd()
			}
			m.step = stepIdle
			return m, nil
		}
		m.actionErr = ""
		if msg.created {
			m.phrase = msg.phrase
			m.step = stepShowPhrase
			m.screen = screenWallet
			m.note = ""
			return m, m.pollCmd()
		}
		m.step = stepIdle
		m.note = "Wallet unlocked."
		return m, m.pollCmd()
	case minerMsg:
		m.busy = false
		m.snap.Miner = msg.status
		if msg.err != nil {
			m.actionErr = msg.err.Error()
			m.note = ""
			return m, nil
		}
		m.actionErr = ""
		if msg.status.Running {
			m.note = "Miner running."
		} else {
			m.note = "Miner stopped."
		}
		return m, nil
	case sentMsg:
		m.busy = false
		m.step = stepIdle
		m.to = ""
		m.amount = ""
		if msg.err != nil {
			m.actionErr = msg.err.Error()
			m.note = ""
			return m, nil
		}
		m.actionErr = ""
		m.note = "Submitted " + msg.txid
		return m, m.pollCmd()
	default:
		return m, nil
	}
}

func (m Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	}
	if m.busy {
		return m, nil
	}
	if m.step == stepShowPhrase {
		if k.String() == "enter" {
			m.phrase = ""
			m.step = stepIdle
			m.note = "Wallet unlocked."
			return m, m.pollCmd()
		}
		return m, nil
	}
	if m.input.Focused() {
		switch k.String() {
		case "esc":
			m.step = stepIdle
			m.draftPass = ""
			m.phrase = ""
			m.to = ""
			m.amount = ""
			m.actionErr = ""
			return m.blur(), nil
		case "enter":
			return m.submitField()
		case "tab":
			m.step = stepIdle
			m.draftPass = ""
			m = m.blur()
			m.screen = cycle(m.screen, 1)
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(k)
			return m, cmd
		}
	}
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "tab":
		m.screen = cycle(m.screen, 1)
		return m, nil
	case "shift+tab":
		m.screen = cycle(m.screen, -1)
		return m, nil
	case "1":
		m.screen = screenDashboard
		return m, nil
	case "2":
		m.screen = screenWallet
		return m, nil
	case "3":
		m.screen = screenMiner
		return m, nil
	case "4":
		m.screen = screenNetwork
		return m, nil
	case "r":
		if m.screen == screenDashboard {
			m.ready = false
			m.nodeErr = ""
			m.actionErr = ""
			m.note = ""
			return m, m.reopenCmd()
		}
	}
	switch m.screen {
	case screenDashboard:
		return m, nil
	case screenWallet:
		return m.onWallet(k)
	case screenMiner:
		return m.onMiner(k)
	case screenNetwork:
		return m.onNetwork(k)
	default:
		panic(fmt.Sprintf("unhandled screen %d", m.screen))
	}
}

func (m Model) onMiner(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "s":
		m.busy = true
		m.actionErr = ""
		m.note = ""
		return m, func() tea.Msg {
			st, err := m.host.StartMiner()
			return minerMsg{status: st, err: err}
		}
	case "x":
		m.snap.Miner = m.host.StopMiner()
		m.actionErr = ""
		m.note = "Miner stopped."
		return m, nil
	case "+", "=":
		m.snap.Miner = m.host.SetThreads(m.snap.Miner.Threads + 1)
		m.actionErr = ""
		return m, nil
	case "-", "_":
		m.snap.Miner = m.host.SetThreads(m.snap.Miner.Threads - 1)
		m.actionErr = ""
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) onNetwork(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	nets := Networks()
	switch k.String() {
	case "up", "k":
		if m.netIndex > 0 {
			m.netIndex--
		}
		return m, nil
	case "down", "j":
		if m.netIndex < len(nets)-1 {
			m.netIndex++
		}
		return m, nil
	case "enter":
		if m.netIndex < 0 || m.netIndex >= len(nets) {
			return m, nil
		}
		name := nets[m.netIndex]
		if name == m.snap.Network && m.nodeErr == "" && m.ready {
			m.note = "Already on " + name + "."
			m.actionErr = ""
			return m, nil
		}
		m.busy = true
		m.actionErr = ""
		m.note = "Switching to " + name + "."
		m.step = stepIdle
		m.phrase = ""
		m.draftPass = ""
		m.to = ""
		m.amount = ""
		m = m.blur()
		return m, func() tea.Msg {
			snap, err := m.host.Switch(context.Background(), name)
			return switchedMsg{snap: snap, err: err}
		}
	case "esc":
		m.screen = screenDashboard
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) focus(placeholder string, echo textinput.EchoMode, limit int) (Model, tea.Cmd) {
	m.input.Blur()
	m.input.Reset()
	m.input.Prompt = ""
	m.input.Placeholder = placeholder
	m.input.EchoMode = echo
	m.input.CharLimit = limit
	if m.width > 8 {
		m.input.Width = m.width - 4
	}
	if m.input.Width > 72 {
		m.input.Width = 72
	}
	if m.input.Width < 20 {
		m.input.Width = 20
	}
	m.actionErr = ""
	return m, m.input.Focus()
}

func (m Model) blur() Model {
	m.input.Blur()
	m.input.Reset()
	m.input.Placeholder = ""
	return m
}

func (m Model) submitField() (tea.Model, tea.Cmd) {
	value := m.input.Value()
	switch m.step {
	case stepCreatePass:
		if err := wallet.ValidatePassphrase(value); err != nil {
			m.actionErr = err.Error()
			return m, nil
		}
		m.draftPass = value
		m.step = stepCreateConfirm
		return m.focus("repeat passphrase", textinput.EchoPassword, 256)
	case stepCreateConfirm:
		if value != m.draftPass {
			m.actionErr = "passphrases do not match"
			return m, nil
		}
		pass := m.draftPass
		m.draftPass = ""
		m.busy = true
		m = m.blur()
		m.step = stepIdle
		return m, func() tea.Msg {
			phrase, addr, err := m.host.CreateWallet(pass)
			return walletReadyMsg{phrase: phrase, address: addr, created: true, err: err}
		}
	case stepRestorePhrase:
		phrase := cleanPhrase(value)
		if phrase == "" {
			m.actionErr = "enter the recovery phrase"
			return m, nil
		}
		m.phrase = phrase
		m.step = stepRestorePass
		return m.focus("passphrase", textinput.EchoPassword, 256)
	case stepRestorePass:
		if err := wallet.ValidatePassphrase(value); err != nil {
			m.actionErr = err.Error()
			return m, nil
		}
		phrase := m.phrase
		m.phrase = ""
		m.busy = true
		m = m.blur()
		m.step = stepIdle
		return m, func() tea.Msg {
			addr, err := m.host.RestoreWallet(phrase, value)
			return walletReadyMsg{address: addr, err: err}
		}
	case stepUnlock:
		if value == "" {
			m.actionErr = "enter the passphrase"
			return m, nil
		}
		m.busy = true
		m = m.blur()
		m.step = stepIdle
		return m, func() tea.Msg {
			addr, err := m.host.UnlockWallet(value)
			return walletReadyMsg{address: addr, err: err}
		}
	case stepSendTo:
		to := strings.TrimSpace(value)
		if to == "" {
			m.actionErr = "enter a destination address"
			return m, nil
		}
		m.to = to
		m.step = stepSendAmount
		return m.focus("amount in Gath", textinput.EchoNormal, 32)
	case stepSendAmount:
		parsed, err := amount.Parse(value)
		if err != nil {
			m.actionErr = err.Error()
			return m, nil
		}
		m.amount = amount.Format(parsed)
		m.step = stepSendConfirm
		m = m.blur()
		m.actionErr = ""
		return m, nil
	default:
		return m, nil
	}
}
