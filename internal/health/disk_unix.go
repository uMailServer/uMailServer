//go:build !windows
// +build !windows

package health

import (
	"context"
	"fmt"
	"syscall"
)

// DiskSpaceCheck creates a disk space checker for Unix systems
func DiskSpaceCheck(path string, warningThreshold, criticalThreshold float64) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Name:    "disk_space",
			Status:  StatusHealthy,
			Details: make(map[string]interface{}),
		}

		var stat syscall.Statfs_t
		err := syscall.Statfs(path, &stat)
		if err != nil {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("failed to check disk space: %v", err)
			return check
		}

		// Calculate usage percentage
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bavail * uint64(stat.Bsize)
		used := total - free
		usagePercent := float64(used) / float64(total) * 100

		check.Details["path"] = path
		check.Details["total_gb"] = float64(total) / 1024 / 1024 / 1024
		check.Details["free_gb"] = float64(free) / 1024 / 1024 / 1024
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
