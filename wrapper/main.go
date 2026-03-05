package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"

	cssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/ratelimiter"
	"github.com/creack/pty"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"
)

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// safeUsernameRe matches usernames that are safe to substitute into file paths.
// This covers both registered nicks and entry-menu's guest-XXXXXXXX names.
var safeUsernameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// loadProxyPublicKey reads the proxy private key from path and returns its
// public key. Called on every auth attempt so key rotations are picked up
// automatically without restarting the wrapper.
func loadProxyPublicKey(path string) (gossh.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := gossh.ParseRawPrivateKey(data)
	if err != nil {
		return nil, err
	}
	signer, err := gossh.NewSignerFromKey(raw)
	if err != nil {
		return nil, err
	}
	return signer.PublicKey(), nil
}

func handleSession(command string, args []string) wish.Middleware {
	return func(next cssh.Handler) cssh.Handler {
		return func(sess cssh.Session) {
			if len(sess.Command()) > 0 {
				_, _ = fmt.Fprintln(sess, "exec not supported")
				next(sess)
				return
			}
			username := sess.User()

			var dbPassphrase string
			for _, env := range sess.Environ() {
				if strings.HasPrefix(env, "SSHLAND_DB_PASS=") {
					dbPassphrase = strings.TrimPrefix(env, "SSHLAND_DB_PASS=")
					break
				}
			}

			resolved := make([]string, len(args))
			for i, a := range args {
				resolved[i] = strings.ReplaceAll(a, "{username}", username)
			}
			if dbPassphrase != "" {
				resolved = append(resolved, "--fd3-enc-key")
			}
			cmd := exec.Command(command, resolved...)

			ptyReq, winCh, isPty := sess.Pty()
			if !isPty {
				_, _ = fmt.Fprintln(sess, "no PTY requested")
				next(sess)
				return
			}

			cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", ptyReq.Term))

			// Pass the passphrase via an anonymous pipe (fd 3 in the child).
			// The pipe is never attached to the PTY so there is no echo.
			// tobby tries fd 3 first and falls back to stdin for older wrappers.
			if dbPassphrase != "" {
				pipeR, pipeW, pipeErr := os.Pipe()
				if pipeErr == nil {
					_, _ = pipeW.WriteString(dbPassphrase + "\n")
					_ = pipeW.Close()
					cmd.ExtraFiles = []*os.File{pipeR}
					defer func() { _ = pipeR.Close() }()
				}
			}

			f, err := pty.Start(cmd)
			if err != nil {
				_, _ = fmt.Fprintf(sess.Stderr(), "error starting %s: %v\n", command, err)
				next(sess)
				return
			}
			defer func() { _ = f.Close() }()

			setWinsize(f, ptyReq.Window.Width, ptyReq.Window.Height)

			go func() {
				for w := range winCh {
					setWinsize(f, w.Width, w.Height)
				}
			}()

			go func() {
				if _, err := io.Copy(f, sess); err != nil {
					log.Printf("stdin copy: %v", err)
				}
			}()
			if _, err := io.Copy(sess, f); err != nil {
				log.Printf("stdout copy: %v", err)
			}

			if err := cmd.Wait(); err != nil {
				log.Printf("command exited: %v", err)
			}
			next(sess)
		}
	}
}

func setWinsize(f *os.File, width, height int) {
	type winsize struct {
		Height uint16
		Width  uint16
		x      uint16
		y      uint16
	}
	ws := winsize{Height: uint16(height), Width: uint16(width)}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: wrapper <command> [args...]")
	}
	command := os.Args[1]
	cmdArgs := os.Args[2:]

	hostKeyPath := getEnv("SSH_HOST_KEY_PATH", "/data/host_key")
	listenAddr := getEnv("SSH_LISTEN_ADDR", "0.0.0.0:2222")
	proxyKeyPath := getEnv("PROXY_KEY_PATH", "/proxy_key/key")

	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(func(ctx cssh.Context, presented cssh.PublicKey) bool {
			if ctx.Permissions().Extensions == nil {
				ctx.Permissions().Extensions = make(map[string]string)
			}
			// Reject usernames that are unsafe for {username} path substitution.
			// Defense-in-depth: entry-menu only ever sends valid nicks or
			// guest-XXXXXXXX names, both of which match this pattern.
			if !safeUsernameRe.MatchString(ctx.User()) {
				return false
			}
			// Only accept connections from the entry-menu's proxy key.
			proxyPub, err := loadProxyPublicKey(proxyKeyPath)
			if err != nil {
				log.Printf("wrapper: loading proxy key from %s: %v", proxyKeyPath, err)
				return false
			}
			return string(proxyPub.Marshal()) == string(presented.Marshal())
		}),
		wish.WithIdleTimeout(10*time.Minute),
		wish.WithMiddleware(
			handleSession(command, cmdArgs),
			activeterm.Middleware(),
			logging.Middleware(),
			ratelimiter.Middleware(ratelimiter.NewRateLimiter(rate.Every(time.Second), 10, 1000)),
		),
	)
	if err != nil {
		log.Fatalf("creating server: %v", err)
	}

	log.Printf("wrapper: running %q on %s", command, listenAddr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
