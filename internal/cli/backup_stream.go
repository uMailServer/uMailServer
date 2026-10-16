package cli

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/scrypt"
)

// Streaming backup format (version 2, F6460).
//
//	magic(11) | version(1)=2 | salt(32) | chunkSize(4, big endian) | chunks...
//
// Every chunk is sealed with AES-256-GCM. The 12-byte nonce is
// counter(8, big endian, in bytes 3..10) with the last byte holding a
// "final" flag (STREAM construction), and the header bytes are bound as
// additional data. A chunk can therefore not be reordered, dropped from the
// middle, or have the stream truncated at a chunk boundary: the last chunk is
// the only one opened with the final flag set. Memory use is bounded by the
// chunk size instead of the archive size.
const (
	backupVersionStream = 2
	defaultChunkSize    = 64 << 10
	maxChunkSize        = 16 << 20
	gcmTagSize          = 16
	streamHeaderSize    = len(backupMagic) + 1 + saltSize + 4
)

var errBackupDecrypt = errors.New("failed to decrypt: wrong password or corrupted backup")

func deriveBackupKey(password string, salt []byte) ([]byte, error) {
	key, err := scrypt.Key([]byte(password), salt, 1<<18, 8, 1, keySize)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}
	return key, nil
}

func newBackupGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm, nil
}

func chunkNonce(dst []byte, counter uint64, final bool) []byte {
	for i := range dst {
		dst[i] = 0
	}
	binary.BigEndian.PutUint64(dst[3:11], counter)
	if final {
		dst[11] = 1
	}
	return dst
}

// streamEncryptWriter encrypts everything written to it. Close must be called
// to emit the final chunk.
type streamEncryptWriter struct {
	w       io.Writer
	gcm     cipher.AEAD
	header  []byte
	chunk   int
	buf     []byte // pending plaintext, never more than chunk+... bytes
	out     []byte
	nonce   [12]byte
	counter uint64
	closed  bool
	err     error
}

// newStreamEncryptWriter writes the header to w and returns the writer.
func newStreamEncryptWriter(w io.Writer, password string, chunk int) (*streamEncryptWriter, error) {
	if chunk <= 0 || chunk > maxChunkSize {
		return nil, fmt.Errorf("invalid chunk size %d", chunk)
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	key, err := deriveBackupKey(password, salt)
	if err != nil {
		return nil, err
	}
	gcm, err := newBackupGCM(key)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, streamHeaderSize)
	header = append(header, backupMagic...)
	header = append(header, backupVersionStream)
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, uint32(chunk))
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return &streamEncryptWriter{
		w: w, gcm: gcm, header: header, chunk: chunk,
		buf: make([]byte, 0, chunk+1),
		out: make([]byte, 0, chunk+gcmTagSize),
	}, nil
}

func (s *streamEncryptWriter) seal(final bool) error {
	s.out = s.gcm.Seal(s.out[:0], chunkNonce(s.nonce[:], s.counter, final), s.buf, s.header)
	s.counter++
	s.buf = s.buf[:0]
	_, err := s.w.Write(s.out)
	return err
}

func (s *streamEncryptWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.closed {
		return 0, errors.New("write after close")
	}
	n := 0
	for len(p) > 0 {
		// Keep at least one byte pending so the final chunk is never empty
		// unless the whole stream is.
		if len(s.buf) == s.chunk {
			if err := s.seal(false); err != nil {
				s.err = err
				return n, err
			}
		}
		take := s.chunk - len(s.buf)
		if take > len(p) {
			take = len(p)
		}
		s.buf = append(s.buf, p[:take]...)
		p = p[take:]
		n += take
	}
	return n, nil
}

// Close seals the final chunk.
func (s *streamEncryptWriter) Close() error {
	if s.err != nil {
		return s.err
	}
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.seal(true); err != nil {
		s.err = err
		return err
	}
	return nil
}

// streamDecryptReader authenticates and decrypts chunk by chunk. A read error
// (including errBackupDecrypt for truncation) is returned only after the
// plaintext of earlier, authenticated chunks was handed out; callers must read
// to EOF before trusting the stream.
type streamDecryptReader struct {
	r       *bufio.Reader
	gcm     cipher.AEAD
	header  []byte
	chunk   int
	in      []byte
	plain   []byte
	pending []byte
	nonce   [12]byte
	counter uint64
	done    bool
	err     error
}

// newStreamDecryptReader parses the header (magic and version included).
func newStreamDecryptReader(r *bufio.Reader, password string) (*streamDecryptReader, error) {
	header := make([]byte, streamHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("invalid backup file: too short")
	}
	if string(header[:len(backupMagic)]) != backupMagic {
		return nil, fmt.Errorf("invalid backup file: bad magic")
	}
	if v := int(header[len(backupMagic)]); v != backupVersionStream {
		return nil, fmt.Errorf("unsupported backup version: %d", v)
	}
	salt := header[len(backupMagic)+1 : len(backupMagic)+1+saltSize]
	chunk := int(binary.BigEndian.Uint32(header[len(header)-4:]))
	if chunk <= 0 || chunk > maxChunkSize {
		return nil, fmt.Errorf("invalid backup: chunk size %d out of range", chunk)
	}
	key, err := deriveBackupKey(password, salt)
	if err != nil {
		return nil, err
	}
	gcm, err := newBackupGCM(key)
	if err != nil {
		return nil, err
	}
	return &streamDecryptReader{
		r: r, gcm: gcm, header: header, chunk: chunk,
		in:    make([]byte, chunk+gcmTagSize),
		plain: make([]byte, 0, chunk),
	}, nil
}

func (s *streamDecryptReader) next() error {
	n, err := io.ReadFull(s.r, s.in)
	final := false
	switch err {
	case nil:
		// Full-size chunk: final only if nothing follows.
		if _, perr := s.r.Peek(1); perr == io.EOF {
			final = true
		} else if perr != nil {
			return perr
		}
	case io.ErrUnexpectedEOF, io.EOF:
		final = true
	default:
		return err
	}
	if n < gcmTagSize {
		return errBackupDecrypt // truncated: no authenticated final chunk
	}
	pt, oerr := s.gcm.Open(s.plain[:0], chunkNonce(s.nonce[:], s.counter, final), s.in[:n], s.header)
	if oerr != nil {
		return errBackupDecrypt
	}
	s.counter++
	s.plain = pt
	s.pending = pt
	if final {
		s.done = true
	}
	return nil
}

func (s *streamDecryptReader) Read(p []byte) (int, error) {
	for len(s.pending) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		if s.done {
			s.err = io.EOF
			return 0, io.EOF
		}
		if err := s.next(); err != nil {
			s.err = err
			return 0, err
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

// decryptStreamBytes decrypts an in-memory v2 envelope.
func decryptStreamBytes(data []byte, password string) ([]byte, error) {
	sr, err := newStreamDecryptReader(bufio.NewReader(bytes.NewReader(data)), password)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(sr)
}
