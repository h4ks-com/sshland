package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/joho/godotenv"
	"golang.org/x/term"
)

type item struct {
	name string
	command string
}


type model struct {
	sess session
	list list.Model
	choice string
	quitting bool
}

func (i item) FilterValue() string { return i.name }

func getEnv(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func runCommand(s ssh.Session, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	ptyReq, _, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		// cmd.Stderr = s.Stderr()
		f, err := pty.Start(cmd)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }() // Best effort.
		// Handle pty size.
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGWINCH)
		go func() {
			for range ch {
				if err := pty.InheritSize(os.Stdin, f); err != nil {
					log.Printf("error resizing pty: %s", err)
				}
			}
		}()
		ch <- syscall.SIGWINCH                        // Initial resize.
		defer func() { signal.Stop(ch); close(ch) }() // Cleanup signals when done.

		// Set stdin in raw mode.
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			log.Printf("Error making raw: %s", err.Error())
			return err
		}
		defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.

		r := io.NopCloser(s)
		defer r.Close()
		go func() {
			_, err = io.Copy(f, r) // stdin
			if err != nil {
				log.Printf("Error reading from stdin for client %s: %s", s.RemoteAddr(), err.Error())
			}
		}()
		_, err = io.Copy(s, f) // stdout
		if err != nil {
			log.Printf("Error writing to stdout for client %s: %s", s.RemoteAddr(), err.Error())
		}
		err = cmd.Wait()
		if err != nil {
			log.Printf("Error waiting for command: %s", err.Error())
			return err
		}
	} else {
		_, err := io.WriteString(s, "No PTY requested.\n")
		if err != nil {
			log.Printf("Error writing to session: %s", err.Error())
			return err
		}
	}
	return nil
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

	// If there is a argument, it will be used as the command to wrap

	var sshchat_port = getEnv("SSH_CHAT_PORT", "")
	var sshchat_host = getEnv("SSH_CHAT_HOST", "")
	var hanb_port = getEnv("HANB_PORT", "")
	var hanb_host = getEnv("HANB_HOST", "")

	var username string 

	items := []list.Item{
		item{name: "hanb", command: fmt.Sprintf("ssh -o StrictHostKeyChecking=no -p %s %s@%s", hanb_port, username, hanb_host)},
		item{name: "Chat", command: fmt.Sprint("ssh -o StrictHostKeyChecking=no -p %s %s@%s", sshchat_port, username, sshchat_host)},
	}

	fmt.Print(items)

	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithMiddleware(
			bubbletea.Middleware(teaHandler)
		)
	)
	
	hostKeyFile := getEnv("SSH_HOST_KEY_PATH", "")
	println("Listening on " + host + ":" + port)

	if hostKeyFile == "" {
		log.Fatal(ssh.ListenAndServe(host+":"+port, nil))
	} else {
		log.Fatal(ssh.ListenAndServe(host+":"+port, nil, ssh.HostKeyFile(hostKeyFile)))
	}
}

func teaHandler(s ssh.Session) (tea.Model, tea.ProgramOption) {
	renderer := bubbletea.MakeRenderer(s)

	m 
}

func (m model) Init() tea.Cmd {
	return nil
}

type commandFinishedMsg struct{ err error }

func execProcess(command string) tea.Cmd {
	c := exec.Command(command)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return commandFinishedMsg{err}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				return m, execProcess(i.command)
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	s := "Welcome to the SSH server.\n"
	s += m.list.View()
	return s
}
