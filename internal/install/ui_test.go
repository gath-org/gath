package install

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInstallScreenConfirmsThenFinishes(t *testing.T) {
	calls := 0
	m := NewModel("/home/gath", "/home/gath/.local/bin", "/src/gath", "go version go1.24.2 linux/amd64", func(context.Context) (Result, error) {
		calls++
		return Result{BinPath: "/home/gath/.local/bin/gath", OnPath: true}, nil
	})
	view := m.View()
	for _, want := range []string{"mainnet", "/home/gath/.local/bin/gath", "go version go1.24.2", "enter install"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q\n%s", want, view)
		}
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("%T", cmd())
	}
	if calls != 0 {
		t.Fatal("quit installed")
	}
	m = next.(Model)
	m, cmd = press(m, "enter")
	if cmd == nil || calls != 0 {
		t.Fatal("enter should schedule the install")
	}
	msg := cmd()
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	next, _ = m.Update(msg)
	m = next.(Model)
	done := m.View()
	if !strings.Contains(done, "Installed /home/gath/.local/bin/gath") || !strings.Contains(done, "mainnet") {
		t.Fatalf("%s", done)
	}
	if m.Failed() {
		t.Fatal("success marked failed")
	}
}

func TestInstallScreenShowsFailure(t *testing.T) {
	m := NewModel("/home/gath", "", "/src", "go1.24.2", func(context.Context) (Result, error) {
		return Result{}, errBuild
	})
	if !strings.Contains(m.View(), "/home/gath/.local/bin/gath") {
		t.Fatal(m.View())
	}
	m, cmd := press(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !m.Failed() || !strings.Contains(m.View(), "build failed") {
		t.Fatalf("%s", m.View())
	}
}

var errBuild = errString("build failed")

type errString string

func (e errString) Error() string { return string(e) }

func press(m Model, key string) (Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "q":
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}
