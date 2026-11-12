package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gath-org/gath/internal/client"
	"github.com/gath-org/gath/internal/wallet"
)

func TestDashboardShowsLiveFacts(t *testing.T) {
	tip := strings.Repeat("ab", 32)
	h := &fakeHost{network: "mainnet", snap: Snapshot{
		Network: "mainnet",
		RPC:     "127.0.0.1:9734",
		P2P:     "0.0.0.0:9733",
		DataDir: "/home/gath/.gath/mainnet",
		Status: client.Status{
			Network:    "mainnet",
			Height:     42,
			Tip:        tip,
			Difficulty: "1.00000000",
			Peers:      2,
			Mempool:    1,
			Version:    "gath",
			Protocol:   1,
			Genesis:    "genesis",
		},
		Peers: []client.Peer{{
			Addr:     "104.207.90.38:9733",
			Agent:    "gath",
			Height:   42,
			Outbound: true,
		}},
		Miner: MinerStatus{Threads: 4},
	}}
	m := New(h)
	m.ready = true
	m.snap = h.snap
	view := m.View()
	for _, want := range []string{
		"mainnet",
		"42",
		tip,
		"1.00000000",
		"peers",
		"104.207.90.38:9733",
		"proof of work",
		"stopped",
		"0 H/s",
		"127.0.0.1:9734",
		"0.0.0.0:9733",
		"> dashboard",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "stable") {
		t.Fatal(view)
	}
}

func TestScreensAndNetworkRoundTrip(t *testing.T) {
	h := &fakeHost{network: "mainnet", snap: Snapshot{
		Network: "mainnet",
		Status:  client.Status{Network: "mainnet", Height: 3, Tip: "aa", Peers: 1},
	}}
	m := readyModel(h)
	m, _ = press(m, "2")
	if m.screen != screenWallet || !strings.Contains(m.View(), "No wallet file") {
		t.Fatalf("wallet screen:\n%s", m.View())
	}
	m, _ = press(m, "3")
	if m.screen != screenMiner || !strings.Contains(m.View(), "hashrate") {
		t.Fatalf("miner screen:\n%s", m.View())
	}
	m, _ = press(m, "4")
	if m.screen != screenNetwork || !strings.Contains(m.View(), "mainnet") {
		t.Fatalf("network screen:\n%s", m.View())
	}
	m, _ = press(m, "down")
	m = run(t, m, "enter")
	if h.network != "devnet" || m.snap.Network != "devnet" {
		t.Fatalf("network %s snap %s switched %v", h.network, m.snap.Network, h.switched)
	}
	if !strings.Contains(m.View(), "devnet") || !strings.Contains(m.View(), "current") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, _ = press(m, "up")
	m = run(t, m, "enter")
	if h.network != "mainnet" || m.snap.Network != "mainnet" {
		t.Fatalf("back on %s snap %s", h.network, m.snap.Network)
	}
	if len(h.switched) != 2 || h.switched[0] != "devnet" || h.switched[1] != "mainnet" {
		t.Fatalf("switched %v", h.switched)
	}
}

func TestMinerKeys(t *testing.T) {
	h := &fakeHost{network: "mainnet", snap: Snapshot{
		Network: "mainnet",
		Wallet:  WalletSnap{Unlocked: true, Address: "gath1qq"},
		Miner:   MinerStatus{Threads: 2},
	}}
	m := readyModel(h)
	m.snap = h.snap
	m, _ = press(m, "3")
	m = run(t, m, "s")
	if h.starts != 1 || !m.snap.Miner.Running {
		t.Fatalf("starts %d running %v err %s", h.starts, m.snap.Miner.Running, m.actionErr)
	}
	m, _ = press(m, "+")
	if m.snap.Miner.Threads != 3 {
		t.Fatalf("threads %d", m.snap.Miner.Threads)
	}
	m, _ = press(m, "x")
	if h.stops != 1 || m.snap.Miner.Running {
		t.Fatalf("stops %d running %v", h.stops, m.snap.Miner.Running)
	}

	locked := &fakeHost{network: "mainnet", snap: Snapshot{Network: "mainnet", Miner: MinerStatus{Threads: 1}}}
	m = readyModel(locked)
	m.snap = locked.snap
	m.screen = screenMiner
	m = run(t, m, "s")
	if locked.starts != 0 || !strings.Contains(m.View(), "unlock the wallet") {
		t.Fatalf("starts %d\n%s", locked.starts, m.View())
	}
}

func TestWalletCreateHidesPhraseAfterAck(t *testing.T) {
	h := &fakeHost{network: "mainnet", snap: Snapshot{Network: "mainnet"}}
	m := readyModel(h)
	m.screen = screenWallet
	m, _ = press(m, "c")
	m.input.SetValue("short")
	m, _ = press(m, "enter")
	want := wallet.ValidatePassphrase("short").Error()
	if m.actionErr != want || h.creates != 0 {
		t.Fatalf("err %q creates %d", m.actionErr, h.creates)
	}
	m.input.SetValue("correct horse")
	m, _ = press(m, "enter")
	if m.step != stepCreateConfirm {
		t.Fatalf("step %d", m.step)
	}
	m.input.SetValue("correct horse other")
	m, _ = press(m, "enter")
	if m.actionErr != "passphrases do not match" {
		t.Fatal(m.actionErr)
	}
	m.input.SetValue("correct horse")
	m = run(t, m, "enter")
	if h.creates != 1 || m.step != stepShowPhrase {
		t.Fatalf("creates %d step %d err %s", h.creates, m.step, m.actionErr)
	}
	view := m.View()
	if !strings.Contains(view, "only backup") || !strings.Contains(view, h.phrase) {
		t.Fatalf("phrase not shown:\n%s", view)
	}
	m, _ = press(m, "enter")
	if strings.Contains(m.View(), h.phrase) {
		t.Fatal("phrase stayed on screen")
	}
	if m.phrase != "" {
		t.Fatal("phrase kept in the model")
	}
}

func TestQuit(t *testing.T) {
	m := readyModel(&fakeHost{network: "mainnet", snap: Snapshot{Network: "mainnet"}})
	_, cmd := press(m, "q")
	if cmd == nil {
		t.Fatal("no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("%T", cmd())
	}
}

func readyModel(h *fakeHost) Model {
	m := New(h)
	m.ready = true
	m.snap = h.snap
	return m
}

func press(m Model, key string) (Model, tea.Cmd) {
	next, cmd := m.Update(keyMsg(key))
	return next.(Model), cmd
}

func run(t *testing.T, m Model, key string) Model {
	t.Helper()
	m, cmd := press(m, key)
	if cmd == nil {
		t.Fatalf("key %s produced no command", key)
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

type fakeHost struct {
	network  string
	snap     Snapshot
	phrase   string
	creates  int
	starts   int
	stops    int
	switched []string
}

func (f *fakeHost) Network() string { return f.network }

func (f *fakeHost) Open(context.Context) (Snapshot, error) { return f.snap, nil }

func (f *fakeHost) Reopen(context.Context) (Snapshot, error) { return f.snap, nil }

func (f *fakeHost) Poll(context.Context) Snapshot { return f.snap }

func (f *fakeHost) Switch(_ context.Context, network string) (Snapshot, error) {
	if err := ValidateNetwork(network); err != nil {
		return Snapshot{}, err
	}
	f.switched = append(f.switched, network)
	f.network = network
	f.snap.Network = network
	f.snap.Status.Network = network
	f.snap.Wallet = WalletSnap{}
	f.snap.Miner = MinerStatus{}
	return f.snap, nil
}

func (f *fakeHost) SetThreads(n int) MinerStatus {
	if n < minThreads {
		n = minThreads
	}
	if n > maxThreads {
		n = maxThreads
	}
	f.snap.Miner.Threads = n
	return f.snap.Miner
}

func (f *fakeHost) StartMiner() (MinerStatus, error) {
	if !f.snap.Wallet.Unlocked || f.snap.Wallet.Address == "" {
		return f.snap.Miner, errNoWallet
	}
	f.starts++
	f.snap.Miner.Running = true
	f.snap.Miner.Address = f.snap.Wallet.Address
	return f.snap.Miner, nil
}

func (f *fakeHost) StopMiner() MinerStatus {
	f.stops++
	f.snap.Miner.Running = false
	return f.snap.Miner
}

func (f *fakeHost) CreateWallet(pass string) (string, string, error) {
	if err := wallet.ValidatePassphrase(pass); err != nil {
		return "", "", err
	}
	f.creates++
	f.phrase = "alpha bravo charlie delta echo foxtrot golf hotel"
	f.snap.Wallet.Exists = true
	f.snap.Wallet.Unlocked = true
	f.snap.Wallet.Address = "gath1qqqq"
	return f.phrase, f.snap.Wallet.Address, nil
}

func (f *fakeHost) RestoreWallet(string, string) (string, error) {
	return "", nil
}

func (f *fakeHost) UnlockWallet(string) (string, error) { return "", nil }

func (f *fakeHost) LockWallet() {
	f.snap.Wallet.Unlocked = false
}

func (f *fakeHost) Send(context.Context, string, string) (string, error) {
	return "", nil
}
