package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"
)

// Identity is stored at /data/identities/{fingerprint} when Logto is enabled.
type Identity struct {
	LogtoSub string `json:"logto_sub"`
	Username string `json:"username"`
}

// keyFingerprint returns the first 32 hex chars of the SHA-256 of the key wire bytes.
func keyFingerprint(key gossh.PublicKey) string {
	h := sha256.Sum256(key.Marshal())
	return hex.EncodeToString(h[:])[:32]
}

// loadIdentity reads the stored identity for key. Returns nil, nil if not found.
func loadIdentity(dir string, key gossh.PublicKey) (*Identity, error) {
	fp := keyFingerprint(key)
	data, err := os.ReadFile(filepath.Join(dir, fp))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("parsing identity %s: %w", fp, err)
	}
	return &id, nil
}

// saveIdentity atomically creates the identity file. Returns os.ErrExist if
// the key is already bound to an identity.
func saveIdentity(dir string, key gossh.PublicKey, id Identity) error {
	fp := keyFingerprint(key)
	data, err := json.Marshal(id)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fp)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// sshuserName returns a deterministic throwaway name for an unrecognised key.
func sshuserName(key gossh.PublicKey) string {
	return "sshuser-" + keyFingerprint(key)[:4]
}
