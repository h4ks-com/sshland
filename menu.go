package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	cssh "github.com/charmbracelet/ssh"
)

type selectedAppKey struct{}

type menuModel struct {
	apps     []AppConfig
	cursor   int
	username string
	isNew    bool
	isGuest  bool
	sess     cssh.Session
	renderer *lipgloss.Renderer
}

type keyMap struct {
	Up    key.Binding
	Down  key.Binding
	Enter key.Binding
	Quit  key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter", " "),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
	),
}

func newMenuModel(apps []AppConfig, username string, isNew, isGuest bool, sess cssh.Session, renderer *lipgloss.Renderer) menuModel {
	return menuModel{
		apps:     apps,
		username: username,
		isNew:    isNew,
		isGuest:  isGuest,
		sess:     sess,
		renderer: renderer,
	}
}

func (m menuModel) Init() tea.Cmd {
	return nil
}

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, keys.Down):
			if m.cursor < len(m.apps)-1 {
				m.cursor++
			}

		case key.Matches(msg, keys.Enter):
			if len(m.apps) > 0 {
				m.sess.Context().SetValue(selectedAppKey{}, m.apps[m.cursor])
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	r := m.renderer

	welcomeStyle := r.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("213")).
		Background(lipgloss.Color("236")).
		Padding(0, 2).
		MarginBottom(1)

	hintStyle := r.NewStyle().
		Foreground(lipgloss.Color("214")).
		Background(lipgloss.Color("236")).
		Padding(0, 2).
		MarginBottom(1)

	hintCmdStyle := r.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("222")).
		Background(lipgloss.Color("236"))

	titleStyle := r.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)

	userStyle := r.NewStyle().
		Foreground(lipgloss.Color("241"))

	subtitleStyle := r.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginBottom(1)

	selectedStyle := r.NewStyle().
		Foreground(lipgloss.Color("212")).
		Bold(true)

	normalStyle := r.NewStyle().
		Foreground(lipgloss.Color("252"))

	descStyle := r.NewStyle().
		Foreground(lipgloss.Color("241"))

	helpStyle := r.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	var out string
	if m.isGuest {
		cmd := hintCmdStyle.Render("ssh-copy-id yournick@h4ks.com")
		out += hintStyle.Render("Want to keep a nick? Run: "+cmd) + "\n"
	} else {
		out += welcomeStyle.Render(fmt.Sprintf("Welcome to h4ks.com, %s!", m.username)) + "\n"
	}

	out += titleStyle.Render("sshland") + "\n"
	out += userStyle.Render(fmt.Sprintf("logged in as %s", m.username)) + "\n"

	if m.isNew {
		out += subtitleStyle.Render("· nick registered!") + "\n"
	}

	for i, app := range m.apps {
		cursor := "  "
		var nameRender string
		if i == m.cursor {
			cursor = "> "
			nameRender = selectedStyle.Render(app.Name)
		} else {
			nameRender = normalStyle.Render(app.Name)
		}
		desc := descStyle.Render("  " + app.Description)
		out += cursor + nameRender + "\n" + desc + "\n"
	}

	out += helpStyle.Render("↑/↓ navigate • enter select • q quit")
	return out
}
