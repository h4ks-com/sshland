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

type menuItem struct {
	app      AppConfig
	disabled bool
}

type menuModel struct {
	apps        []AppConfig
	cursor      int
	subMenu     string // "" = main menu, "games" = games submenu, "movies" = movies submenu
	username    string
	isNew       bool
	isGuest     bool
	sess        cssh.Session
	renderer    *lipgloss.Renderer
	loginCfg    *LogtoConfig
	pendingMgr  *PendingAuthManager
	publicKey   gossh.PublicKey
	loginState  *loginWaitState
	width       int
	tokenForApp *AppConfig // non-nil when OAuth flow was triggered to get a token for an app launch
}

type keyMap struct {
	Up    key.Binding
	Down  key.Binding
	Enter key.Binding
	Quit  key.Binding
	Esc   key.Binding
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
	Esc: key.NewBinding(
		key.WithKeys("esc"),
	),
}

func newMenuModel(apps []AppConfig, username string, isNew, isGuest bool, sess cssh.Session, renderer *lipgloss.Renderer, loginCfg *LogtoConfig, pendingMgr *PendingAuthManager, publicKey gossh.PublicKey) menuModel {
	return menuModel{
		apps:       apps,
		username:   username,
		isNew:      isNew,
		isGuest:    isGuest,
		sess:       sess,
		renderer:   renderer,
		loginCfg:   loginCfg,
		pendingMgr: pendingMgr,
		publicKey:  publicKey,
	}
}

// visibleApps returns menu items for the current submenu state.
// Main menu: injects "games ▸" and "movies ▸" before logout, excludes grouped apps.
// Games submenu: injects "‹ back" first, includes only group=="games" apps.
// Movies submenu: injects "‹ back" first, includes only group=="movies" apps.
func (m menuModel) visibleApps() []menuItem {
	if m.subMenu == "games" {
		var items []menuItem
		items = append(items, menuItem{app: AppConfig{Name: "back", Description: "Back to main menu"}})
		for _, a := range m.apps {
			if a.Group == "games" {
				items = append(items, menuItem{app: a})
			}
		}
		return items
	}

	if m.subMenu == "movies" {
		var items []menuItem
		items = append(items, menuItem{app: AppConfig{Name: "back", Description: "Back to main menu"}})
		for _, a := range m.apps {
			if a.Group == "movies" {
				items = append(items, menuItem{app: a})
			}
		}
		return items
	}

	var items []menuItem
	if m.loginCfg != nil && m.isGuest {
		if m.publicKey != nil {
			items = append(items, menuItem{app: AppConfig{Name: "login", Description: "Authenticate to claim your nick"}})
		} else {
			items = append(items, menuItem{
				app:      AppConfig{Name: "login", Description: "run ssh-keygen first to get a key"},
				disabled: true,
			})
		}
	}
	for _, a := range m.apps {
		if a.RequiresAuth && m.isGuest {
			continue
		}
		if a.Group == "games" || a.Group == "movies" {
			continue
		}
		items = append(items, menuItem{app: a})
	}
	items = append(items, menuItem{app: AppConfig{Name: "games", Description: "Play something fun"}})
	items = append(items, menuItem{app: AppConfig{Name: "movies", Description: "Watch ASCII animations"}})
	if m.loginCfg != nil && !m.isGuest {
		items = append(items, menuItem{app: AppConfig{Name: "logout", Description: "We will stop recognizing your ssh key"}})
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
		return tea.Batch(tea.ClearScreen, tickCmd())
	}
	return tea.ClearScreen
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
		m.loginState = nil
		if msg.Token != "" {
			m.sess.Context().SetValue(oauthTokenKey{}, msg.Token)
		}
		if m.tokenForApp != nil {
			// OAuth was triggered to get a token for an app launch, not for login.
			// Don't change username/isGuest — the user is already authenticated.
			app := *m.tokenForApp
			m.tokenForApp = nil
			m.sess.Context().SetValue(selectedAppKey{}, app)
			return m, tea.Quit
		}
		m.username = msg.Username
		m.isGuest = false
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

		case key.Matches(msg, keys.Esc):
			if m.subMenu != "" {
				m.subMenu = ""
				m.cursor = 0
				return m, tea.ClearScreen
			}

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
			if len(items) > 0 && !items[m.cursor].disabled {
				selected := items[m.cursor].app
				if selected.Name == "games" {
					m.subMenu = "games"
					m.cursor = 0
					return m, tea.ClearScreen
				}
				if selected.Name == "movies" {
					m.subMenu = "movies"
					m.cursor = 0
					return m, tea.ClearScreen
				}
				if selected.Name == "back" {
					m.subMenu = ""
					m.cursor = 0
					return m, tea.ClearScreen
				}
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
				if selected.RequiresOAuth && m.loginCfg != nil {
					existingToken, _ := m.sess.Context().Value(oauthTokenKey{}).(string)
					if existingToken == "" {
						// Attempt silent refresh before showing the OAuth URL.
						if m.publicKey != nil {
							if id, _ := loadIdentity(m.loginCfg.IdentitiesDir, m.publicKey); id != nil && id.RefreshToken != "" {
								if accessToken, newRefresh, err := m.loginCfg.RefreshAccessToken(id.RefreshToken); err == nil {
									id.RefreshToken = newRefresh
									_ = writeIdentity(m.loginCfg.IdentitiesDir, m.publicKey, *id)
									m.sess.Context().SetValue(oauthTokenKey{}, accessToken)
									m.sess.Context().SetValue(selectedAppKey{}, selected)
									return m, tea.Quit
								}
								// Refresh failed (expired/revoked) — fall through to OAuth URL.
							}
						}
						state := newRandomState()
						ch := m.pendingMgr.Register(state, m.publicKey)
						m.loginState = &loginWaitState{url: m.loginCfg.BuildAuthURL(state)}
						m.tokenForApp = &selected
						return m, tea.Batch(waitForAuthCmd(ch), tickCmd(), tea.ClearScreen)
					}
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
		Foreground(lipgloss.Color("238"))

	descStyle := r.NewStyle().
		Foreground(lipgloss.Color("241"))

	externalStyle := r.NewStyle().
		Foreground(lipgloss.Color("214"))

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
		var displayName string
		switch item.app.Name {
		case "games":
			displayName = "games ▸"
		case "movies":
			displayName = "movies ▸"
		case "back":
			displayName = "‹ back"
		default:
			displayName = item.app.Name
		}

		var cursor, nameRender string
		switch {
		case item.disabled:
			cursor = "  "
			nameRender = disabledStyle.Render(displayName)
		case i == m.cursor:
			cursor = "> "
			nameRender = selectedStyle.Render(displayName)
		default:
			cursor = "  "
			nameRender = normalStyle.Render(displayName)
		}

		if item.app.External {
			nameRender += " " + externalStyle.Render("(external)")
		}

		out += cursor + nameRender + "\n" + descStyle.Render("  "+item.app.Description) + "\n"
	}

	helpText := "↑/↓ navigate • enter select • q quit"
	if m.subMenu != "" {
		helpText = "↑/↓ navigate • enter select • esc back • q quit"
	}
	out += helpStyle.Render(helpText)
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
