//go:build windows

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// Attach the parent terminal for explicit command-line operations.
func init() {
	if len(os.Args) <= 1 {
		return // double clicked: the window is the interface
	}
	const attachParentProcess = ^uint32(0)
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if kernel32.NewProc("AttachConsole").Find() != nil {
		return
	}
	// Preserve redirected output handles.
	if hasStdHandle(syscall.STD_OUTPUT_HANDLE) {
		return
	}
	r, _, _ := kernel32.NewProc("AttachConsole").Call(uintptr(attachParentProcess))
	if r == 0 {
		return // no parent console, for instance started by the scheduler
	}
	reopen(&os.Stdout, syscall.STD_OUTPUT_HANDLE, "CONOUT$")
	reopen(&os.Stderr, syscall.STD_ERROR_HANDLE, "CONOUT$")
	reopen(&os.Stdin, syscall.STD_INPUT_HANDLE, "CONIN$")
}

func hasStdHandle(std int) bool {
	h, err := windows.GetStdHandle(uint32(std))
	return err == nil && h != 0 && h != windows.InvalidHandle
}

func reopen(f **os.File, std int, name string) {
	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr(name),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return
	}
	windows.SetStdHandle(uint32(std), h)
	*f = os.NewFile(uintptr(h), name)
}

// holdWindowOpen is unused by the windowed application and parent-terminal mode.
func holdWindowOpen() {}
