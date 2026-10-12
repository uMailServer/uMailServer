package cli

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// R226 F7030: every header field (magic, version, salt, chunk size) must be
// authenticated or rejected; empty archives round-trip.
func TestStreamHeaderFieldsAuthenticatedR226(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 200)
	env := sealStream(t, "pw", data, 64)
	mut := func(name string, f func(b []byte)) {
		t.Helper()
		b := append([]byte{}, env...)
		f(b)
		if got, err := decryptStreamBytes(b, "pw"); err == nil {
			t.Errorf("%s: accepted (%d bytes)", name, len(got))
		}
	}
	mut("magic", func(b []byte) { b[0] ^= 1 })
	mut("version", func(b []byte) { b[len(backupMagic)] = backupVersion })
	mut("salt-last", func(b []byte) { b[len(backupMagic)+saltSize] ^= 0x80 })
	// Chunk size changed to another valid value: framing and AAD both differ.
	mut("chunk-size", func(b []byte) { binary.BigEndian.PutUint32(b[streamHeaderSize-4:], 128) })
	mut("chunk-size-zero", func(b []byte) { binary.BigEndian.PutUint32(b[streamHeaderSize-4:], 0) })
	mut("chunk-size-huge", func(b []byte) { binary.BigEndian.PutUint32(b[streamHeaderSize-4:], maxChunkSize+1) })

	// A v2 envelope relabelled as legacy v1 must fail through the BackupManager path too.
	bm := &BackupManager{password: "pw"}
	b := append([]byte{}, env...)
	b[len(backupMagic)] = backupVersion
	if _, err := bm.decryptBackup(b); err == nil {
		t.Error("v2 relabelled as v1 accepted")
	}

	// Empty plaintext via the manager API.
	enc, err := bm.encryptBackup(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := bm.decryptBackup(enc)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty round trip: %v %d", err, len(got))
	}
}
