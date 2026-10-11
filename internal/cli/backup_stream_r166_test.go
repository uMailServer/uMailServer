package cli

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

// F6460: chunked authenticated encryption.

func sealStream(t *testing.T, pw string, data []byte, chunk int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := newStreamEncryptWriter(&buf, pw, chunk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStreamRoundTripChunkBoundaries(t *testing.T) {
	for _, n := range []int{0, 1, 63, 64, 65, 128, 129, 1000} {
		data := make([]byte, n)
		_, _ = rand.Read(data)
		env := sealStream(t, "pw", data, 64)
		got, err := decryptStreamBytes(env, "pw")
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("n=%d: err=%v equal=%v", n, err, bytes.Equal(got, data))
		}
	}
}

func chunkLen(chunk int) int { return chunk + gcmTagSize }

func TestStreamTamperTruncateReorderRejected(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 40) // 640 bytes = 10 chunks of 64
	env := sealStream(t, "pw", data, 64)
	body := env[streamHeaderSize:]
	cl := chunkLen(64)

	mustFail := func(name string, e []byte) {
		t.Helper()
		if _, err := decryptStreamBytes(e, "pw"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// flipped ciphertext bit
	f := append([]byte{}, env...)
	f[len(f)-5] ^= 1
	mustFail("tamper", f)
	// header tamper (chunk size is bound as AAD and also changes framing)
	h := append([]byte{}, env...)
	h[len(backupMagic)+1] ^= 1 // salt
	mustFail("salt", h)
	// truncation at a chunk boundary (drop the final chunk)
	mustFail("truncate-boundary", env[:streamHeaderSize+len(body)-(len(body)%cl)-cl])
	// truncation mid-chunk
	mustFail("truncate-mid", env[:len(env)-3])
	// truncation to header only
	mustFail("header-only", env[:streamHeaderSize])
	// reorder two chunks
	r := append([]byte{}, env...)
	a := r[streamHeaderSize : streamHeaderSize+cl]
	b := r[streamHeaderSize+cl : streamHeaderSize+2*cl]
	tmp := append([]byte{}, a...)
	copy(a, b)
	copy(b, tmp)
	mustFail("reorder", r)
	// duplicated chunk
	d := append([]byte{}, env[:streamHeaderSize+cl]...)
	d = append(d, env[streamHeaderSize:]...)
	mustFail("duplicate", d)
	// appended trailing data
	mustFail("append", append(append([]byte{}, env...), 1, 2, 3))
	// wrong password
	if _, err := decryptStreamBytes(env, "other"); err == nil {
		t.Error("wrong password accepted")
	}
}

// legacySeal builds the version-1 single-buffer envelope.
func legacySeal(t *testing.T, pw string, data []byte) []byte {
	t.Helper()
	salt := make([]byte, saltSize)
	nonce := make([]byte, nonceSize)
	_, _ = rand.Read(salt)
	_, _ = rand.Read(nonce)
	key, err := deriveBackupKey(pw, salt)
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(blk)
	out := append([]byte(backupMagic), backupVersion)
	out = append(out, salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, data, nil)
}

func r166Config(t *testing.T) (*config.Config, string) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(data, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config", "a.yaml"), []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.DataDir = data
	return cfg, root
}

func TestBackupEncryptedStreamRestoreVerifyAndLegacy(t *testing.T) {
	cfg, root := r166Config(t)
	bm := NewBackupManager(cfg)
	bm.SetPassword("pw")
	out := filepath.Join(root, "out")
	if err := bm.Backup(out); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(out, "*.enc"))
	if len(files) != 1 {
		t.Fatalf("files=%v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if raw[len(backupMagic)] != backupVersionStream {
		t.Fatalf("version byte %d", raw[len(backupMagic)])
	}
	if err := bm.Verify(files[0]); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := bm.Restore(files[0]); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "restore_temp", "config", "config", "a.yaml")); string(b) != "x: 1\n" {
		t.Fatalf("restored content %q", b)
	}

	// Truncating at a chunk boundary must fail Verify and Restore, and
	// Restore must publish nothing.
	cl := chunkLen(defaultChunkSize)
	body := len(raw) - streamHeaderSize
	if body > cl {
		trunc := filepath.Join(root, "trunc.tar.gz.enc")
		cut := streamHeaderSize + (body/cl)*cl
		if cut == len(raw) {
			cut -= cl
		}
		_ = os.WriteFile(trunc, raw[:cut], 0o600)
		if err := bm.Verify(trunc); err == nil {
			t.Error("verify accepted truncated backup")
		}
	}
	bad := append([]byte{}, raw...)
	bad[len(bad)-1] ^= 1
	badf := filepath.Join(root, "bad.tar.gz.enc")
	_ = os.WriteFile(badf, bad, 0o600)
	if err := bm.Verify(badf); err == nil {
		t.Error("verify accepted tampered backup")
	}
	_ = os.RemoveAll(filepath.Join(root, "restore_temp"))
	if err := bm.Restore(badf); err == nil {
		t.Error("restore accepted tampered backup")
	}
	if _, err := os.Stat(filepath.Join(root, "restore_temp")); err == nil {
		t.Error("tampered restore published restore_temp")
	}
}

func TestRestoreVerifyLegacyV1Format(t *testing.T) {
	cfg, root := r166Config(t)
	// Produce a plain tar.gz with the current code, then wrap it in v1.
	plainBM := NewBackupManager(cfg)
	out := filepath.Join(root, "out")
	if err := plainBM.Backup(out); err != nil {
		t.Fatal(err)
	}
	pf, _ := filepath.Glob(filepath.Join(out, "*.tar.gz"))
	tarGz, _ := os.ReadFile(pf[0])
	legacy := filepath.Join(root, "legacy.tar.gz.enc")
	_ = os.WriteFile(legacy, legacySeal(t, "pw", tarGz), 0o600)

	bm := NewBackupManager(cfg)
	bm.SetPassword("pw")
	if err := bm.Verify(legacy); err != nil {
		t.Fatalf("verify v1: %v", err)
	}
	if err := bm.Restore(legacy); err != nil {
		t.Fatalf("restore v1: %v", err)
	}
	bm.SetPassword("nope")
	_ = os.RemoveAll(filepath.Join(root, "restore_temp"))
	if err := bm.Restore(legacy); err == nil {
		t.Fatal("v1 wrong password accepted")
	}
	// decryptBackup in-memory form too
	bm.SetPassword("pw")
	if got, err := bm.decryptBackup(legacySeal(t, "pw", []byte("hello world of backups, long enough"))); err != nil || string(got) != "hello world of backups, long enough" {
		t.Fatalf("decryptBackup v1: %q %v", got, err)
	}
}

type zeroReader struct{ n int64 }

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > z.n {
		p = p[:z.n]
	}
	for i := range p {
		p[i] = byte(i)
	}
	z.n -= int64(len(p))
	return len(p), nil
}

// The encrypt/decrypt path must allocate O(chunk), not O(archive).
func TestStreamMemoryBound(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a large stream")
	}
	const size = 96 << 20
	var sink countingDiscard
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	pr, pw := io.Pipe()
	go func() {
		w, err := newStreamEncryptWriter(pw, "pw", defaultChunkSize)
		if err == nil {
			_, err = io.Copy(w, &zeroReader{n: size})
		}
		if err == nil {
			err = w.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	br := bufio.NewReader(pr)
	sr, err := newStreamDecryptReader(br, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&sink, sr); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if sink.n != size {
		t.Fatalf("got %d bytes", sink.n)
	}
	// scrypt (256 MiB x2) dominates; the archive itself would add 96+ MiB
	// per copy. Compare allocations excluding the two key derivations.
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 2*(256<<20)+(40<<20) {
		t.Fatalf("allocated %d MiB for a %d MiB stream", alloc>>20, size>>20)
	}
}

type countingDiscard struct{ n int64 }

func (c *countingDiscard) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

var _ = strings.Repeat
