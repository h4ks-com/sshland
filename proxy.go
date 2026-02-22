package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"

	cssh "github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// proxyKey is a throwaway ED25519 key used for all internal proxy connections.
// Generated once per process; all app containers on the mesh accept any key.
var (
	proxyOnce   sync.Once
	proxySigner gossh.Signer
)

func getProxySigner() gossh.Signer {
	proxyOnce.Do(func() {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic("generating proxy key: " + err.Error())
		}
		proxySigner, err = gossh.NewSignerFromKey(priv)
		if err != nil {
			panic("creating proxy signer: " + err.Error())
		}
	})
	return proxySigner
}

func Connect(sess cssh.Session, app AppConfig, username string) error {
	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		return fmt.Errorf("no PTY requested")
	}

	cfg := &gossh.ClientConfig{
		User: username,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(getProxySigner()),
			gossh.Password("internal"),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // internal mesh only
	}

	client, err := gossh.Dial("tcp", app.Addr(), cfg)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", app.Addr(), err)
	}
	defer func() { _ = client.Close() }()

	remote, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer func() { _ = remote.Close() }()

	remote.Stdin = sess
	remote.Stdout = sess
	remote.Stderr = sess.Stderr()

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}
	if err := remote.RequestPty(ptyReq.Term, ptyReq.Window.Height, ptyReq.Window.Width, modes); err != nil {
		return fmt.Errorf("requesting pty: %w", err)
	}

	go func() {
		for w := range winCh {
			_ = remote.WindowChange(w.Height, w.Width)
		}
	}()

	if err := remote.Shell(); err != nil {
		return fmt.Errorf("starting shell: %w", err)
	}

	return remote.Wait()
}
