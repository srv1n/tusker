//go:build darwin

package main

import (
	"debug/macho"
	"fmt"
	"runtime"
)

func validateACPAdapterNativeBinary(path string) error {
	file, err := macho.Open(path)
	if err != nil {
		return fmt.Errorf("ACP adapter is not a native Mach-O executable: %w", err)
	}
	defer file.Close()
	if file.Type != macho.TypeExec {
		return fmt.Errorf("ACP adapter Mach-O is not an executable")
	}
	want := macho.CpuArm64
	if runtime.GOARCH == "amd64" {
		want = macho.CpuAmd64
	}
	if file.Cpu != want {
		return fmt.Errorf("ACP adapter Mach-O architecture %s does not match %s", file.Cpu, runtime.GOARCH)
	}
	return nil
}
