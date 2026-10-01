//go:build !linux

package agent

import (
	"fmt"
	"net"
)

func platformRestartAddress(_, _ string) (*net.UnixAddr, error) {
	return nil, fmt.Errorf("owned restart control requires Linux peer credentials")
}

func verifyRestartPeer(*net.UnixConn) error {
	return fmt.Errorf("owned restart control requires Linux peer credentials")
}
