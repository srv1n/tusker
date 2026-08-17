//go:build !darwin || !cgo

package main

import (
	"os"
	"runtime"
)

func v7DarwinDescriptorHasMutationACL(*os.File) (bool, error) {
	return false, v7FullGateProviderUnsupportedPlatformError(runtime.GOOS, "descriptor-bound Darwin ACL inspection requires cgo")
}
