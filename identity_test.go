package main

import (
	"os"
	"testing"
)

func TestKeyFingerprint(t *testing.T) {
	key1 := genKey(t)
	key2 := genKey(t)

	fp1 := keyFingerprint(key1)
	fp2 := keyFingerprint(key2)

	if len(fp1) != 32 {
		t.Errorf("fingerprint length: want 32, got %d", len(fp1))
	}
	if fp1 == fp2 {
		t.Error("different keys should have different fingerprints")
	}
	// Deterministic: same key → same fingerprint
	if keyFingerprint(key1) != fp1 {
		t.Error("fingerprint not deterministic")
	}
}

func TestSshuserName(t *testing.T) {
	key := genKey(t)
	name := sshuserName(key)

	const prefix = "sshuser-"
	if name[:len(prefix)] != prefix {
		t.Errorf("sshuserName: expected prefix %q, got %q", prefix, name)
	}
	if len(name) != len(prefix)+4 {
		t.Errorf("sshuserName: expected len %d, got %d (%q)", len(prefix)+4, len(name), name)
	}
}

func TestSaveAndLoadIdentity(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)
	id := Identity{LogtoSub: "sub123", Username: "alice"}

	if err := saveIdentity(dir, key, id); err != nil {
		t.Fatalf("saveIdentity: %v", err)
	}

	loaded, err := loadIdentity(dir, key)
	if err != nil {
		t.Fatalf("loadIdentity: %v", err)
	}
	if loaded == nil {
		t.Fatal("loadIdentity returned nil after save")
	}
	if loaded.Username != id.Username {
		t.Errorf("username: want %q, got %q", id.Username, loaded.Username)
	}
	if loaded.LogtoSub != id.LogtoSub {
		t.Errorf("logto_sub: want %q, got %q", id.LogtoSub, loaded.LogtoSub)
	}
}

func TestLoadIdentityNotFound(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)

	id, err := loadIdentity(dir, key)
	if err != nil {
		t.Fatalf("loadIdentity on missing key: %v", err)
	}
	if id != nil {
		t.Error("expected nil identity for unknown key")
	}
}

func TestSaveIdentityAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	key := genKey(t)
	id := Identity{LogtoSub: "sub123", Username: "alice"}

	if err := saveIdentity(dir, key, id); err != nil {
		t.Fatalf("first save: %v", err)
	}

	err := saveIdentity(dir, key, id)
	if err == nil {
		t.Fatal("expected error on duplicate save, got nil")
	}
	if !os.IsExist(err) {
		t.Fatalf("expected IsExist error, got: %v", err)
	}
}
