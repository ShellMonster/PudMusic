package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

const (
	mobileSessionKey      = "bilibili-session"
	mobileCipherKey       = mobileSessionKey + "-key-v1"
	mobileAlternateKey    = mobileCipherKey + "-alternate"
	mobileAlternateHeader = "PUDMUSIC-SESSION-ALT\x00"
)

type sessionSecrets interface {
	Get(string) ([]byte, error)
	Set(string, []byte) error
	Delete(string) error
}

type sealedSessionStore struct {
	secrets sessionSecrets
	path    string
	name    string
}

func (s sealedSessionStore) Name() string { return s.name }

func (s sealedSessionStore) Load(ctx context.Context) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		data, err = s.secrets.Get(mobileSessionKey)
		if errors.Is(err, mygo.ErrSecretNotFound) {
			return Session{}, ErrNotLoggedIn
		}
		if err != nil {
			return Session{}, mobileCredentialError("读取登录态", err)
		}
	case err != nil:
		return Session{}, mobileCredentialError("读取登录态", err)
	default:
		keyName, ciphertext := sealedSessionRecord(data)
		key, err := s.secrets.Get(keyName)
		box, err := sealedSessionCipher(key, err)
		if err != nil {
			return Session{}, err
		}
		data, err = box.Open(nil, nil, ciphertext, []byte(keyName))
		if err != nil {
			return Session{}, fmt.Errorf("%w: 登录态解不开", ErrCorrupt)
		}
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("%w: 登录态解不开: %v", ErrCorrupt, err)
	}
	if !session.LoggedIn() {
		return Session{}, ErrNotLoggedIn
	}
	return session, nil
}

func (s sealedSessionStore) Save(ctx context.Context, session Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session.Normalize(time.Now())
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("app: 序列化登录态: %w", err)
	}
	previous, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return mobileCredentialError("读取登录态", err)
	}
	keyName, _ := sealedSessionRecord(previous)
	key, keyErr := s.secrets.Get(keyName)
	if err := ctx.Err(); err != nil {
		return err
	}
	if keyErr != nil || len(key) != 32 {
		if len(previous) != 0 {
			if keyName == mobileCipherKey {
				keyName = mobileAlternateKey
			} else {
				keyName = mobileCipherKey
			}
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return mobileCredentialError("生成登录态密钥", err)
		}
		keyErr = s.secrets.Set(keyName, key)
	}
	box, err := sealedSessionCipher(key, keyErr)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return mobileCredentialError("创建登录态目录", err)
	}
	var header []byte
	if keyName == mobileAlternateKey {
		header = []byte(mobileAlternateHeader)
	}
	if err := writeFileAtomicMode(s.path, box.Seal(header, nil, data, []byte(keyName)), 0o600); err != nil {
		return mobileCredentialError("保存登录态", err)
	}
	return nil
}

func (s sealedSessionStore) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	legacyErr := s.secrets.Delete(mobileSessionKey)
	if errors.Is(legacyErr, mygo.ErrSecretNotFound) {
		legacyErr = nil
	}
	fileErr := os.Remove(s.path)
	if errors.Is(fileErr, os.ErrNotExist) {
		fileErr = nil
	}
	keyErr := s.secrets.Delete(mobileCipherKey)
	if errors.Is(keyErr, mygo.ErrSecretNotFound) {
		keyErr = nil
	}
	alternateErr := s.secrets.Delete(mobileAlternateKey)
	if errors.Is(alternateErr, mygo.ErrSecretNotFound) {
		alternateErr = nil
	}
	if err := errors.Join(legacyErr, fileErr, keyErr, alternateErr); err != nil {
		return mobileCredentialError("清除登录态", err)
	}
	return nil
}

func sealedSessionRecord(data []byte) (string, []byte) {
	if ciphertext, ok := bytes.CutPrefix(data, []byte(mobileAlternateHeader)); ok {
		return mobileAlternateKey, ciphertext
	}
	return mobileCipherKey, data
}

func sealedSessionCipher(key []byte, err error) (cipher.AEAD, error) {
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, fmt.Errorf("%w: 登录态密钥已丢失", ErrCorrupt)
	}
	if err != nil {
		return nil, mobileCredentialError("读取登录态密钥", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: 登录态密钥无效", ErrCorrupt)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, mobileCredentialError("打开登录态密钥", err)
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func mobileCredentialError(operation string, err error) error {
	if errors.Is(err, errors.ErrUnsupported) {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, operation, err)
	}
	return fmt.Errorf("%w: %s: %v", ErrBackend, operation, err)
}
