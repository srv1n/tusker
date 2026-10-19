//go:build !darwin && !linux

package main

import "os"

// noteFileChange has no portable ctime here; size+mtime stay the key.
func noteFileChange(os.FileInfo) (uint64, int64) { return 0, 0 }
