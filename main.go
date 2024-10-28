package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/joho/godotenv"
	"github.com/manifoldco/promptui"
	"golang.org/x/term"
)

func getEnv(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
func write(s ssh.Session, msg string) {
	_, ok := io.WriteString(s, msg)
	if ok != nil {
		log.Println("Error writing to session: " + ok.Error())
	}
}

func runCommand(s ssh.Session, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	ptyReq, _, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		ctx, cancel := context.WithCancel(context.Background())
		// cmd.Stderr = s.Stderr()
		f, err := pty.Start(cmd)
		if err != nil {
			write(s, "Error starting command: "+err.Error())
			cancel()
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
			cancel()
			return err
		}
		defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.
		go func(ctx context.Context) {
			for {
				select {
				case <-ctx.Done():
					println("ctx.Done")
					return
				default:
					_, err := io.Copy(f, s) // stdin
					if err != nil {
						log.Printf("Error reading from stdin for client %s: %s", s.RemoteAddr(), err.Error())
					}
				}
			}
		}(ctx)
		_, err = io.Copy(s, f) // stdout
		if err != nil {
			log.Printf("Error writing to stdout for client %s: %s", s.RemoteAddr(), err.Error())
		}
		err = cmd.Wait()
		cancel()
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

func sshInto(s ssh.Session, username string, host string, port string) {
	err := runCommand(s, "ssh", "-o", "StrictHostKeyChecking=no", "-p", port, username+"@"+host)
	if err != nil {
		write(s, "Error: "+err.Error()+"\n")
	}
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
	if len(os.Args) > 1 {
		ssh.Handle(func(s ssh.Session) {
			log.Printf("Client connected: %s", s.RemoteAddr())
			err := runCommand(s, os.Args[1])
			if err != nil {
				write(s, "Error: "+err.Error()+"\n")
			}
			log.Printf("Client disconnected: %s", s.RemoteAddr())
		})
		println("Wrapping command: " + os.Args[1])
		println("Listening on " + host + ":" + port)
		log.Fatal(ssh.ListenAndServe(host+":"+port, nil))
		return
	}

	var sshchat_port = getEnv("SSH_CHAT_PORT", "")
	var sshchat_host = getEnv("SSH_CHAT_HOST", "")
	var hanb_port = getEnv("HANB_PORT", "")
	var hanb_host = getEnv("HANB_HOST", "")

	ssh.Handle(func(s ssh.Session) {
		for {
			// Clear terminal
			write(s, "\033[H\033[2J")
			write(s, "Welcome to the SSH server "+s.User()+"!\n")
			prompt := promptui.Select{
				Label:  "Select option:",
				Items:  []string{"Chat", "hanb", "Exit"},
				Stdin:  s,
				Stdout: s,
			}
			_, result, err := prompt.Run()

			if err != nil {
				write(s, "Prompt failed\n"+err.Error())
				return
			}
			write(s, "Selected: "+result+"\n")
			switch result {
			case "Chat":
				if sshchat_port == "" {
					write(s, "SSH_CHAT_PORT not set\n")
					return
				}
				if sshchat_host == "" {
					write(s, "SSH_CHAT_HOST not set\n")
					return
				}
				write(s, "Connecting to SSH Chat\n")
				sshInto(s, s.User(), sshchat_host, sshchat_port)
			case "hanb":
				if hanb_port == "" {
					write(s, "HANB_PORT not set\n")
					return
				}
				if hanb_host == "" {
					write(s, "HANB_HOST not set\n")
					return
				}
				sshInto(s, s.User(), hanb_host, hanb_port)
			case "Exit":
				write(s, "Goodbye!\n")
				s.Close()
			}
		}
	})

	println("Listening on " + host + ":" + port)
	log.Fatal(ssh.ListenAndServe(host+":"+port, nil))
}
