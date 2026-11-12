package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gath-org/gath/internal/client"
)

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')
	b.WriteString(rule(width))
	b.WriteByte('\n')
	b.WriteString(m.tabs())
	b.WriteString("\n\n")
	if !m.ready && m.screen == screenDashboard && m.nodeErr == "" {
		fmt.Fprintf(&b, "Starting %s node.\n", m.snap.Network)
	} else if m.busy && m.screen != screenWallet {
		b.WriteString("Working.\n")
	} else {
		b.WriteString(m.body(width))
	}
	if m.actionErr != "" {
		b.WriteString("\n\n")
		b.WriteString(m.actionErr)
	} else if m.nodeErr != "" && m.ready {
		b.WriteString("\n\n")
		b.WriteString(m.nodeErr)
	} else if m.note != "" {
		b.WriteString("\n\n")
		b.WriteString(m.note)
	}
	b.WriteString("\n\n")
	b.WriteString(rule(width))
	b.WriteByte('\n')
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	net := m.snap.Network
	if net == "" {
		net = DefaultNetwork
	}
	if !m.ready {
		return fmt.Sprintf("gath  %s  starting", net)
	}
	if m.snap.Status.Tip == "" && m.nodeErr != "" {
		return fmt.Sprintf("gath  %s  node unavailable", net)
	}
	return fmt.Sprintf("gath  %s  height %d  peers %d", net, m.snap.Status.Height, m.snap.Status.Peers)
}

func (m Model) tabs() string {
	names := []struct {
		s    screen
		name string
	}{
		{screenDashboard, "dashboard"},
		{screenWallet, "wallet"},
		{screenMiner, "miner"},
		{screenNetwork, "network"},
	}
	var parts []string
	for _, n := range names {
		if n.s == m.screen {
			parts = append(parts, "> "+n.name)
			continue
		}
		parts = append(parts, "  "+n.name)
	}
	return strings.Join(parts, "    ")
}

func (m Model) footer() string {
	if m.input.Focused() {
		return "enter confirm    esc cancel    tab leave"
	}
	if m.step == stepShowPhrase {
		return "enter continue"
	}
	if m.step == stepSendConfirm {
		return "enter confirm    esc cancel"
	}
	switch m.screen {
	case screenDashboard:
		return "1 dashboard  2 wallet  3 miner  4 network  r reconnect  q quit"
	case screenWallet:
		return "1 dashboard  2 wallet  3 miner  4 network  q quit"
	case screenMiner:
		return "s start  x stop  + threads  - threads    1 dashboard  2 wallet  3 miner  4 network  q quit"
	case screenNetwork:
		return "up down select    enter switch    esc back    q quit"
	default:
		panic(fmt.Sprintf("unhandled screen %d", m.screen))
	}
}

func (m Model) body(width int) string {
	switch m.screen {
	case screenDashboard:
		return dashboardBody(m.snap)
	case screenWallet:
		return m.walletBody(width)
	case screenMiner:
		return minerBody(m.snap)
	case screenNetwork:
		return m.networkBody()
	default:
		panic(fmt.Sprintf("unhandled screen %d", m.screen))
	}
}

func dashboardBody(s Snapshot) string {
	var b strings.Builder
	tip := s.Status.Tip
	if tip == "" {
		tip = "-"
	}
	diff := s.Status.Difficulty
	if diff == "" {
		diff = "-"
	}
	fmt.Fprintf(&b, "%s\n", row("height", fmt.Sprintf("%d", s.Status.Height)))
	fmt.Fprintf(&b, "%s\n", row("tip", tip))
	fmt.Fprintf(&b, "%s\n", row("difficulty", diff))
	fmt.Fprintf(&b, "%s\n", row("mempool", fmt.Sprintf("%d", s.Status.Mempool)))
	fmt.Fprintf(&b, "%s\n", row("peers", fmt.Sprintf("%d", s.Status.Peers)))
	if s.Status.Genesis != "" {
		fmt.Fprintf(&b, "%s\n", row("genesis", s.Status.Genesis))
	}
	if s.Status.Version != "" {
		fmt.Fprintf(&b, "%s\n", row("version", fmt.Sprintf("%s  protocol %d", s.Status.Version, s.Status.Protocol)))
	}
	if s.RPC != "" {
		fmt.Fprintf(&b, "%s\n", row("api", s.RPC))
	}
	if s.Attached {
		b.WriteString(row("node", "attached to an existing node"))
		b.WriteByte('\n')
	} else if s.P2P != "" {
		fmt.Fprintf(&b, "%s\n", row("listen", s.P2P))
	}
	if s.DataDir != "" {
		label := "data"
		if s.Attached {
			label = "wallet"
		}
		fmt.Fprintf(&b, "%s\n", row(label, displayPath(s.DataDir)))
	}

	b.WriteString("\npeers\n")
	if len(s.Peers) == 0 {
		b.WriteString("  none\n")
	} else {
		limit := len(s.Peers)
		if limit > 8 {
			limit = 8
		}
		for _, p := range s.Peers[:limit] {
			dir := "in"
			if p.Outbound {
				dir = "out"
			}
			agent := p.Agent
			if agent == "" {
				agent = "-"
			}
			fmt.Fprintf(&b, "  %s  %s  height %d  %s\n", p.Addr, dir, p.Height, agent)
		}
		if len(s.Peers) > limit {
			fmt.Fprintf(&b, "  and %d more\n", len(s.Peers)-limit)
		}
	}

	b.WriteString("\nproof of work\n")
	state := "stopped"
	if s.Miner.Running {
		state = "running"
	}
	fmt.Fprintf(&b, "%s\n", row("mining", state))
	fmt.Fprintf(&b, "%s\n", row("threads", fmt.Sprintf("%d", s.Miner.Threads)))
	fmt.Fprintf(&b, "%s\n", row("hashrate", FormatHashrate(s.Miner.Hashrate, s.Miner.Measuring)))
	fmt.Fprintf(&b, "%s\n", row("blocks", fmt.Sprintf("%d", s.Miner.Blocks)))
	if s.Miner.LastHash != "" {
		fmt.Fprintf(&b, "%s\n", row("last", shortID(s.Miner.LastHash)))
	}

	b.WriteString("\nwallet\n")
	switch {
	case s.Wallet.Unlocked:
		fmt.Fprintf(&b, "%s\n", row("address", s.Wallet.Address))
		spendable := s.Wallet.Balance.Spendable
		if spendable == "" {
			spendable = "-"
		}
		confirmed := s.Wallet.Balance.Confirmed
		if confirmed == "" {
			confirmed = "-"
		}
		fmt.Fprintf(&b, "%s\n", row("spendable", spendable))
		fmt.Fprintf(&b, "%s\n", row("confirmed", confirmed))
	case s.Wallet.Exists:
		b.WriteString("  locked\n")
	default:
		b.WriteString("  no wallet file\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) walletBody(width int) string {
	var b strings.Builder
	net := m.snap.Network
	if net == "" {
		net = DefaultNetwork
	}
	switch m.step {
	case stepShowPhrase:
		b.WriteString("Write these words down. They are the only backup.\n")
		b.WriteString("The passphrase only encrypts the wallet file on this machine.\n\n")
		b.WriteString(wrapWords(m.phrase, width-2))
		b.WriteString("\n\nPress enter when you have stored them. They will not be shown again.")
		return b.String()
	case stepSendConfirm:
		fmt.Fprintf(&b, "Send %s Gath to %s", m.amount, m.to)
		return b.String()
	case stepCreatePass, stepCreateConfirm, stepRestorePhrase, stepRestorePass, stepUnlock, stepSendTo, stepSendAmount:
		b.WriteString(fieldLabel(m.step))
		b.WriteByte('\n')
		b.WriteString(m.input.View())
		return b.String()
	case stepIdle:
		if m.busy {
			return "Working."
		}
	default:
		panic(fmt.Sprintf("unhandled wallet step %d", m.step))
	}
	if m.snap.Wallet.Unlocked {
		fmt.Fprintf(&b, "%s\n", row("address", m.snap.Wallet.Address))
		fmt.Fprintf(&b, "%s\n", row("spendable", blankDash(m.snap.Wallet.Balance.Spendable)))
		fmt.Fprintf(&b, "%s\n", row("confirmed", blankDash(m.snap.Wallet.Balance.Confirmed)))
		b.WriteString("\nhistory\n")
		b.WriteString(historyText(m.snap.Wallet.History))
		b.WriteString("\ns send    l lock")
		return b.String()
	}
	if m.snap.Wallet.Exists {
		fmt.Fprintf(&b, "Wallet file found for %s.\n\n", net)
		b.WriteString("u unlock")
		return b.String()
	}
	fmt.Fprintf(&b, "No wallet file for %s.\n\n", net)
	b.WriteString("c create\nr restore\n\n")
	b.WriteString("The recovery phrase is the backup. The passphrase only encrypts the file on this machine.")
	return b.String()
}

func fieldLabel(step walletStep) string {
	switch step {
	case stepCreatePass:
		return "Choose a passphrase of at least 8 characters."
	case stepCreateConfirm:
		return "Repeat the passphrase."
	case stepRestorePhrase:
		return "Enter the recovery phrase."
	case stepRestorePass:
		return "Choose a passphrase of at least 8 characters for this file."
	case stepUnlock:
		return "Wallet passphrase."
	case stepSendTo:
		return "Destination address."
	case stepSendAmount:
		return "Amount in Gath."
	default:
		return ""
	}
}

func historyText(rows []client.HistoryTx) string {
	if len(rows) == 0 {
		return "  no transactions yet\n"
	}
	limit := len(rows)
	if limit > 8 {
		limit = 8
	}
	var b strings.Builder
	for _, tx := range rows[:limit] {
		when := "mempool"
		if tx.Confirmed && tx.Time > 0 {
			when = time.Unix(int64(tx.Time), 0).UTC().Format("2006-01-02 15:04")
		} else if tx.Confirmed {
			when = fmt.Sprintf("height %d", tx.Height)
		}
		fmt.Fprintf(&b, "  %s  %s  %s\n", when, formatAtoms(tx.NetAtoms), shortID(tx.TxID))
	}
	return b.String()
}

func (m Model) networkBody() string {
	var b strings.Builder
	b.WriteString("The terminal starts on mainnet. The node, wallet, and miner follow this network.\n\n")
	for i, name := range Networks() {
		mark := "  "
		if i == m.netIndex {
			mark = "> "
		}
		suffix := ""
		if name == m.snap.Network {
			suffix = "  current"
		}
		fmt.Fprintf(&b, "%s%s%s\n", mark, name, suffix)
	}
	return strings.TrimRight(b.String(), "\n")
}

func minerBody(s Snapshot) string {
	var b strings.Builder
	state := "stopped"
	if s.Miner.Running {
		state = "running"
	}
	fmt.Fprintf(&b, "%s\n", row("status", state))
	fmt.Fprintf(&b, "%s\n", row("threads", fmt.Sprintf("%d", s.Miner.Threads)))
	fmt.Fprintf(&b, "%s\n", row("hashrate", FormatHashrate(s.Miner.Hashrate, s.Miner.Measuring)))
	fmt.Fprintf(&b, "%s\n", row("blocks", fmt.Sprintf("%d", s.Miner.Blocks)))
	last := s.Miner.LastHash
	if last == "" {
		last = "-"
	}
	fmt.Fprintf(&b, "%s\n", row("last", shortID(last)))
	pay := s.Miner.Address
	if pay == "" {
		if s.Wallet.Unlocked && s.Wallet.Address != "" {
			pay = s.Wallet.Address
		} else {
			pay = "unlock the wallet on this network"
		}
	}
	fmt.Fprintf(&b, "%s\n", row("pay to", pay))
	if s.Miner.LastError != "" {
		fmt.Fprintf(&b, "\n%s", s.Miner.LastError)
	}
	b.WriteString("\nThe block reward is paid to the unlocked wallet on this network.")
	return b.String()
}

func blankDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func rule(width int) string {
	if width > 76 {
		width = 76
	}
	if width < 20 {
		width = 20
	}
	return strings.Repeat("-", width)
}
