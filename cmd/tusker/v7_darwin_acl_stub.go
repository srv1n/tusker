//go:build !darwin || !cgo

package main

import (
	"fmt"
	"os"
	"runtime"
)

func v7DarwinDescriptorHasMutationACL(*os.File) (bool, error) {
	return false, fmt.Errorf("descriptor-bound Darwin ACL inspection is unsupported on %s without cgo", runtime.GOOS)
}
