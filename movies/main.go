package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
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

type movieDef struct {
	port    string
	command string
	args    []string
}

var movieDefs = []movieDef{
	{"2222", "/usr/local/bin/ascii-movie", []string{"play", "sw1"}},
	{"2223", "/usr/local/bin/nyancat", nil},
	{"2224", "/usr/local/bin/donut", nil},
	{"2225", "/usr/local/bin/cbonsai", []string{"-l"}},
	{"2226", "/usr/local/bin/fire", nil},
	{"2227", "/usr/local/bin/gol", nil},
	{"2228", "/usr/local/bin/ascii-movie", []string{"play", "rick_roll"}},
	{"2229", "/usr/local/bin/ascii-movie", []string{"play", "/movies/bad_apple.txt"}},
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

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

func movieHandler(command string, args []string) wish.Middleware {
	return func(next cssh.Handler) cssh.Handler {
		return func(sess cssh.Session) {
			if len(sess.Command()) > 0 {
				_, _ = fmt.Fprintln(sess, "exec not supported")
				next(sess)
				return
			}
			ptyReq, winCh, isPty := sess.Pty()
			if !isPty {
				_, _ = fmt.Fprintln(sess, "no PTY requested")
				next(sess)
				return
			}

			cmd := exec.Command(command, args...)
			cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", ptyReq.Term))

			f, err := pty.Start(cmd)
			if err != nil {
				_, _ = fmt.Fprintf(sess.Stderr(), "error starting %s: %v\n", command, err)
				next(sess)
				return
			}
			defer func() { _ = f.Close() }()

			setWinsize(f, ptyReq.Window.Width, ptyReq.Window.Height, ptyReq.Window.WidthPixels, ptyReq.Window.HeightPixels)

			go func() {
				for w := range winCh {
					setWinsize(f, w.Width, w.Height, w.WidthPixels, w.HeightPixels)
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

func setWinsize(f *os.File, width, height, xpixel, ypixel int) {
	type winsize struct {
		Height uint16
		Width  uint16
		XPixel uint16
		YPixel uint16
	}
	ws := winsize{Height: uint16(height), Width: uint16(width), XPixel: uint16(xpixel), YPixel: uint16(ypixel)}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
}

// startServer starts one wish SSH server for a movie. Errors are logged but
// do not terminate the process — a single broken server must not take down
// the other three.
func startServer(movie movieDef, hostKey []byte, proxyKeyPath string) {
	srv, err := wish.NewServer(
		wish.WithAddress("0.0.0.0:"+movie.port),
		wish.WithHostKeyPEM(hostKey),
		wish.WithPublicKeyAuth(func(ctx cssh.Context, presented cssh.PublicKey) bool {
			if ctx.Permissions().Extensions == nil {
				ctx.Permissions().Extensions = make(map[string]string)
			}
			proxyPub, err := loadProxyPublicKey(proxyKeyPath)
			if err != nil {
				log.Printf("movies: loading proxy key: %v", err)
				return false
			}
			return string(proxyPub.Marshal()) == string(presented.Marshal())
		}),
		wish.WithIdleTimeout(10*time.Minute),
		wish.WithMiddleware(
			movieHandler(movie.command, movie.args),
			activeterm.Middleware(),
			logging.Middleware(),
			ratelimiter.Middleware(ratelimiter.NewRateLimiter(rate.Every(time.Second), 10, 1000)),
		),
	)
	if err != nil {
		log.Printf("movies: creating server on port %s: %v", movie.port, err)
		return
	}
	log.Printf("movies: %q on :%s", movie.command, movie.port)
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("movies: server port %s: %v", movie.port, err)
	}
}

func main() {
	proxyKeyPath := getEnv("PROXY_KEY_PATH", "/proxy_key/key")

	// Generate a single in-memory host key shared by all servers.
	// Using one key avoids concurrent file-creation races when four goroutines
	// would otherwise all try to write to /tmp simultaneously.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("generating host key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		log.Fatalf("marshaling host key: %v", err)
	}
	hostKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	for _, movie := range movieDefs {
		go startServer(movie, hostKey, proxyKeyPath)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("movies: shutting down")
}
