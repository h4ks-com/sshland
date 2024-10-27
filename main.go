package main

import (
	"io"
	"log"
	"os"
	"os/exec"

	"github.com/gliderlabs/ssh"
	"github.com/joho/godotenv"
	"github.com/manifoldco/promptui"
)

func getEnv(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func sshInto(s io.Writer, username string, host string, port string) {
	cmd := exec.Command("ssh", "-tt", "-o", "StrictHostKeyChecking=no", "-p", port, username+"@"+host)
	cmd.Stdout = s
	cmd.Stderr = s
	error := cmd.Run()
	if error != nil {
		_, err := s.Write([]byte("Error: " + error.Error() + "\n"))
		if err != nil {
			log.Fatal(err)
		}
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

	var port = getEnv("SSH_LISTEN_PORT", "2222")
	var host = getEnv("SSH_LISTEN_HOST", "0.0.0.0")
	var sshchat_port = getEnv("SSH_CHAT_PORT", "")
	var sshchat_host = getEnv("SSH_CHAT_HOST", "")

	ssh.Handle(func(s ssh.Session) {
		write := func(msg string) {
			_, ok := io.WriteString(s, msg)
			if ok != nil {
				log.Fatal(ok)
			}
		}
		write("Welcome to the SSH server\n\n")
		prompt := promptui.Select{
			Label:  "Select option:",
			Items:  []string{"Chat", "Shell", "Exit"},
			Stdin:  s,
			Stdout: s,
		}
		_, result, err := prompt.Run()

		if err != nil {
			write("Prompt failed\n" + err.Error())
			return
		}
		write("Selected: " + result + "\n")
		switch result {
		case "Chat":
			if sshchat_port == "" {
				write("SSH_CHAT_PORT not set\n")
				return
			}
			if sshchat_host == "" {
				write("SSH_CHAT_HOST not set\n")
				return
			}
			write("Connecting to SSH Chat\n")
			sshInto(s, s.User(), sshchat_host, sshchat_port)
		case "Shell":
			write("Hah? You want shell? No shell for you!\n")
		case "Exit":
			write("Goodbye\n")
			s.Close()
		}
	})

	println("Listening on " + host + ":" + port)
	log.Fatal(ssh.ListenAndServe(host+":"+port, nil))
}
