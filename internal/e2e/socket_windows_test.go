//go:build e2e && windows

package e2e

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

func automationSocketBase() string {
	base := filepath.Join(os.TempDir(), "mg")
	_ = os.MkdirAll(base, 0o700)
	return base
}

func dialAutomation(path string) (net.Conn, error) {
	return net.DialTimeout("unix", path, 500*time.Millisecond)
}
