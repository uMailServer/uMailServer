package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreExistingRegularFile(t *testing.T) {
	s, e := NewMessageStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("valid content")
	id, e := s.StoreMessage("u", data)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.StoreMessage("u", data)
	if e != nil || again != id {
		t.Fatalf("invalid duplicate control id=%s err=%v", again, e)
	}
	got, e := s.ReadMessage("u", id)
	if e != nil || !bytes.Equal(got, data) {
		t.Fatalf("invalid read control %q %v", got, e)
	}
	fmt.Println("CONTROL EXPECTED: duplicate readable regular file ACTUAL: matched")
}
func TestStoreDirectoryCollisionError(t *testing.T) {
	root := t.TempDir()
	s, e := NewMessageStore(root)
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("directory collision")
	hash := sha256.Sum256(data)
	id := hex.EncodeToString(hash[:])
	path := filepath.Join(root, "u", id[:2], id[2:4], id)
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	got, e := s.StoreMessage("u", data)
	fmt.Printf("EXPECTED: non-nil store error for directory ACTUAL: id=%q err=%v\n", got, e)
	if e == nil {
		t.Fatal("DEFECT F4777 directory collision reported as successful stored message")
	}
	info, e := os.Stat(path)
	if e != nil || !info.IsDir() {
		t.Fatal("fixture directory changed")
	}
}
func TestStoreDuplicatePayloadEdges(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("one"), []byte("two")} {
		s, e := NewMessageStore(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		id, e := s.StoreMessage("u", data)
		if e != nil {
			t.Fatal(e)
		}
		again, e := s.StoreMessage("u", data)
		if e != nil || again != id {
			t.Fatalf("repeat id=%v error=%v", again, e)
		}
		got, e := s.ReadMessage("u", id)
		if e != nil || !bytes.Equal(got, data) {
			t.Fatalf("read %q %v", got, e)
		}
	}
}
