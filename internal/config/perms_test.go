//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// F6014: the data directory holds secrets and mail; it must be created 0700.
func TestRegressionF6014DataDirPerms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	c := DefaultConfig()
	c.Server.DataDir = root
	if err := c.EnsureDataDir(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{root, filepath.Join(root, "queue"), filepath.Join(root, "domains"), filepath.Join(root, "tmp")} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s mode %v grants group/other access", d, fi.Mode().Perm())
		}
	}
	// Validate (checkDirWritable) creating a missing data dir must be 0700 too.
	root2 := filepath.Join(t.TempDir(), "d2")
	if err := checkDirWritable(root2); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(root2); fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("checkDirWritable created %v", fi.Mode().Perm())
	}
}
