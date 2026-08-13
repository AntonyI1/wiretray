//go:build windows

package tray

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	pFindWindowEx             = user32.NewProc("FindWindowExW")
	pGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	pSendMessage              = user32.NewProc("SendMessageW")
)

// closeActiveMenu ends menu mode on our tray window, synchronously, so
// that a clicked item behaves like a standard Windows menu: the menu
// closes first, then the action and any icon repaint happen with no
// open menu left to race. The window is found by class AND process id,
// because every app built on the same systray library registers the
// same class name.
func closeActiveMenu() {
	hwnd := ownTrayWindow()
	if hwnd == 0 {
		return
	}
	const wmCancelMode = 0x001F
	_, _, _ = pSendMessage.Call(hwnd, wmCancelMode, 0, 0)
}

func ownTrayWindow() uintptr {
	cls, err := syscall.UTF16PtrFromString("SystrayClass")
	if err != nil {
		return 0
	}
	pid := uint32(os.Getpid())

	var hwnd uintptr
	for {
		hwnd, _, _ = pFindWindowEx.Call(0, hwnd, uintptr(unsafe.Pointer(cls)), 0)
		if hwnd == 0 {
			return 0
		}
		var owner uint32
		_, _, _ = pGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			return hwnd
		}
	}
}
