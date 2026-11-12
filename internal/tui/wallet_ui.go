package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) onWallet(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.step == stepSendConfirm {
		switch k.String() {
		case "enter":
			to := m.to
			amt := m.amount
			m.busy = true
			m.actionErr = ""
			m.note = ""
			return m, func() tea.Msg {
				txid, err := m.host.Send(context.Background(), to, amt)
				return sentMsg{txid: txid, err: err}
			}
		case "esc":
			m.step = stepIdle
			m.to = ""
			m.amount = ""
			m.actionErr = ""
			return m, nil
		default:
			return m, nil
		}
	}
	if m.step != stepIdle {
		return m, nil
	}
	if m.snap.Wallet.Unlocked {
		switch k.String() {
		case "s":
			m.step = stepSendTo
			m.note = ""
			m.actionErr = ""
			return m.focus("destination address", textinput.EchoNormal, 120)
		case "l":
			m.host.LockWallet()
			m.snap.Wallet.Unlocked = false
			m.snap.Wallet.Address = ""
			m.snap.Wallet.Balance.Spendable = ""
			m.snap.Wallet.Balance.Confirmed = ""
			m.snap.Wallet.History = nil
			m.note = "Wallet locked."
			m.actionErr = ""
			return m, m.pollCmd()
		default:
			return m, nil
		}
	}
	if m.snap.Wallet.Exists {
		if k.String() == "u" || k.String() == "enter" {
			m.step = stepUnlock
			m.note = ""
			return m.focus("passphrase", textinput.EchoPassword, 256)
		}
		return m, nil
	}
	switch k.String() {
	case "c":
		m.step = stepCreatePass
		m.note = ""
		return m.focus("passphrase", textinput.EchoPassword, 256)
	case "r":
		m.step = stepRestorePhrase
		m.note = ""
		return m.focus("recovery phrase", textinput.EchoNormal, 512)
	default:
		return m, nil
	}
}
