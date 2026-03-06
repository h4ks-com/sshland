package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	cssh "github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// proxyKey is an ED25519 keypair used exclusively for entry-menu → wrapper
// internal connections. It is persisted to PROXY_KEY_PATH so wrapper containers
// can verify the key across restarts without sharing the private key.
var (
	proxyOnce   sync.Once
	proxySigner gossh.Signer
)

func getProxySigner() gossh.Signer {
	proxyOnce.Do(func() {
		path := getEnv("PROXY_KEY_PATH", "/proxy_key/key")

		// Try to load an existing persisted key first.
		if data, err := os.ReadFile(path); err == nil {
			raw, err := gossh.ParseRawPrivateKey(data)
			if err == nil {
				if s, err := gossh.NewSignerFromKey(raw); err == nil {
					proxySigner = s
					// Ensure wrapper containers (non-root) can read the key,
					// fixing volumes written by older deployments with 0600.
					_ = os.Chmod(path, 0644)
					return
				}
			}
			log.Printf("proxy: could not parse existing key at %s, regenerating: %v", path, err)
		}

		// Generate a fresh key and persist it.
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic("generating proxy key: " + err.Error())
		}
		proxySigner, err = gossh.NewSignerFromKey(priv)
		if err != nil {
			panic("creating proxy signer: " + err.Error())
		}

		block, err := gossh.MarshalPrivateKey(priv, "")
		if err != nil {
			log.Printf("proxy: marshalling proxy key: %v", err)
			return
		}
		_ = os.MkdirAll(filepath.Dir(path), 0700)
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0644); err != nil {
			log.Printf("proxy: could not persist proxy key to %s: %v", path, err)
		}
	})
	return proxySigner
}

func Connect(sess cssh.Session, app AppConfig, username, token string, mux *sshInputMux) error {
	ptyReq, winCh, isPty := sess.Pty()
	if !isPty {
		return fmt.Errorf("no PTY requested")
	}

	// Encode the OAuth token into the SSH username as "username|token" so the
	// wrapper can extract it without relying on SSH env channel requests, which
	// some server implementations handle inconsistently.
	sshUser := username
	if token != "" {
		sshUser = username + "|" + token
	}

	cfg := &gossh.ClientConfig{
		User: sshUser,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(getProxySigner()),
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

	// Use StdinPipe instead of remote.Stdin so the SSH library does NOT start
	// its own stdin copy goroutine. That goroutine would block on mux.ch
	// waiting for the next keypress even after the remote process exits, and
	// remote.Wait() would not return until a key arrived — consuming and
	// discarding that first key. With StdinPipe, Wait() returns as soon as
	// stdout/stderr are done, and we cancel our goroutine immediately.
	stdinW, err := remote.StdinPipe()
	if err != nil {
		return fmt.Errorf("getting stdin pipe: %w", err)
	}

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

	done := make(chan struct{})
	go func() {
		defer func() { _ = stdinW.Close() }()
		for {
			select {
			case <-done:
				return
			case chunk, ok := <-mux.ch:
				if !ok {
					return
				}
				if _, err := stdinW.Write(chunk); err != nil {
					return
				}
			}
		}
	}()

	err = remote.Wait()
	close(done) // stops the stdin goroutine without consuming from mux.ch
	return err
}
