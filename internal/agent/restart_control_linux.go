package agent

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

func platformRestartAddress(homeHash, unit string) (*net.UnixAddr, error) {
	return &net.UnixAddr{Net: "unix", Name: "@bt-deploy-" + fmt.Sprint(os.Geteuid()) + "-" + homeHash + "-" + unit}, nil
}

func verifyRestartPeer(conn *net.UnixConn) error {
	return verifyRestartPeerUID(conn, os.Geteuid())
}

func verifyRestartPeerUID(conn *net.UnixConn, expected int) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			peerErr = err
		} else if int(cred.Uid) != expected {
			peerErr = fmt.Errorf("restart peer UID differs")
		}
	}); err != nil {
		return err
	}
	return peerErr
}
