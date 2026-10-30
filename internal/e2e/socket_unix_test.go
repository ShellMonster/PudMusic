//go:build e2e && !windows

package e2e

import (
	"net"
	"time"
)

func automationSocketBase() string { return "/tmp" }

func dialAutomation(path string) (net.Conn, error) {
	return net.DialTimeout("unix", path, 500*time.Millisecond)
}
