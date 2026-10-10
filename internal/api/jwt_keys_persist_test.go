package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
	"go.etcd.io/bbolt"
)

// Regression tests for F5540 (rotated JWT keys lost on restart) and F5541
// (legacy jwt_secret kid never pruned by rotation).

func jwtKeysSecret(tag string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("jwt-keys-test-"+tag)))
}

type jwtKeysEnv struct {
	t    *testing.T
	path string
	db   *db.DB
}

func newJWTKeysEnv(t *testing.T) *jwtKeysEnv {
	t.Helper()
	e := &jwtKeysEnv{t: t, path: filepath.Join(t.TempDir(), "a.db")}
	t.Cleanup(func() {
		if e.db != nil {
			e.db.Close()
		}
	})
	return e
}

// start (re)opens the database and builds a server, as a process start does.
func (e *jwtKeysEnv) start(secret string, disableLegacy bool) *Server {
	e.t.Helper()
	if e.db != nil {
		e.db.Close()
	}
	d, err := db.Open(e.path)
	if err != nil {
		e.t.Fatalf("open db: %v", err)
	}
	e.db = d
	return NewServer(d, nil, Config{JWTSecret: secret, TokenExpiry: time.Hour, DisableLegacyJWT: disableLegacy})
}

func jwtKeysRotate(s *Server) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.handleJWTRotate(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin/jwt/rotate", nil))
	return w
}

func jwtKeysMustRotate(t *testing.T, s *Server) {
	t.Helper()
	if w := jwtKeysRotate(s); w.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", w.Code, w.Body.String())
	}
}

func jwtKeysSign(t *testing.T, kid string, key []byte) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "root@ex.com", "admin": true, "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = kid
	str, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return str
}

// jwtKeysIssue signs like handleLogin: current kid and its secret.
func jwtKeysIssue(t *testing.T, s *Server) (string, string) {
	t.Helper()
	kid, key := s.signingKey()
	return kid, jwtKeysSign(t, kid, key)
}

func jwtKeysValid(s *Server, tok string) bool {
	p, err := jwt.Parse(tok, s.jwtKey, jwt.WithValidMethods([]string{"HS256"}))
	return err == nil && p.Valid
}

func jwtKeysSnapshot(s *Server) (string, map[string]string) {
	s.jwtMu.RLock()
	defer s.jwtMu.RUnlock()
	m := make(map[string]string, len(s.jwtSecrets))
	for k, v := range s.jwtSecrets {
		m[k] = v
	}
	return s.currentKid, m
}

func (e *jwtKeysEnv) rawRecord() string {
	e.t.Helper()
	var raw string
	if err := e.db.BoltDB().View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(jwtKeysBucket)); b != nil {
			raw = string(b.Get([]byte(jwtKeysRecord)))
		}
		return nil
	}); err != nil {
		e.t.Fatalf("read record: %v", err)
	}
	return raw
}

func TestJWTKeysSurviveRestart(t *testing.T) {
	e := newJWTKeysEnv(t)
	secret := jwtKeysSecret("legacy")
	legacy := jwtKeysSign(t, "default", []byte(secret))

	a := e.start(secret, true)
	jwtKeysMustRotate(t, a)
	rotKid, post := jwtKeysIssue(t, a)

	b := e.start(secret, true)
	if kid, _ := b.signingKey(); kid != rotKid {
		t.Fatalf("signing kid after restart = %q, want rotated %q", kid, rotKid)
	}
	if !jwtKeysValid(b, post) {
		t.Fatal("post-rotation token rejected after restart")
	}
	if jwtKeysValid(b, legacy) {
		t.Fatal("legacy jwt_secret token accepted after restart with disable_legacy_jwt")
	}

	// Without the flag the legacy kid is still inside the retention window.
	c := e.start(secret, false)
	if !jwtKeysValid(c, legacy) || !jwtKeysValid(c, post) {
		t.Fatal("tokens within the retention window rejected after restart")
	}
	// A second rotation after restart keeps the restored keys.
	jwtKeysMustRotate(t, c)
	d := e.start(secret, false)
	if !jwtKeysValid(d, post) || !jwtKeysValid(d, legacy) {
		t.Fatal("restored keys lost by a rotation after restart")
	}
}

func TestJWTKeysChangedSecretDiscardsPersistedSet(t *testing.T) {
	e := newJWTKeysEnv(t)
	a := e.start(jwtKeysSecret("old"), false)
	jwtKeysMustRotate(t, a)
	_, post := jwtKeysIssue(t, a)

	newSecret := jwtKeysSecret("new")
	b := e.start(newSecret, false)
	kid, key := b.signingKey()
	if kid != "default" || string(key) != newSecret {
		t.Fatalf("after jwt_secret change signing kid=%q, want default with the new secret", kid)
	}
	if jwtKeysValid(b, post) {
		t.Fatal("key rotated under the old jwt_secret still accepted after jwt_secret change")
	}
}

func TestJWTKeysRecordIsSealed(t *testing.T) {
	e := newJWTKeysEnv(t)
	secret := jwtKeysSecret("legacy")
	a := e.start(secret, false)
	w := jwtKeysRotate(a)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate status=%d", w.Code)
	}
	_, keys := jwtKeysSnapshot(a)
	raw := e.rawRecord()
	if !strings.HasPrefix(raw, "enc2:") {
		t.Fatalf("persisted record is not sealed: %.20q", raw)
	}
	for kid, sec := range keys {
		if strings.Contains(raw, sec) {
			t.Fatalf("persisted record contains key material for kid %q", kid)
		}
		if strings.Contains(w.Body.String(), sec) {
			t.Fatalf("rotate response contains key material for kid %q", kid)
		}
	}
}

func TestJWTKeysUnsealedRecordIgnored(t *testing.T) {
	e := newJWTKeysEnv(t)
	secret := jwtKeysSecret("legacy")
	e.start(secret, false)
	forged := jwtKeysSecret("forged")
	plain, _ := json.Marshal(persistedJWTKeys{Current: "kx", Keys: map[string]string{"kx": forged}})
	if err := e.db.BoltDB().Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(jwtKeysBucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(jwtKeysRecord), plain)
	}); err != nil {
		t.Fatalf("write record: %v", err)
	}
	b := e.start(secret, false)
	if kid, _ := b.signingKey(); kid != "default" {
		t.Fatalf("unsealed record loaded: signing kid %q", kid)
	}
	if jwtKeysValid(b, jwtKeysSign(t, "kx", []byte(forged))) {
		t.Fatal("token signed with a key from an unsealed record accepted")
	}
}

func TestJWTKeysRotatePersistFailureKeepsKeys(t *testing.T) {
	e := newJWTKeysEnv(t)
	a := e.start(jwtKeysSecret("legacy"), false)
	beforeKid, beforeKeys := jwtKeysSnapshot(a)
	e.db.Close() // inject: the store fails
	e.db = nil
	if w := jwtKeysRotate(a); w.Code != http.StatusInternalServerError {
		t.Fatalf("rotate with failing store status=%d, want 500", w.Code)
	}
	afterKid, afterKeys := jwtKeysSnapshot(a)
	if afterKid != beforeKid || len(afterKeys) != len(beforeKeys) {
		t.Fatalf("failed rotation changed keys: kid %q->%q, %d->%d keys", beforeKid, afterKid, len(beforeKeys), len(afterKeys))
	}
}

// Concurrent rotations released by one barrier: the persisted set must equal
// the published one (no rotation lost between disk and memory).
func TestJWTKeysConcurrentRotatePersistsPublishedSet(t *testing.T) {
	e := newJWTKeysEnv(t)
	secret := jwtKeysSecret("legacy")
	a := e.start(secret, false)
	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = jwtKeysRotate(a).Code
		}(i)
	}
	// Verification concurrent with the rotations.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 50; i++ {
			kid, key := a.signingKey()
			tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "root@ex.com"})
			tok.Header["kid"] = kid
			str, err := tok.SignedString(key)
			if err != nil {
				continue // empty key cannot occur; nothing to verify then
			}
			_ = jwtKeysValid(a, str) // result races with rotation; only memory safety is checked here
		}
	}()
	close(start)
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("rotation %d status=%d", i, c)
		}
	}
	memKid, memKeys := jwtKeysSnapshot(a)
	b := e.start(secret, false)
	diskKid, diskKeys := jwtKeysSnapshot(b)
	if memKid != diskKid || len(memKeys) != len(diskKeys) {
		t.Fatalf("persisted set differs: mem kid=%q keys=%d, disk kid=%q keys=%d", memKid, len(memKeys), diskKid, len(diskKeys))
	}
	for kid, sec := range memKeys {
		if diskKeys[kid] != sec {
			t.Fatalf("persisted secret for kid %q differs", kid)
		}
	}
	if len(memKeys) != 5 {
		t.Fatalf("after 8 rotations want 5 retained keys, got %d", len(memKeys))
	}
}

func TestJWTKeysLegacyKidPrunedByRotation(t *testing.T) {
	e := newJWTKeysEnv(t)
	secret := jwtKeysSecret("legacy")
	legacy := jwtKeysSign(t, "default", []byte(secret))
	a := e.start(secret, false)
	var rotated []string
	for i := 0; i < 4; i++ {
		jwtKeysMustRotate(t, a)
		_, tok := jwtKeysIssue(t, a)
		rotated = append(rotated, tok)
	}
	// Boundary: legacy + 4 rotated = 5 keys, all retained.
	if !jwtKeysValid(a, legacy) {
		t.Fatal("legacy token rejected inside the retention window")
	}
	jwtKeysMustRotate(t, a)
	// Legacy is the oldest key: the 5th rotation retires it.
	if jwtKeysValid(a, legacy) {
		t.Fatal("legacy jwt_secret token still valid after 5 rotations")
	}
	for i, tok := range rotated {
		if !jwtKeysValid(a, tok) {
			t.Fatalf("rotated token %d pruned before the legacy key", i)
		}
	}
	// No-kid legacy-secret token must not fall back to the legacy secret.
	noKid := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "root@ex.com", "exp": time.Now().Add(time.Hour).Unix()})
	noKidStr, _ := noKid.SignedString([]byte(secret))
	if jwtKeysValid(a, noKidStr) {
		t.Fatal("no-kid legacy-secret token accepted after the legacy kid was pruned")
	}
	// Retirement survives a restart.
	b := e.start(secret, false)
	if jwtKeysValid(b, legacy) {
		t.Fatal("pruned legacy kid restored by restart")
	}
}

func TestPruneJWTSecretsOrder(t *testing.T) {
	keys := map[string]string{"default": "a", "custom": "b", "k100": "c", "k200": "d", "kxyz": "e", "k300": "f"}
	pruned := pruneJWTSecrets(keys, "k300", 3)
	if len(keys) != 3 {
		t.Fatalf("want 3 keys, got %d (%v)", len(keys), pruned)
	}
	for _, kid := range []string{"k200", "k300"} {
		if _, ok := keys[kid]; !ok {
			t.Fatalf("newest kid %q pruned; pruned=%v", kid, pruned)
		}
	}
	for _, kid := range []string{"default", "custom", "kxyz"} {
		if _, ok := keys[kid]; ok {
			t.Fatalf("non-rotation kid %q kept over rotated kids; pruned=%v", kid, pruned)
		}
	}
	// The current key is never pruned, even when it is not a rotation kid.
	only := map[string]string{"default": "a", "k1": "b"}
	pruneJWTSecrets(only, "default", 1)
	if _, ok := only["default"]; !ok || len(only) != 1 {
		t.Fatalf("current key pruned or limit missed: %v", only)
	}
	// At the limit nothing is pruned.
	at := map[string]string{"default": "a", "k1": "b"}
	if p := pruneJWTSecrets(at, "k1", 2); len(p) != 0 || len(at) != 2 {
		t.Fatalf("pruned at the limit: %v", p)
	}
}
