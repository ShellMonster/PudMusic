package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

type fixtureSecrets map[string][]byte

func (f fixtureSecrets) Get(key string) ([]byte, error) {
	value, ok := f[key]
	if !ok {
		return nil, mygo.ErrSecretNotFound
	}
	return bytes.Clone(value), nil
}

func (f fixtureSecrets) Set(key string, value []byte) error {
	if len(value) > mygo.MaxSecretSize {
		return errors.New("fixture secret is too large")
	}
	f[key] = bytes.Clone(value)
	return nil
}

func (f fixtureSecrets) Delete(key string) error { delete(f, key); return nil }

// TestSealedSessionSurvivesTheSecretSizeLimit verifies that a large session can be restored and cleared without storing it as a plain file.
func TestSealedSessionSurvivesTheSecretSizeLimit(t *testing.T) {
	store := sealedSessionStore{secrets: fixtureSecrets{}, path: filepath.Join(t.TempDir(), "session.enc")}
	ctx := t.Context()
	const marker = "pudmusic-qa-value"
	session := Session{SESSDATA: strings.Repeat(marker, 500)}
	if err := store.Save(ctx, session); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte(marker)) {
		t.Fatal("found plaintext in the session file")
	}
	if got, err := store.Load(ctx); err != nil || got.SESSDATA != session.SESSDATA {
		t.Fatalf("session did not restore: %v", err)
	}
	blob[len(blob)-1] ^= 1
	if err := os.WriteFile(store.path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("modified ciphertext loaded: %v", err)
	}
	if err := store.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("session still present after clearing: %v", err)
	}
}
