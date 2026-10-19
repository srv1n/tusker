package main

import (
	"os"
	"syscall"
)

// noteFileChange returns the file's inode and status-change time. Any write,
// rename-over or utimes moves ctime, and no user call can set it back.
func noteFileChange(info os.FileInfo) (uint64, int64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return st.Ino, st.Ctim.Nano()
}
