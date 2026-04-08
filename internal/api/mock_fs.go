package api

import (
	"io"
	"io/fs"
	"strings"
	"time"
)

// MockFS mock for embed.FS testing
type MockFS struct {
	Files     map[string]string
	OpenError error
	StatError error
}

// mockFile implements io.ReadSeeker for testing
type mockFile struct {
	content   string
	name      string
	pos       int
	statError error
}

func (f *mockFile) Stat() (fs.FileInfo, error) {
	if f.statError != nil {
		return nil, f.statError
	}
	return &mockFileInfo{name: f.name, size: int64(len(f.content))}, nil
}

func (f *mockFile) Read(p []byte) (int, error) {
	if f.pos >= len(f.content) {
		return 0, io.EOF
	}
	n := copy(p, f.content[f.pos:])
	f.pos += n
	return n, nil
}

func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = int(offset)
	case io.SeekCurrent:
		f.pos += int(offset)
	case io.SeekEnd:
		f.pos = len(f.content) + int(offset)
	}
	return int64(f.pos), nil
}

func (f *mockFile) Close() error {
	return nil
}

// mockFileInfo implements fs.FileInfo
type mockFileInfo struct {
	name string
	size int64
}

func (fi *mockFileInfo) Name() string       { return fi.name }
func (fi *mockFileInfo) Size() int64        { return fi.size }
func (fi *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *mockFileInfo) ModTime() time.Time { return time.Now() }
func (fi *mockFileInfo) IsDir() bool        { return false }
func (fi *mockFileInfo) Sys() interface{}   { return nil }

func (m *MockFS) Open(name string) (fs.File, error) {
	if m.OpenError != nil {
		return nil, m.OpenError
	}
	content, ok := m.Files[name]
	if !ok {
		// Try with index.html fallback
		if name == "index.html" || strings.HasSuffix(name, "/") {
			if content, ok = m.Files["index.html"]; ok {
				return &mockFile{content: content, name: "index.html", statError: m.StatError}, nil
			}
		}
		return nil, fs.ErrNotExist
	}
	return &mockFile{content: content, name: name, statError: m.StatError}, nil
}

func (m *MockFS) ReadFile(name string) ([]byte, error) {
	content, ok := m.Files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(content), nil
}

func (m *MockFS) Exists(name string) bool {
	_, ok := m.Files[name]
	return ok
}
