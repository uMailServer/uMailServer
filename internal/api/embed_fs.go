package api

import (
	"io"
	"io/fs"
)

// embedFSAdapter wraps embed.FS to implement the FileSystem interface
type embedFSAdapter struct {
	fs fs.FS
}

// NewEmbedFSAdapter creates a new adapter for embed.FS
func NewEmbedFSAdapter(embeddedFS fs.FS) FileSystem {
	return &embedFSAdapter{fs: embeddedFS}
}

// newEmbedFSSub creates a sub-FS from an embedded FS
func newEmbedFSSub(embeddedFS fs.FS, path string) FileSystem {
	subFS, err := fs.Sub(embeddedFS, path)
	if err != nil {
		// Return a failing adapter
		return &embedFSAdapter{fs: nil}
	}
	return &embedFSAdapter{fs: subFS}
}

func (a *embedFSAdapter) Open(name string) (fs.File, error) {
	if a.fs == nil {
		return nil, fs.ErrNotExist
	}
	return a.fs.Open(name)
}

func (a *embedFSAdapter) ReadFile(name string) ([]byte, error) {
	if a.fs == nil {
		return nil, fs.ErrNotExist
	}
	file, err := a.fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func (a *embedFSAdapter) Exists(name string) bool {
	if a.fs == nil {
		return false
	}
	_, err := a.fs.Open(name)
	return err == nil
}
