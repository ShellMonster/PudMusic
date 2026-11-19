//go:build ios || android

package app

import (
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

func NewSystemStore(dir string) (Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, ErrUnavailable
	}
	name := "android-keystore"
	if runtime.GOOS == "ios" {
		name = "ios-keychain"
	}
	return sealedSessionStore{secrets: mygo.Secrets, path: filepath.Join(dir, "bilibili-session.enc"), name: name}, nil
}
