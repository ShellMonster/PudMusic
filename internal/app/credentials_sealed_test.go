package app

import (
	"bytes"
	"context"
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

type blockedSessionSave struct {
	Store
	started, release chan struct{}
}

func (store blockedSessionSave) Save(ctx context.Context, session Session) error {
	close(store.started)
	<-store.release
	return store.Store.Save(context.WithoutCancel(ctx), session)
}

// TestCancelLoginWhileSavingLeavesNoSession verifies that a late credential write is rolled back after cancellation.
func TestCancelLoginWhileSavingLeavesNoSession(t *testing.T) {
	store := blockedSessionSave{Store: NewMemoryStore("fixture"), started: make(chan struct{}), release: make(chan struct{})}
	manager := NewManager(store)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	lease := manager.BeginRenew()
	account := &Account{mgr: manager, stop: cancel, loginLease: lease}
	result := make(chan error, 1)
	go func() {
		result <- lease.Commit(ctx, func(session *Session) error {
			session.SESSDATA = "synthetic-review"
			return nil
		})
	}()
	<-store.started
	account.cancel()
	close(store.release)
	if err := <-result; !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("canceled login result = %v", err)
	}
	if _, err := store.Load(t.Context()); !errors.Is(err, ErrNotLoggedIn) || manager.Current().LoggedIn() {
		t.Fatalf("canceled login remains signed in, load error = %v", err)
	}
}

type restoredSecrets struct{ fixtureSecrets }

func (store restoredSecrets) Get(key string) ([]byte, error) {
	value, err := store.fixtureSecrets.Get(key)
	if key == mobileCipherKey && bytes.Equal(value, []byte("unreadable-backup")) {
		return nil, errors.New("keystore authentication failed")
	}
	return value, err
}

// TestNewLoginReplacesAnUnreadableRestoredKey checks that a fresh login can persist after an Android backup restore.
func TestNewLoginReplacesAnUnreadableRestoredKey(t *testing.T) {
	secrets := restoredSecrets{fixtureSecrets{mobileCipherKey: []byte("unreadable-backup")}}
	store := sealedSessionStore{secrets: secrets, path: filepath.Join(t.TempDir(), "session.enc")}
	session := Session{SESSDATA: "synthetic-review"}
	if err := store.Save(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(t.Context()); err != nil || got.SESSDATA != session.SESSDATA {
		t.Fatalf("new login did not restore: %v", err)
	}
}

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
