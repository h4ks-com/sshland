package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// genKey returns a fresh ed25519 public key and its golang.org/x/crypto/ssh wrapper.
func genKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh pubkey: %v", err)
	}
	return sshPub
}

// ---------- isValidNick ----------

func TestIsValidNick(t *testing.T) {
	valid := []string{
		"alice", "Bob", "x1", "a_b-c", "twentycharslongname1",
	}
	for _, nick := range valid {
		if !isValidNick(nick) {
			t.Errorf("expected valid: %q", nick)
		}
	}

	invalid := []string{
		"",                       // empty
		"a",                      // too short (only 1 char, regex requires 2+)
		"1alice",                 // starts with digit
		"-alice",                 // starts with hyphen
		"_alice",                 // starts with underscore
		"has space",              // space not allowed
		"has.dot",                // dot not allowed
		"toolongname1234567890x", // >20 chars
		// blocked prefixes (all cases)
		"guest", "Guest", "GUEST", "guestuser",
		"root", "rootuser",
		"admin", "Admin", "administrator",
		"sshland", "sshlandbot",
	}
	for _, nick := range invalid {
		if isValidNick(nick) {
			t.Errorf("expected invalid: %q", nick)
		}
	}
}

// ---------- saveNickKey / loadNickKey ----------

func TestSaveAndLoadNickKey(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)

	// Save for the first time — must succeed.
	if err := saveNickKey(dir, "alice", key); err != nil {
		t.Fatalf("saveNickKey: %v", err)
	}

	// Load it back and compare wire bytes.
	loaded, err := loadNickKey(dir, "alice")
	if err != nil {
		t.Fatalf("loadNickKey: %v", err)
	}
	if loaded == nil {
		t.Fatal("loadNickKey returned nil after save")
	}
	if string(loaded.Marshal()) != string(key.Marshal()) {
		t.Error("loaded key does not match saved key")
	}
}

func TestLoadNickKeyNotFound(t *testing.T) {
	dir := t.TempDir()

	key, err := loadNickKey(dir, "nobody")
	if err != nil {
		t.Fatalf("loadNickKey on missing nick: %v", err)
	}
	if key != nil {
		t.Error("expected nil key for unregistered nick")
	}
}

func TestSaveNickKeyAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)

	if err := saveNickKey(dir, "bob", key); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Second save must fail with "already exists".
	err := saveNickKey(dir, "bob", key)
	if err == nil {
		t.Fatal("expected error on duplicate save, got nil")
	}
	if !os.IsExist(err) {
		t.Fatalf("expected IsExist error, got: %v", err)
	}
}

// TestSaveNickKeyRace ensures concurrent registrations of the same nick result
// in exactly one winner and the rest getting an error.
func TestSaveNickKeyRace(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)

	const goroutines = 20
	results := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			results[i] = saveNickKey(dir, "racetest", key)
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Errorf("expected exactly 1 winner in race, got %d", winners)
	}
}

// TestNickFileContent verifies the saved file is a valid authorized-key line.
func TestNickFileContent(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)

	if err := saveNickKey(dir, "charlie", key); err != nil {
		t.Fatalf("saveNickKey: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "charlie"))
	if err != nil {
		t.Fatalf("read nick file: %v", err)
	}

	parsed, _, _, _, err := gossh.ParseAuthorizedKey(data)
	if err != nil {
		t.Fatalf("ParseAuthorizedKey on saved file: %v", err)
	}
	if string(parsed.Marshal()) != string(key.Marshal()) {
		t.Error("parsed key from file does not match original")
	}
}

// TestKeyMismatch verifies that a different key for the same nick does not match.
func TestKeyMismatch(t *testing.T) {
	dir := t.TempDir()
	key1 := genKey(t)
	key2 := genKey(t)

	if err := saveNickKey(dir, "dave", key1); err != nil {
		t.Fatalf("saveNickKey: %v", err)
	}

	stored, err := loadNickKey(dir, "dave")
	if err != nil {
		t.Fatalf("loadNickKey: %v", err)
	}

	if string(stored.Marshal()) == string(key2.Marshal()) {
		t.Error("different keys should not match")
	}
}

// authOutcome mirrors the decision logic inside makePublicKeyHandler, operating
// purely on the filesystem so it can be exercised in unit tests without a live
// SSH server.
//
// Returns one of:
//
//	"guest"  – nick is invalid or blocked prefix (ephemeral session allowed)
//	"new"    – nick just registered to this key
//	"ok"     – nick already registered, key matches
//	"denied" – nick already registered, key does NOT match (or I/O error)
func authOutcome(nicksDir, nick string, key gossh.PublicKey) string {
	if !isValidNick(nick) {
		return "guest"
	}
	stored, err := loadNickKey(nicksDir, nick)
	if err != nil {
		return "denied"
	}
	if stored == nil {
		if err := saveNickKey(nicksDir, nick, key); err != nil {
			return "denied"
		}
		return "new"
	}
	if string(stored.Marshal()) != string(key.Marshal()) {
		return "denied"
	}
	return "ok"
}

// TestAuthFlow exercises the full registration → returning → intruder sequence.
func TestAuthFlow(t *testing.T) {
	dir := t.TempDir()
	ownerKey := genKey(t)
	intruderKey := genKey(t)

	// First connect: nick is unclaimed → registered.
	if got := authOutcome(dir, "eve", ownerKey); got != "new" {
		t.Fatalf("first connect: want 'new', got %q", got)
	}

	// Second connect with the same key → welcome back.
	if got := authOutcome(dir, "eve", ownerKey); got != "ok" {
		t.Fatalf("returning owner: want 'ok', got %q", got)
	}

	// Third connect with a different key → rejected.
	if got := authOutcome(dir, "eve", intruderKey); got != "denied" {
		t.Fatalf("intruder: want 'denied', got %q", got)
	}
}

// TestAuthFlowGuestNicks verifies that blocked/invalid nicks always get guest
// treatment regardless of the key presented.
func TestAuthFlowGuestNicks(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)

	cases := []string{
		"guest",     // blocked prefix
		"guestuser", // starts with blocked prefix
		"root",      // blocked
		"admin",     // blocked
		"sshland",   // blocked
		"1badstart", // starts with digit
		"has space", // invalid chars
		"a",         // too short
	}
	for _, nick := range cases {
		if got := authOutcome(dir, nick, key); got != "guest" {
			t.Errorf("nick %q: want 'guest', got %q", nick, got)
		}
	}
}

// TestAuthFlowConcurrentRegistration ensures only one concurrent registrant wins.
func TestAuthFlowConcurrentRegistration(t *testing.T) {
	dir := t.TempDir()

	const goroutines = 30
	outcomes := make([]string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			outcomes[i] = authOutcome(dir, "frank", genKey(t))
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, o := range outcomes {
		switch o {
		case "new":
			winners++
		case "denied":
			// Lost the race — correct.
		default:
			t.Errorf("unexpected outcome %q", o)
		}
	}
	if winners != 1 {
		t.Errorf("expected exactly 1 winner, got %d", winners)
	}
}
