//go:build windows

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// captureStderr points the process's real stderr at the log file when
// no console exists, so runtime panics land somewhere readable instead
// of vanishing. A windowed (windowsgui) process has no console; headless
// runs in a terminal keep their console output.
func captureStderr(f *os.File) {
	getConsole := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	if h, _, _ := getConsole.Call(); h != 0 {
		return // a console is attached; leave stderr alone
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd())); err != nil {
		return
	}
	os.Stderr = f
}
