package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	cssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/logging"
	"github.com/creack/pty"
)

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func handleSession(command string, args []string) wish.Middleware {
	return func(next cssh.Handler) cssh.Handler {
		return func(sess cssh.Session) {
			username := sess.User()
			resolved := make([]string, len(args))
			for i, a := range args {
				resolved[i] = strings.ReplaceAll(a, "{username}", username)
			}
			cmd := exec.Command(command, resolved...)

			ptyReq, winCh, isPty := sess.Pty()
			if !isPty {
				_, _ = fmt.Fprintln(sess, "no PTY requested")
				next(sess)
				return
			}

			cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", ptyReq.Term))

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
	listenAddr := getEnv("SSH_LISTEN_ADDR", "0.0.0.0:22")

	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(func(ctx cssh.Context, _ cssh.PublicKey) bool {
			if ctx.Permissions().Extensions == nil {
				ctx.Permissions().Extensions = make(map[string]string)
			}
			return true
		}),
		wish.WithPasswordAuth(func(_ cssh.Context, _ string) bool {
			return true
		}),
		wish.WithMiddleware(
			handleSession(command, cmdArgs),
			activeterm.Middleware(),
			logging.Middleware(),
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
