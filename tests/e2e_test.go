// Tests for the sshland stack. They connect to a running compose stack and
// exercise the SSH menu, proxy, and each sub-service.
//
// Run against an already-running stack (default port 6922):
//
//	go test ./tests/ -v -timeout 60s
//
// Run with automatic stack lifecycle (builds + starts on port 16922):
//
//	go test ./tests/ -v -timeout 120s -run-stack
package tests_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

var (
	flagRunStack = flag.Bool("run-stack", false, "start docker compose stack before tests and stop it after")

	testAddr   string
	testSigner gossh.Signer
)

func TestMain(m *testing.M) {
	flag.Parse()

	port := os.Getenv("SSH_PORT")
	if port == "" {
		if *flagRunStack {
			port = "16922"
		} else {
			port = "6922"
		}
	}
	testAddr = "localhost:" + port

	// Generate a throwaway key pair — the server accepts all public keys.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("keygen: %v", err)
	}
	testSigner, err = gossh.NewSignerFromKey(priv)
	if err != nil {
		log.Fatalf("signer: %v", err)
	}

	if *flagRunStack {
		dataDir, err := os.MkdirTemp("", "sshland-e2e-*")
		if err != nil {
			log.Fatalf("tempdir: %v", err)
		}
		defer func() { _ = os.RemoveAll(dataDir) }()
		startStack(port, dataDir)
		defer stopStack(port, dataDir)
	}

	if !dialWait(testAddr, 30*time.Second) {
		log.Fatalf("timed out waiting for SSH at %s", testAddr)
	}

	os.Exit(m.Run())
}

// ---------- stack lifecycle ----------

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..")
}

func composeCmd(port, dataDir string, args ...string) *exec.Cmd {
	root := repoRoot()
	full := append([]string{"compose", "-f", filepath.Join(root, "compose.yaml"),
		"-p", "sshland-e2e"}, args...)
	cmd := exec.Command("docker", full...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"SSH_PORT="+port,
		"DATA_DIR="+dataDir,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd
}

func startStack(port, dataDir string) {
	cmd := composeCmd(port, dataDir, "up", "-d", "--build", "--wait")
	if err := cmd.Run(); err != nil {
		log.Fatalf("compose up: %v", err)
	}
}

func stopStack(port, dataDir string) {
	_ = composeCmd(port, dataDir, "down", "-v", "--remove-orphans").Run()
}

func dialWait(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = c.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// ---------- SSH helpers ----------

func sshClient(t *testing.T) *gossh.Client {
	t.Helper()
	// "guest" is a blocked prefix → always an ephemeral guest session regardless
	// of key, so the test key never conflicts with a registered nick.
	cfg := &gossh.ClientConfig{
		User:            "guest",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(testSigner)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // test only
		Timeout:         10 * time.Second,
	}
	cl, err := gossh.Dial("tcp", testAddr, cfg)
	if err != nil {
		t.Fatalf("ssh dial %s: %v", testAddr, err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

// outBuf collects SSH session output, responds to terminal capability queries
// so the bubbletea program doesn't block waiting for terminal responses, and
// lets tests poll the accumulated text.
type outBuf struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	stdin io.Writer // set after creation so we can respond to queries
}

func (b *outBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	b.respondToQueries(string(p))
	return len(p), nil
}

// respondToQueries detects terminal capability query sequences and writes the
// minimal correct response. Without this, charmbracelet/wish's queryTerminal
// blocks indefinitely because the SSH stdin pipe has no Fd() to interrupt.
func (b *outBuf) respondToQueries(raw string) {
	if b.stdin == nil {
		return
	}
	// DA1 (Primary Device Attributes) — server asks "what terminal are you?"
	if strings.Contains(raw, "\x1b[c") {
		_, _ = b.stdin.Write([]byte("\x1b[?1;2c"))
	}
	// OSC 11 — server asks "what is your background color?"
	if strings.Contains(raw, "\x1b]11;?") {
		_, _ = b.stdin.Write([]byte("\x1b]11;rgb:0000/0000/0000\a"))
	}
}

func (b *outBuf) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return stripANSI(b.buf.String())
}

func (b *outBuf) waitFor(want string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(b.text(), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[=>]|\x1b\][^\x07]*\x07|\r`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func ptySession(t *testing.T, cl *gossh.Client) (io.WriteCloser, *outBuf, *gossh.Session) {
	t.Helper()
	sess, err := cl.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	modes := gossh.TerminalModes{gossh.ECHO: 0, gossh.TTY_OP_ISPEED: 14400, gossh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		t.Fatalf("request pty: %v", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	t.Cleanup(func() { _ = stdin.Close() })

	out := &outBuf{stdin: stdin}
	sess.Stdout = out
	sess.Stderr = out

	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	// sess.Wait() starts the internal goroutines that copy channel data to
	// sess.Stdout. Without calling it, out never receives any bytes.
	go func() { _ = sess.Wait() }()

	return stdin, out, sess
}

// menuOrder matches the order in docker/config.yaml.
var menuOrder = []string{"chat", "tobby", "hanb", "wordle"}

// selectApp waits for the menu and navigates to the named app.
func selectApp(t *testing.T, stdin io.Writer, out *outBuf, name string) {
	t.Helper()
	if !out.waitFor("sshland", 5*time.Second) {
		t.Fatalf("menu did not appear; output:\n%s", out.text())
	}
	idx := -1
	for i, n := range menuOrder {
		if n == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("unknown app %q", name)
	}
	for i := 0; i < idx; i++ {
		_, _ = stdin.Write([]byte("j"))
		time.Sleep(50 * time.Millisecond)
	}
	_, _ = stdin.Write([]byte("\r"))
}

// assertProxy waits for service output after selecting an app, and fails if
// the proxy reported a connection error.
func assertProxy(t *testing.T, out *outBuf, successHint string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		text := out.text()
		if strings.Contains(text, "connection error") {
			t.Fatalf("proxy failed:\n%s", text)
		}
		if successHint == "" || strings.Contains(text, successHint) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	text := out.text()
	if strings.Contains(text, "connection error") {
		t.Fatalf("proxy failed:\n%s", text)
	}
	if successHint != "" {
		t.Logf("hint %q not seen within %s; output:\n%s", successHint, d, text)
	}
}

// ---------- Tests ----------

// TestDebugRaw prints the raw bytes received from the server to help diagnose issues.
func TestDebugRaw(t *testing.T) {
	cl := sshClient(t)
	_, out, _ := ptySession(t, cl)
	time.Sleep(3 * time.Second)
	raw := out.buf.String()
	t.Logf("raw bytes (%d): %q", len(raw), raw)
	t.Logf("stripped: %q", out.text())
}

func TestMenuDisplayed(t *testing.T) {
	_, out, _ := ptySession(t, sshClient(t))

	if !out.waitFor("sshland", 5*time.Second) {
		t.Fatalf("menu never appeared:\n%s", out.text())
	}
	for _, app := range menuOrder {
		if !out.waitFor(app, 2*time.Second) {
			t.Errorf("app %q missing from menu:\n%s", app, out.text())
		}
	}
}

func TestGuestUsername(t *testing.T) {
	_, out, _ := ptySession(t, sshClient(t))

	if !out.waitFor("guest-", 5*time.Second) {
		t.Fatalf("guest username not in menu:\n%s", out.text())
	}
}

func TestWordle(t *testing.T) {
	stdin, out, _ := ptySession(t, sshClient(t))
	selectApp(t, stdin, out, "wordle")
	assertProxy(t, out, "", 5*time.Second)
}

func TestSshchat(t *testing.T) {
	stdin, out, _ := ptySession(t, sshClient(t))
	selectApp(t, stdin, out, "chat")
	// ssh-chat sends a welcome banner on connect
	assertProxy(t, out, "Welcome", 5*time.Second)
}

func TestHanb(t *testing.T) {
	stdin, out, _ := ptySession(t, sshClient(t))
	selectApp(t, stdin, out, "hanb")
	assertProxy(t, out, "", 5*time.Second)
}

func TestTobby(t *testing.T) {
	stdin, out, _ := ptySession(t, sshClient(t))
	selectApp(t, stdin, out, "tobby")

	// tobby shows a setup dialog ("Nickname", "Port") on first run, or connects
	// to IRC and shows the channel ("#dev") on subsequent runs. Either way the
	// tobby UI must have actually rendered — a crash would show an error instead.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		text := out.text()
		if strings.Contains(text, "connection error") || strings.Contains(text, "Symbol") {
			t.Fatalf("tobby failed:\n%s", text)
		}
		// "Nickname" appears in tobby's setup dialog.
		// "irc.h4ks" appears in tobby's connected view (server name).
		// Neither appears in the sshland menu, so both are safe positive signals.
		// ("#dev" is NOT used here: the menu itself contains "IRC client (#dev on h4ks.com)")
		if strings.Contains(text, "Nickname") || strings.Contains(text, "irc.h4ks") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("tobby UI never appeared:\n%s", out.text())
}
