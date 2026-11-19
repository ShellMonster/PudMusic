package app

import (
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
	mobileSessionKey = "bilibili-session"
	mobileCipherKey  = mobileSessionKey + "-key-v1"
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
		box, err := s.sessionCipher(false)
		if err != nil {
			return Session{}, err
		}
		data, err = box.Open(nil, nil, data, []byte(mobileCipherKey))
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
	box, err := s.sessionCipher(true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return mobileCredentialError("创建登录态目录", err)
	}
	if err := writeFileAtomicMode(s.path, box.Seal(nil, nil, data, []byte(mobileCipherKey)), 0o600); err != nil {
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
	if err := errors.Join(legacyErr, fileErr); err != nil {
		return mobileCredentialError("清除登录态", err)
	}
	return nil
}

func (s sealedSessionStore) sessionCipher(create bool) (cipher.AEAD, error) {
	key, err := s.secrets.Get(mobileCipherKey)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		if !create {
			return nil, fmt.Errorf("%w: 登录态密钥已丢失", ErrCorrupt)
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, mobileCredentialError("生成登录态密钥", err)
		}
		err = s.secrets.Set(mobileCipherKey, key)
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
