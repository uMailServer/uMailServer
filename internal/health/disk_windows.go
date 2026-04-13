//go:build windows
// +build windows

package health

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"
)

// DiskSpaceCheck creates a disk space checker for Windows systems
func DiskSpaceCheck(path string, warningThreshold, criticalThreshold float64) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Name:    "disk_space",
			Status:  StatusHealthy,
			Details: make(map[string]interface{}),
		}

		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

		pathPtr, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("invalid path: %v", err)
			return check
		}

		var freeBytesAvailable, totalBytes, totalFreeBytes uint64

		// #nosec G103 -- required for Windows kernel32 syscall with unsafe.Pointer
		ret, _, err := getDiskFreeSpaceEx.Call(
			uintptr(unsafe.Pointer(pathPtr)),
			uintptr(unsafe.Pointer(&freeBytesAvailable)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFreeBytes)),
		)

		if ret == 0 {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("failed to check disk space: %v", err)
			return check
		}

		// Calculate usage percentage
		used := totalBytes - totalFreeBytes
		usagePercent := float64(used) / float64(totalBytes) * 100

		check.Details["path"] = path
		check.Details["total_gb"] = float64(totalBytes) / 1024 / 1024 / 1024
		check.Details["free_gb"] = float64(totalFreeBytes) / 1024 / 1024 / 1024
		check.Details["used_percent"] = usagePercent

		if usagePercent >= criticalThreshold {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("disk critically full: %.1f%% used", usagePercent)
		} else if usagePercent >= warningThreshold {
			check.Status = StatusDegraded
			check.Message = fmt.Sprintf("disk space warning: %.1f%% used", usagePercent)
		} else {
			check.Message = fmt.Sprintf("disk space healthy: %.1f%% used", usagePercent)
		}

		return check
	}
}
