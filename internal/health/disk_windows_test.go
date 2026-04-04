//go:build windows
// +build windows

package health

import (
	"context"
	"testing"
)

func TestDiskSpaceCheck(t *testing.T) {
	// Test with a valid Windows path
	checker := DiskSpaceCheck("C:\\", 80.0, 90.0)
	check := checker(context.Background())

	// Should succeed on Windows with valid path
	if check.Name != "disk_space" {
		t.Errorf("expected check name 'disk_space', got %s", check.Name)
	}

	t.Logf("Disk space check: status=%s, message=%s", check.Status, check.Message)
	t.Logf("Disk space details: %+v", check.Details)

	// Verify details are populated
	if _, ok := check.Details["path"]; !ok {
		t.Error("expected path in details")
	}
	if _, ok := check.Details["total_gb"]; !ok {
		t.Error("expected total_gb in details")
	}
	if _, ok := check.Details["free_gb"]; !ok {
		t.Error("expected free_gb in details")
	}
	if _, ok := check.Details["used_percent"]; !ok {
		t.Error("expected used_percent in details")
	}
}

func TestDiskSpaceCheck_InvalidPath(t *testing.T) {
	// Test with an invalid path that cannot be converted to UTF16
	// Using null character which is invalid in Windows paths
	checker := DiskSpaceCheck("\\x00invalid", 80.0, 90.0)
	check := checker(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status for invalid path, got %s", check.Status)
	}
}

func TestDiskSpaceCheck_NonExistentPath(t *testing.T) {
	// Test with a non-existent drive letter
	checker := DiskSpaceCheck("Z:\\", 80.0, 90.0)
	check := checker(context.Background())

	// This may fail or succeed depending on whether Z: drive exists
	t.Logf("Non-existent path check: status=%s, message=%s", check.Status, check.Message)
}
