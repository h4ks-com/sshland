package main

import (
	"io"
	"log"
	"os"

	"github.com/gliderlabs/ssh"
	"github.com/joho/godotenv"
)

func getEnv(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
func main() {
	ssh.Handle(func(s ssh.Session) {
		_, ok := io.WriteString(s, "Hello world\n")
		if ok != nil {
			log.Fatal(ok)
		}
	})
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
	println("Listening on " + host + ":" + port)
	log.Fatal(ssh.ListenAndServe(host+":"+port, nil))
}
