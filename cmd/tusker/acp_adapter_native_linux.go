//go:build linux

package main

import (
	"debug/elf"
	"fmt"
	"runtime"
)

func validateACPAdapterNativeBinary(path string) error {
	file, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("ACP adapter is not a native ELF executable: %w", err)
	}
	defer file.Close()
	if file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN {
		return fmt.Errorf("ACP adapter ELF is not an executable")
	}
	if file.Entry == 0 {
		return fmt.Errorf("ACP adapter ELF has no executable entry point")
	}
	want := elf.EM_AARCH64
	if runtime.GOARCH == "amd64" {
		want = elf.EM_X86_64
	}
	if file.Machine != want {
		return fmt.Errorf("ACP adapter ELF architecture %s does not match %s", file.Machine, runtime.GOARCH)
	}
	hasExecutableLoad := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Flags&elf.PF_X != 0 && program.Filesz > 0 && program.Memsz >= program.Filesz {
			hasExecutableLoad = true
			break
		}
	}
	if !hasExecutableLoad {
		return fmt.Errorf("ACP adapter ELF has no executable load segment")
	}
	return nil
}
