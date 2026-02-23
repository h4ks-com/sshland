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
	"regexp"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
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

var (
	validNickRe     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{1,19}$`)
	blockedPrefixes = []string{"guest", "root", "admin", "sshland"}
)

func isValidNick(nick string) bool {
	if !validNickRe.MatchString(nick) {
		return false
	}
	lower := strings.ToLower(nick)
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}

// loadNickKey reads the stored public key for nick. Returns nil, nil if the
// nick file does not exist yet.
func loadNickKey(nicksDir, nick string) (gossh.PublicKey, error) {
	data, err := os.ReadFile(filepath.Join(nicksDir, nick))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key, _, _, _, err := gossh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// saveNickKey atomically creates the nick file and writes the authorized-key
// line. Returns an error (including os.ErrExist) if the file already exists.
func saveNickKey(nicksDir, nick string, key gossh.PublicKey) error {
	path := filepath.Join(nicksDir, nick)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(gossh.MarshalAuthorizedKey(key))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
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
type isNewNickKey struct{}
type isGuestKey struct{}

func makePublicKeyHandler(nicksDir string) cssh.Option {
	return wish.WithPublicKeyAuth(func(ctx cssh.Context, key cssh.PublicKey) bool {
		// charmbracelet/ssh resets Permissions before calling this handler, leaving
		// Extensions as a nil map. The library writes to Extensions immediately after
		// we return, so we must initialize it here to prevent a nil-map panic.
		if ctx.Permissions().Extensions == nil {
			ctx.Permissions().Extensions = make(map[string]string)
		}

		nick := ctx.User()

		// Invalid or blocked nick → ephemeral guest session.
		if !isValidNick(nick) {
			ctx.SetValue(usernameKey{}, guestName(ctx))
			ctx.SetValue(isGuestKey{}, true)
			return true
		}

		stored, err := loadNickKey(nicksDir, nick)
		if err != nil {
			log.Printf("loadNickKey %s: %v", nick, err)
			return false
		}

		if stored == nil {
			// Nick not yet registered — claim it atomically.
			if err := saveNickKey(nicksDir, nick, key); err != nil {
				if os.IsExist(err) {
					// Lost a registration race; the nick was just claimed by someone else.
					return false
				}
				log.Printf("saveNickKey %s: %v", nick, err)
				return false
			}
			ctx.SetValue(usernameKey{}, nick)
			ctx.SetValue(isNewNickKey{}, true)
			return true
		}

		// Nick is registered — key must match.
		if string(stored.Marshal()) != string(key.Marshal()) {
			return false
		}
		ctx.SetValue(usernameKey{}, nick)
		return true
	})
}

// makeTerminalMiddleware replaces activeterm.Middleware(). Sessions with a PTY
// proceed normally to the bubbletea menu. Non-PTY sessions (e.g. ssh-copy-id)
// receive a one-line status message and exit cleanly.
func makeTerminalMiddleware() wish.Middleware {
	return func(next cssh.Handler) cssh.Handler {
		return func(sess cssh.Session) {
			_, _, isPty := sess.Pty()
			if isPty {
				next(sess)
				return
			}
			username, _ := sess.Context().Value(usernameKey{}).(string)
			isNew, _ := sess.Context().Value(isNewNickKey{}).(bool)
			isGuest, _ := sess.Context().Value(isGuestKey{}).(bool)
			switch {
			case isGuest:
				_, _ = fmt.Fprintf(sess, "sshland: connect as ssh yournick@host to register a nick\n")
			case isNew:
				_, _ = fmt.Fprintf(sess, "sshland: nick %q registered to your key\n", username)
			default:
				_, _ = fmt.Fprintf(sess, "sshland: welcome back, %s\n", username)
			}
			_ = sess.Exit(0)
		}
	}
}

func makeMenuHandler(cfg Config) func(sess cssh.Session) (tea.Model, []tea.ProgramOption) {
	return func(sess cssh.Session) (tea.Model, []tea.ProgramOption) {
		username, ok := sess.Context().Value(usernameKey{}).(string)
		if !ok || username == "" {
			username = guestName(sess.Context())
		}
		isNew, _ := sess.Context().Value(isNewNickKey{}).(bool)
		isGuest, _ := sess.Context().Value(isGuestKey{}).(bool)
		renderer := bm.MakeRenderer(sess)
		m := newMenuModel(cfg.Apps, username, isNew, isGuest, sess, renderer)
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
	nicksDir := getEnv("NICKS_DIR", "/data/nicks")
	configPath := getEnv("CONFIG_PATH", "/etc/sshland/config.yaml")
	listenAddr := getEnv("SSH_LISTEN_ADDR", "0.0.0.0:22")

	if err := os.MkdirAll(nicksDir, 0700); err != nil {
		log.Fatalf("creating nicks dir %s: %v", nicksDir, err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		hostKeyOption(hostKeyPath),
		makePublicKeyHandler(nicksDir),
		wish.WithPasswordAuth(func(_ cssh.Context, _ string) bool {
			return false // no password auth
		}),
		wish.WithIdleTimeout(10*time.Minute),
		wish.WithMiddleware(
			makeAppMiddleware(),
			bm.Middleware(makeMenuHandler(cfg)),
			makeTerminalMiddleware(),
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
