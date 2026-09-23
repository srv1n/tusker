//go:build darwin

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func ownedByCurrentUID(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func sameUIDPeer(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix peer")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		var cred *unix.Xucred
		cred, peerErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if peerErr == nil {
			uid = cred.Uid
		}
	}); err != nil {
		return err
	}
	if peerErr != nil {
		return peerErr
	}
	if uid != uint32(os.Getuid()) {
		return fmt.Errorf("uid mismatch")
	}
	return nil
}
