package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/ratelimiter"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"
)

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// resolveUsername returns the comment field from authorized_keys if the
// presented key matches, otherwise returns guest-<shortSessionID>.
func resolveUsername(ctx cssh.Context, key cssh.PublicKey, authKeysPath string) string {
	data, err := os.ReadFile(authKeysPath)
	if err != nil {
		return guestName(ctx)
	}
	presented := key.Marshal()
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed, comment, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		if string(parsed.Marshal()) == string(presented) && comment != "" {
			return comment
		}
	}
	return guestName(ctx)
}

func guestName(ctx cssh.Context) string {
	sid := ctx.SessionID()
	if len(sid) > 8 {
		sid = sid[:8]
	}
	return "guest-" + sid
}

// hostKeyOption loads a host key from path if it's a regular file.
// If the file is missing or is a directory (Docker bind-mount artifact),
// it generates a fresh ED25519 key and tries to persist it for future restarts.
func hostKeyOption(path string) cssh.Option {
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		return wish.WithHostKeyPath(path)
	}
	// Generate a transient key and try to persist it.
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("generating host key: %v", err)
	}
	block, err := gossh.MarshalPrivateKey(privKey, "")
	if err != nil {
		log.Fatalf("marshalling host key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(block)
	// Best-effort save: remove the directory Docker may have created, then write the file.
	_ = os.RemoveAll(path)
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	if err := os.WriteFile(path, pemBytes, 0600); err != nil {
		log.Printf("could not persist host key to %s: %v (will regenerate on restart)", path, err)
	}
	return wish.WithHostKeyPEM(pemBytes)
}

type usernameKey struct{}

func makePublicKeyHandler(authKeysPath string) cssh.Option {
	return wish.WithPublicKeyAuth(func(ctx cssh.Context, key cssh.PublicKey) bool {
		username := resolveUsername(ctx, key, authKeysPath)
		ctx.SetValue(usernameKey{}, username)
		// charmbracelet/ssh resets Permissions before calling this handler, leaving
		// Extensions as a nil map. The library writes to Extensions immediately after
		// we return, so we must initialize it here to prevent a nil-map panic.
		if ctx.Permissions().Extensions == nil {
			ctx.Permissions().Extensions = make(map[string]string)
		}
		return true
	})
}

func makeMenuHandler(cfg Config) func(sess cssh.Session) (tea.Model, []tea.ProgramOption) {
	return func(sess cssh.Session) (tea.Model, []tea.ProgramOption) {
		username, ok := sess.Context().Value(usernameKey{}).(string)
		if !ok || username == "" {
			sid := sess.Context().SessionID()
			if len(sid) > 8 {
				sid = sid[:8]
			}
			username = "guest-" + sid
		}
		renderer := bm.MakeRenderer(sess)
		m := newMenuModel(cfg.Apps, username, sess, renderer)
		return m, []tea.ProgramOption{tea.WithAltScreen()}
	}
}

func makeAppMiddleware() wish.Middleware {
	return func(next cssh.Handler) cssh.Handler {
		return func(sess cssh.Session) {
			app, ok := sess.Context().Value(selectedAppKey{}).(AppConfig)
			if !ok {
				// user quit without selecting
				next(sess)
				return
			}
			username, _ := sess.Context().Value(usernameKey{}).(string)
			if username == "" {
				username = sess.User()
			}
			log.Printf("proxying %s to %s as %s", sess.RemoteAddr(), app.Addr(), username)
			if err := Connect(sess, app, username); err != nil {
				_, _ = fmt.Fprintf(sess.Stderr(), "connection error: %v\n", err)
			}
			next(sess)
		}
	}
}

func main() {
	hostKeyPath := getEnv("SSH_HOST_KEY_PATH", "/data/host_key")
	authKeysPath := getEnv("AUTHORIZED_KEYS_PATH", "/data/authorized_keys")
	configPath := getEnv("CONFIG_PATH", "/etc/sshland/config.yaml")
	listenAddr := getEnv("SSH_LISTEN_ADDR", "0.0.0.0:22")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		hostKeyOption(hostKeyPath),
		makePublicKeyHandler(authKeysPath),
		wish.WithPasswordAuth(func(_ cssh.Context, _ string) bool {
			return false // no password auth
		}),
		wish.WithIdleTimeout(10*time.Minute),
		wish.WithMiddleware(
			makeAppMiddleware(),
			bm.Middleware(makeMenuHandler(cfg)),
			activeterm.Middleware(),
			logging.Middleware(),
			ratelimiter.Middleware(ratelimiter.NewRateLimiter(rate.Every(time.Second), 10, 1000)),
		),
	)
	if err != nil {
		log.Fatalf("creating server: %v", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("starting sshland on %s", listenAddr)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()

	<-done
	log.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
