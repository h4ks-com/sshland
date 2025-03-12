package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/joho/godotenv"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

const listHeight = 14

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
	quitTextStyle     = lipgloss.NewStyle().Margin(1, 0, 2, 4)
)

type item string

func (i item) FilterValue() string { return "" }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}


type model struct {
	list     list.Model
	s  ssh.Session
	choice   string
	quitting bool
	altscreenActive bool
	err             error
}


func (m model) Init() tea.Cmd {
	return nil
}

func getEnv(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var sshchat_port = getEnv("SSH_CHAT_PORT", "")
	var sshchat_host = getEnv("SSH_CHAT_HOST", "")
	var hanb_port = getEnv("HANB_PORT", "")
	var hanb_host = getEnv("HANB_HOST", "")
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.choice = string(i)
			}
			switch m.choice {
			case "Chat":
				return m, sshInto(m.s, m.s.User(), sshchat_host, sshchat_port)
			case "hanb":
				return m, sshInto(m.s, m.s.User(), hanb_host, hanb_port)
			case "Quit":
				m.s.Close()
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	return "\n" + m.list.View()
}




func runCommand(s ssh.Session, command string, args ...string) tea.Cmd {
	cmd := exec.Command(command, args...)
	cmd.Stdout = s
    	cmd.Stdin = s
	return tea.ExecProcess(cmd, nil)
}

func sshInto(s ssh.Session, username string, host string, port string) tea.Cmd {
	cmd := runCommand(s, "ssh", "-o", "StrictHostKeyChecking=no", "-p", port, username+"@"+host)
	return cmd
}

func main() {
	err := godotenv.Load()
	if err != nil {
		if err.Error() == "open .env: no such file or directory" {
			log.Println("No .env file found. Using default settings and environment variables.")
		} else {
			log.Fatal("Error loading .env file")
		}
	}

	var port = getEnv("SSH_LISTEN_PORT", "22")
	var host = getEnv("SSH_LISTEN_HOST", "0.0.0.0")



	ssh.Handle(func(s ssh.Session) {
		for {
			// Clear terminal
			items := []list.Item{
				item("Chat"),
				item("hanb"),
				item("Quit"),
			}
			const defaultWidth = 20

			l := list.New(items, itemDelegate{}, defaultWidth, listHeight)
			l.Title = "What do you want for dinner?"
			l.SetShowStatusBar(false)
			l.SetFilteringEnabled(false)
			l.Styles.Title = titleStyle
			l.Styles.PaginationStyle = paginationStyle
			l.Styles.HelpStyle = helpStyle
			m := model{list: l}
			m.s = s

			if _, err := tea.NewProgram(m, tea.WithInput(s), tea.WithOutput(s)).Run(); err != nil {
				fmt.Println("Error running program:", err)
				os.Exit(1)
			}
		}
	})

	hostKeyFile := getEnv("SSH_HOST_KEY_PATH", "")
	println("Listening on " + host + ":" + port)

	if hostKeyFile == "" {
		log.Fatal(ssh.ListenAndServe(host+":"+port, nil))
	} else {
		log.Fatal(ssh.ListenAndServe(host+":"+port, nil, ssh.HostKeyFile(hostKeyFile)))
	}
}
