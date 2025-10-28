// +build windows

package main

import (
	"syscall"
	"unsafe"
)

func showMessageBox(title, text, typ string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")
	var flags uintptr = 0x00000000 // MB_OK
	switch typ {
	case "info":
		flags = 0x00000040 // MB_ICONINFORMATION
	case "error":
		flags = 0x00000010 // MB_ICONERROR
	}
	// call
	messageBoxW.Call(0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		flags)
}
