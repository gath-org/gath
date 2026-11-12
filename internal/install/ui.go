package install

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type stage int

const (
	stageConfirm stage = iota
	stageWork
	stageDone
	stageFail
)

// Model is the install screen.
type Model struct {
	prefix string
	source string
	goVer  string
	home   string
	width  int
	stage  stage
	err    string
	result Result
	place  func(context.Context) (Result, error)
}

type placedMsg struct {
	result Result
	err    error
}

// NewModel builds the confirm screen. place runs when the operator confirms.
func NewModel(home, prefix, source, goVer string, place func(context.Context) (Result, error)) Model {
	if prefix == "" {
		prefix = DefaultBinDir(home)
	}
	return Model{
		prefix: prefix,
		source: source,
		goVer:  goVer,
		home:   home,
		width:  80,
		place:  place,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	case placedMsg:
		if msg.err != nil {
			m.stage = stageFail
			m.err = msg.err.Error()
			return m, nil
		}
		m.stage = stageDone
		m.result = msg.result
		m.err = ""
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "enter":
		if m.stage == stageDone || m.stage == stageFail {
			return m, tea.Quit
		}
		if m.stage != stageConfirm || m.place == nil {
			return m, nil
		}
		m.stage = stageWork
		m.err = ""
		place := m.place
		return m, func() tea.Msg {
			res, err := place(context.Background())
			return placedMsg{result: res, err: err}
		}
	default:
		return m, nil
	}
}

func (m Model) View() string {
	width := m.width
	if width <= 0 || width > 76 {
		width = 76
	}
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	b.WriteString("gath install\n")
	b.WriteString(strings.Repeat("-", width))
	b.WriteByte('\n')
	switch m.stage {
	case stageConfirm:
		b.WriteString("Install the gath binary and put it on PATH.\n")
		b.WriteString("gath opens the terminal on mainnet.\n\n")
		fmt.Fprintf(&b, "%-12s%s\n", "source", blank(m.source))
		fmt.Fprintf(&b, "%-12s%s\n", "install to", filepath.Join(m.prefix, "gath"))
		fmt.Fprintf(&b, "%-12s%s\n", "go", blank(m.goVer))
	case stageWork:
		b.WriteString("Building the gath binary.\n")
	case stageDone:
		b.WriteString(Summary(m.result, m.home, m.prefix))
	case stageFail:
		b.WriteString("Install failed.\n\n")
		b.WriteString(m.err)
	default:
		panic(fmt.Sprintf("unhandled install stage %d", m.stage))
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("-", width))
	b.WriteByte('\n')
	switch m.stage {
	case stageConfirm:
		b.WriteString("enter install    q quit")
	case stageWork:
		b.WriteString("building")
	case stageDone, stageFail:
		b.WriteString("enter or q to close")
	default:
		panic(fmt.Sprintf("unhandled install stage %d", m.stage))
	}
	return b.String()
}

// Failed reports that the install was attempted and did not succeed.
func (m Model) Failed() bool { return m.stage == stageFail }

// Err is the install error shown on the failure screen.
func (m Model) Err() error {
	if !m.Failed() {
		return nil
	}
	if m.err == "" {
		return errors.New("install failed")
	}
	return errors.New(m.err)
}

func blank(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
