package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	cssh "github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

type selectedAppKey struct{}

type tickMsg struct{}
type authResultMsg AuthResult

type loginWaitState struct {
	url        string
	spinnerIdx int
	errMsg     string
}

var spinnerFrames = []string{"◌", "◍", "◎", "●", "◎", "◍"}

// menuItem wraps an AppConfig with disabled state for agent-gated apps.
type menuItem struct {
	app      AppConfig
	disabled bool
	reason   string
}

type menuModel struct {
	apps           []AppConfig
	cursor         int
	username       string
	isNew          bool
	isGuest        bool
	agentAvailable bool
	sess           cssh.Session
	renderer       *lipgloss.Renderer
	loginCfg       *LogtoConfig
	pendingMgr     *PendingAuthManager
	publicKey      gossh.PublicKey
	loginState     *loginWaitState
	width          int
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

func newMenuModel(apps []AppConfig, username string, isNew, isGuest bool, sess cssh.Session, renderer *lipgloss.Renderer, loginCfg *LogtoConfig, pendingMgr *PendingAuthManager, publicKey gossh.PublicKey, agentAvailable bool) menuModel {
	return menuModel{
		apps:           apps,
		username:       username,
		isNew:          isNew,
		isGuest:        isGuest,
		agentAvailable: agentAvailable,
		sess:           sess,
		renderer:       renderer,
		loginCfg:       loginCfg,
		pendingMgr:     pendingMgr,
		publicKey:      publicKey,
	}
}

// visibleApps filters auth-required apps for guests, shows agent-required apps
// as disabled when agent forwarding is unavailable, and prepends login / appends
// logout when Logto is configured.
func (m menuModel) visibleApps() []menuItem {
	var items []menuItem
	if m.loginCfg != nil && m.isGuest {
		items = append(items, menuItem{app: AppConfig{Name: "login", Description: "Authenticate to claim your nick"}})
	}
	for _, a := range m.apps {
		if a.RequiresAuth && m.isGuest {
			continue
		}
		if a.RequiresAgent && !m.agentAvailable {
			items = append(items, menuItem{app: a, disabled: true, reason: "requires ssh -A"})
		} else {
			items = append(items, menuItem{app: a})
		}
	}
	if m.loginCfg != nil && !m.isGuest {
		items = append(items, menuItem{app: AppConfig{Name: "logout", Description: "Remove this key's login and sign out"}})
	}
	return items
}

func waitForAuthCmd(ch chan AuthResult) tea.Cmd {
	return func() tea.Msg {
		return authResultMsg(<-ch)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m menuModel) Init() tea.Cmd {
	if m.loginState != nil {
		return tickCmd()
	}
	return nil
}

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tickMsg:
		if m.loginState != nil {
			m.loginState.spinnerIdx = (m.loginState.spinnerIdx + 1) % len(spinnerFrames)
			return m, tickCmd()
		}
		return m, nil

	case authResultMsg:
		if m.loginState == nil {
			// User cancelled before auth completed.
			return m, nil
		}
		if msg.Err != nil {
			m.loginState.errMsg = msg.Err.Error()
			return m, nil
		}
		m.username = msg.Username
		m.isGuest = false
		m.loginState = nil
		// Write the authenticated username into the session context so that
		// makeAppMiddleware uses the Logto username, not the original SSH username.
		m.sess.Context().SetValue(usernameKey{}, msg.Username)
		m.sess.Context().SetValue(isGuestKey{}, false)
		return m, tea.ClearScreen

	case tea.KeyMsg:
		if m.loginState != nil {
			// Only allow cancellation while waiting for auth.
			if key.Matches(msg, keys.Quit) {
				m.loginState = nil
				return m, tea.ClearScreen
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, keys.Down):
			items := m.visibleApps()
			if m.cursor < len(items)-1 {
				m.cursor++
			}

		case key.Matches(msg, keys.Enter):
			items := m.visibleApps()
			if len(items) > 0 {
				item := items[m.cursor]
				if item.disabled {
					return m, nil
				}
				selected := item.app
				if selected.Name == "login" {
					state := newRandomState()
					ch := m.pendingMgr.Register(state, m.publicKey)
					m.loginState = &loginWaitState{
						url: m.loginCfg.BuildAuthURL(state),
					}
					return m, tea.Batch(waitForAuthCmd(ch), tickCmd(), tea.ClearScreen)
				}
				if selected.Name == "logout" {
					if m.publicKey != nil && m.loginCfg != nil {
						_ = deleteIdentity(m.loginCfg.IdentitiesDir, m.publicKey)
					}
					m.sess.Context().SetValue(usernameKey{}, sshuserName(m.publicKey))
					m.sess.Context().SetValue(isGuestKey{}, true)
					return m, tea.Quit
				}
				m.sess.Context().SetValue(selectedAppKey{}, selected)
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	if m.loginState != nil {
		return m.loginView()
	}
	return m.menuView()
}

func (m menuModel) loginView() string {
	r := m.renderer
	s := m.loginState

	titleStyle := r.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)

	urlStyle := r.NewStyle().
		Foreground(lipgloss.Color("39"))

	spinnerStyle := r.NewStyle().
		Foreground(lipgloss.Color("212"))

	helpStyle := r.NewStyle().
		Foreground(lipgloss.Color("241"))

	errStyle := r.NewStyle().
		Foreground(lipgloss.Color("196")).
		MarginTop(1)

	// Pre-wrap at terminal width so BubbleTea tracks the correct view height.
	// Without explicit \n, soft-wrap makes BubbleTea undercount lines and
	// re-render from the wrong position, leaving ghost frames on screen.
	// The OSC 8 hyperlink on the wrapped display handles click-to-open.
	// The raw URL is shown below the link as a plain copiable line.
	lineWidth := m.width
	if lineWidth < 72 {
		lineWidth = 72
	}

	var out string
	out += titleStyle.Render("Authenticate with Logto") + "\n\n"
	out += "  Open in browser:\n"
	out += osc8Link(s.url, urlStyle.Render(chunkURL(s.url, lineWidth))) + "\n\n"

	if s.errMsg != "" {
		out += errStyle.Render("Error: "+s.errMsg) + "\n"
	} else {
		spinner := spinnerFrames[s.spinnerIdx]
		out += "  " + spinnerStyle.Render("Waiting "+spinner) + "  " + helpStyle.Render("(q to cancel)") + "\n"
	}

	return out
}

func (m menuModel) menuView() string {
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

	disabledStyle := r.NewStyle().
		Foreground(lipgloss.Color("255")).
		Italic(true)

	disabledDescStyle := r.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true)

	descStyle := r.NewStyle().
		Foreground(lipgloss.Color("241"))

	helpStyle := r.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	items := m.visibleApps()

	var out string
	if m.isGuest {
		if m.loginCfg == nil {
			cmd := hintCmdStyle.Render("ssh-copy-id yournick@h4ks.com")
			out += hintStyle.Render("Want to keep a nick? Run: "+cmd) + "\n"
		}
		out += titleStyle.Render("Welcome to h4ks - sshland !") + "\n"
	} else {
		out += welcomeStyle.Render(fmt.Sprintf("Welcome to h4ks.com, %s!", m.username)) + "\n"
	}

	if m.isGuest {
		out += userStyle.Render(fmt.Sprintf("not authenticated · %s", m.username)) + "\n"
	} else {
		out += userStyle.Render(fmt.Sprintf("logged in as %s", m.username)) + "\n"
	}

	if m.isNew {
		out += subtitleStyle.Render("· nick registered!") + "\n"
	}

	for i, item := range items {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		var nameRender, desc string
		if item.disabled {
			nameRender = disabledStyle.Render(item.app.Name)
			descText := "  " + item.app.Description
			if item.reason != "" {
				descText += "  [" + item.reason + "]"
			}
			desc = disabledDescStyle.Render(descText)
		} else if i == m.cursor {
			nameRender = selectedStyle.Render(item.app.Name)
			desc = descStyle.Render("  " + item.app.Description)
		} else {
			nameRender = normalStyle.Render(item.app.Name)
			desc = descStyle.Render("  " + item.app.Description)
		}
		out += cursor + nameRender + "\n" + desc + "\n"
	}

	out += helpStyle.Render("↑/↓ navigate • enter select • q quit")
	return out
}

// osc8Link wraps text in an OSC 8 hyperlink sequence so terminals that support
// it (iTerm2, WezTerm, Kitty, GNOME Terminal, VS Code) treat the entire display
// text as a single clickable link to url, regardless of visual line wrapping.
func osc8Link(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// chunkURL breaks s into lines of exactly width bytes with no indent so the
// full terminal width is used. The explicit \n lets BubbleTea count the view
// height correctly and avoid ghost frames on re-render.
func chunkURL(s string, width int) string {
	if len(s) <= width {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && i%width == 0 {
			b.WriteByte('\n')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
