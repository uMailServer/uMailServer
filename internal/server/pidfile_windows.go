//go:build windows

package server

import "syscall"

func isProcessRunning(pid int) bool {
	// #nosec G115 -- Windows PIDs are DWORD (32-bit unsigned), uint32 conversion is safe
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle)
	return true
}
